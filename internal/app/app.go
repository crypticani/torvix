package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"

	"github.com/crypticani/torvix/internal/adapters/postgres"
	metricsadapter "github.com/crypticani/torvix/internal/adapters/prometheus"
	awsadapter "github.com/crypticani/torvix/internal/adapters/providers/aws"
	ociadapter "github.com/crypticani/torvix/internal/adapters/providers/oci"
	"github.com/crypticani/torvix/internal/config"
	"github.com/crypticani/torvix/internal/core/alerting"
	"github.com/crypticani/torvix/internal/core/analytics"
	"github.com/crypticani/torvix/internal/core/collect"
	"github.com/crypticani/torvix/internal/core/forecasting"
	"github.com/crypticani/torvix/internal/core/normalize"
	"github.com/crypticani/torvix/internal/core/reporting"
	"github.com/crypticani/torvix/internal/domain"
	"github.com/crypticani/torvix/internal/logging"
	httpapi "github.com/crypticani/torvix/internal/ports/http"
	"github.com/crypticani/torvix/internal/ports/providers"
	"github.com/crypticani/torvix/internal/waste"
	wasteengine "github.com/crypticani/torvix/internal/waste/engine"
)

type App struct {
	server          *http.Server
	repo            *postgres.Repository
	alerting        *alerting.Service
	reporting       *reporting.Service
	collector       ingestionRunner
	wasteDetector   waste.Detector
	schedulerCfg    config.Scheduler
	reportingCfg    config.Reporting
	wasteCfg        config.Waste
	logger          *slog.Logger
	schedulerLogger *slog.Logger
	stop            chan struct{}
	stopOnce        sync.Once
	schedulerCtx    context.Context
	cancelScheduler context.CancelFunc
}

type ingestionRunner interface {
	Run(context.Context, time.Time) ([]collect.ProviderResult, error)
}

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	return NewWithLoggers(cfg, logging.Loggers{
		App:       logger,
		HTTP:      logger,
		Ingestion: logger,
		DB:        logger,
		OCI:       logger,
		AWS:       logger,
		Scheduler: logger,
		Alerting:  logger,
		Waste:     logger,
	})
}

func NewWithLoggers(cfg config.Config, loggers logging.Loggers) (*App, error) {
	loggers = loggers.WithDefaults()
	connectCtx, cancelConnect := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelConnect()

	repo, err := postgres.NewWithLogger(connectCtx, cfg.DB.DSN, cfg.DB.MaxConns, cfg.DB.MinConns, loggers.DB)
	if err != nil {
		return nil, err
	}
	migrationCtx, cancelMigration := context.WithTimeout(context.Background(), time.Hour)
	defer cancelMigration()
	if err := postgres.NewMigratorWithLogger(repo.Pool(), "migrations", loggers.App).Run(migrationCtx); err != nil {
		repo.Close()
		return nil, err
	}
	lifecycleCtx, cancelLifecycle := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelLifecycle()
	if err := repo.ApplyDataLifecyclePolicies(lifecycleCtx, cfg.Ingestion.RetentionDays, cfg.Ingestion.CompressionAfterDays); err != nil {
		repo.Close()
		return nil, err
	}

	var collectors []providers.Collector
	if cfg.Providers.OCI.Enabled {
		ociCollector, err := ociadapter.New(cfg.Providers.OCI.WithIngestionDefaults(cfg.Ingestion), loggers.OCI, repo)
		if err != nil {
			return nil, err
		}
		collectors = append(collectors, ociCollector)
	}
	if cfg.Providers.AWS.Enabled {
		awsCollector, err := awsadapter.New(context.Background(), cfg.Providers.AWS, loggers.AWS)
		if err != nil {
			return nil, err
		}
		collectors = append(collectors, awsCollector)
	}

	reg := prom.NewRegistry()
	metrics := metricsadapter.New(cfg.Metrics.Namespace, reg, cfg.Metrics.CostStatsEnabled)

	normalizer := normalize.New()
	collectorSvc := collect.NewWithPolicy(loggers.Ingestion, repo, normalizer, collectors, metrics, collect.Policy{
		LookbackDays:         cfg.Ingestion.LookbackDays,
		RetentionDays:        cfg.Ingestion.RetentionDays,
		CompressionAfterDays: cfg.Ingestion.CompressionAfterDays,
	})
	analyticsSvc := analytics.New(repo)
	forecastingSvc := forecasting.New(repo)
	dailyReportLagDays := cfg.Reporting.DailyReportTargetLagDays
	if cfg.Providers.AWS.Enabled {
		switch cfg.Providers.AWS.IngestionMode {
		case "cur_s3":
			if cfg.Providers.AWS.CURReportLagDays > dailyReportLagDays {
				dailyReportLagDays = cfg.Providers.AWS.CURReportLagDays
			}
		case "cost_explorer":
			if cfg.Providers.AWS.ReportLagDays > dailyReportLagDays {
				dailyReportLagDays = cfg.Providers.AWS.ReportLagDays
			}
		}
	}
	reportingSvc := reporting.NewWithOptions(analyticsSvc, forecastingSvc, reporting.Options{
		Timezone:                 cfg.Reporting.Timezone,
		DailyReportTargetLagDays: dailyReportLagDays,
		RequireCompleteIngestion: cfg.Reporting.RequireCompleteIngestion,
	})
	alertingSvc := alerting.NewWithLogger(&http.Client{Timeout: 10 * time.Second}, cfg.Reporting.Webhooks, loggers.Alerting)
	var wasteProviders []waste.InventoryProvider
	if cfg.Providers.OCI.Enabled {
		ociInventory, err := ociadapter.NewInventoryCollector(cfg.Providers.OCI, repo, loggers.Waste)
		if err != nil {
			loggers.Waste.Warn("OCI waste inventory collector disabled", "error", err)
		} else {
			wasteProviders = append(wasteProviders, ociInventory)
		}
	}
	if cfg.Providers.AWS.Enabled {
		wasteProviders = append(wasteProviders, awsadapter.NewWasteStub(loggers.Waste))
	}
	wasteSvc := wasteengine.NewService(waste.Config{
		Enabled:                cfg.Waste.DetectionEnabled,
		Provider:               domainProvider(cfg.Waste.Provider),
		ScanIntervalHours:      cfg.Waste.ScanIntervalHours,
		MinResourceAgeDays:     cfg.Waste.MinResourceAgeDays,
		StoppedInstanceMinDays: cfg.Waste.StoppedInstanceMinDays,
		OldBackupDays:          cfg.Waste.OldBackupDays,
		MinCostThreshold:       cfg.Waste.MinCostThreshold,
		HighMonthlyThreshold:   cfg.Waste.HighMonthlyThreshold,
		Currency:               cfg.Waste.Currency,
		EnableTagExclusions:    cfg.Waste.EnableTagExclusions,
		ExclusionTagKeys:       cfg.Waste.ExclusionTagKeys,
	}, repo, wasteProviders, loggers.Waste)
	handler := httpapi.NewWithOptions(collectorSvc, analyticsSvc, forecastingSvc, reportingSvc, alertingSvc, reg, httpapi.HandlerOptions{
		LookbackDays:   cfg.Ingestion.LookbackDays,
		RetentionDays:  cfg.Ingestion.RetentionDays,
		APIAuthEnabled: cfg.API.Auth.Enabled,
		APIAuthToken:   cfg.API.Auth.BearerToken,
		GrafanaMetrics: metrics,
		Logger:         loggers.HTTP,
		Waste:          wasteSvc,
	})

	schedulerCtx, cancelScheduler := context.WithCancel(context.Background())
	return &App{
		server: &http.Server{
			Addr:              cfg.HTTP.Address,
			Handler:           requestLogger(loggers.HTTP, handler),
			ReadHeaderTimeout: 5 * time.Second,
		},
		repo:            repo,
		alerting:        alertingSvc,
		reporting:       reportingSvc,
		collector:       collectorSvc,
		wasteDetector:   wasteSvc,
		schedulerCfg:    cfg.Scheduler,
		reportingCfg:    cfg.Reporting,
		wasteCfg:        cfg.Waste,
		logger:          loggers.App,
		schedulerLogger: loggers.Scheduler,
		stop:            make(chan struct{}),
		schedulerCtx:    schedulerCtx,
		cancelScheduler: cancelScheduler,
	}, nil
}

func (a *App) Start() error {
	if a.schedulerCfg.Enabled {
		go a.runIngestionScheduler()
		go a.runReportScheduler()
		go a.runWasteScheduler()
	}
	return a.server.ListenAndServe()
}

func (a *App) runIngestionScheduler() {
	d, err := time.ParseDuration(a.schedulerCfg.IngestInterval)
	if err != nil || d <= 0 {
		a.schedulerLogger.Error("invalid ingest interval", "error", err)
		return
	}
	a.schedulerLogger.Info("starting ingestion scheduler", "interval", d.String())
	ticker := time.NewTicker(d)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.schedulerLogger.Info("scheduler triggered ingestion")
			a.runScheduledIngestion(a.schedulerCtx, time.Now)
		case <-a.stop:
			return
		}
	}
}

func (a *App) runReportScheduler() {
	location, err := reporting.LoadLocation(a.reportingCfg.Timezone)
	if err != nil {
		a.schedulerLogger.Error("invalid report timezone", "timezone", a.reportingCfg.Timezone, "error", err)
		return
	}
	daily, err := parseReportCron(a.reportingCfg.DailyReportCron, location)
	if err != nil {
		a.schedulerLogger.Error("invalid daily report cron", "cron", a.reportingCfg.DailyReportCron, "error", err)
		return
	}
	weekly, err := parseReportCron(a.reportingCfg.WeeklyReportCron, location)
	if err != nil {
		a.schedulerLogger.Error("invalid weekly report cron", "cron", a.reportingCfg.WeeklyReportCron, "error", err)
		return
	}
	a.schedulerLogger.Info("starting report scheduler", "timezone", location.String(), "daily_cron", a.reportingCfg.DailyReportCron, "weekly_cron", a.reportingCfg.WeeklyReportCron)

	now := time.Now()
	a.deliverDueScheduledReportsWithCrons(a.schedulerCtx, now, daily, weekly)
	nextDaily := daily.Next(now)
	nextWeekly := weekly.Next(now)
	for {
		nextRun := earliest(nextDaily, nextWeekly)
		timer := time.NewTimer(time.Until(nextRun))
		select {
		case <-timer.C:
			runAt := time.Now()
			if !runAt.Before(nextDaily) {
				a.deliverScheduledReport(a.schedulerCtx, "daily", runAt)
				nextDaily = daily.Next(runAt)
			}
			if !runAt.Before(nextWeekly) {
				a.deliverScheduledReport(a.schedulerCtx, "weekly", runAt)
				nextWeekly = weekly.Next(runAt)
			}
		case <-a.stop:
			timer.Stop()
			return
		}
	}
}

func (a *App) runWasteScheduler() {
	if a.wasteDetector == nil || !a.wasteCfg.DetectionEnabled {
		return
	}
	interval := time.Duration(a.wasteCfg.ScanIntervalHours) * time.Hour
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	a.schedulerLogger.Info("starting waste detection scheduler", "provider", a.wasteCfg.Provider, "interval", interval.String())
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.schedulerLogger.Info("scheduler triggered waste detection", "provider", a.wasteCfg.Provider)
			if _, err := a.wasteDetector.Run(a.schedulerCtx); err != nil && err != wasteengine.ErrRunAlreadyActive {
				a.schedulerLogger.Error("scheduled waste detection failed", "provider", a.wasteCfg.Provider, "error", err)
			}
		case <-a.stop:
			return
		}
	}
}

func (a *App) runScheduledIngestion(ctx context.Context, now func() time.Time) {
	if a.collector == nil {
		a.schedulerLogger.Error("scheduled ingestion skipped because collector is not configured")
		return
	}
	if now == nil {
		now = time.Now
	}
	started := now()
	results, err := a.collector.Run(ctx, time.Time{})
	completedAt := now()
	duration := completedAt.Sub(started)
	status := scheduledIngestionStatus(results, err)
	if err != nil {
		a.schedulerLogger.Error("scheduled ingestion failed", "status", status, "error", err, "duration", duration.String())
	} else {
		a.schedulerLogger.Info("scheduled ingestion completed", "status", status, "duration", duration.String())
	}
	a.notifyScheduledIngestionComplete(ctx, scheduledIngestionRun{
		Status:          status,
		Results:         results,
		Error:           err,
		DurationSeconds: duration.Seconds(),
	})
	a.deliverDueScheduledReports(ctx, completedAt)
}

func (a *App) deliverScheduledReport(ctx context.Context, period string, now time.Time) {
	if a.reporting == nil || a.alerting == nil {
		return
	}
	if len(a.alerting.ReportDestinations()) == 0 {
		logger := a.schedulerLogger
		if logger == nil {
			logger = a.logger
		}
		if logger != nil {
			logger.Info("scheduled report delivery skipped", "reason", "no enabled alert destinations", "period", period)
		}
		return
	}
	reportCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	for _, result := range a.reporting.DeliverScheduledReport(reportCtx, a.alerting, period, now, reporting.DeliverOptions{}) {
		logger := a.schedulerLogger
		if logger == nil {
			logger = a.logger
		}
		if result.Skipped && result.SkipReason != "" && logger != nil {
			logger.Info("scheduled report delivery skipped", "reason", result.SkipReason, "period", result.Period, "from", result.From, "to", result.To, "destination", result.Destination)
		}
		if result.Error != nil && logger != nil {
			logger.Error("failed to deliver scheduled report", "period", result.Period, "from", result.From, "to", result.To, "destination", result.Destination, "error", result.Error)
		}
		if !result.Skipped && result.Error == nil && logger != nil {
			logger.Info("scheduled report delivered", "period", result.Period, "from", result.From, "to", result.To, "destination", result.Destination)
		}
	}
}

func (a *App) deliverDueScheduledReports(ctx context.Context, now time.Time) {
	location, err := reporting.LoadLocation(a.reportingCfg.Timezone)
	if err != nil {
		a.schedulerLogger.Error("invalid report timezone", "timezone", a.reportingCfg.Timezone, "error", err)
		return
	}
	daily, err := parseReportCron(a.reportingCfg.DailyReportCron, location)
	if err != nil {
		a.schedulerLogger.Error("invalid daily report cron", "cron", a.reportingCfg.DailyReportCron, "error", err)
		return
	}
	weekly, err := parseReportCron(a.reportingCfg.WeeklyReportCron, location)
	if err != nil {
		a.schedulerLogger.Error("invalid weekly report cron", "cron", a.reportingCfg.WeeklyReportCron, "error", err)
		return
	}
	a.deliverDueScheduledReportsWithCrons(ctx, now, daily, weekly)
}

func (a *App) deliverDueScheduledReportsWithCrons(ctx context.Context, now time.Time, daily, weekly reportCron) {
	if daily.DueAtOrBefore(now) {
		a.schedulerLogger.Info("delivering due daily report", "run_at", now)
		a.deliverScheduledReport(ctx, "daily", now)
	}
	if weekly.DueAtOrBefore(now) {
		a.schedulerLogger.Info("delivering due weekly report", "run_at", now)
		a.deliverScheduledReport(ctx, "weekly", now)
	}
}

type scheduledIngestionRun struct {
	Status          string
	Results         []collect.ProviderResult
	Error           error
	DurationSeconds float64
}

func scheduledIngestionStatus(results []collect.ProviderResult, err error) string {
	if err == nil {
		return "success"
	}
	if len(results) > 0 {
		return "partial_failure"
	}
	return "failed"
}

func (a *App) notifyScheduledIngestionComplete(ctx context.Context, run scheduledIngestionRun) {
	if a.alerting == nil {
		return
	}
	alertCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := a.alerting.SendNotification(alertCtx, scheduledIngestionNotification(run)); err != nil {
		a.schedulerLogger.Error("failed to deliver scheduled ingestion completion alert", "status", run.Status, "error", err)
	}
}

func scheduledIngestionNotification(run scheduledIngestionRun) alerting.Notification {
	filesProcessed, filesSkipped, skippedOld, recordsParsed, recordsWithinLookback, recordsSkippedOld, recordsInserted := 0, 0, 0, 0, 0, 0, 0
	for _, result := range run.Results {
		filesProcessed += result.FilesProcessed
		filesSkipped += result.FilesSkipped
		skippedOld += result.SkippedOldFiles
		recordsParsed += result.RecordsParsed
		recordsWithinLookback += result.RecordsWithinLookback
		recordsSkippedOld += result.RecordsSkippedOld
		recordsInserted += result.RecordsInserted
	}
	severity := "success"
	switch run.Status {
	case "partial_failure":
		severity = "warning"
	case "failed":
		severity = "error"
	}
	message := fmt.Sprintf("Scheduled ingestion finished with status %s.", run.Status)
	if run.Error != nil {
		message += " Error: " + run.Error.Error()
	}
	return alerting.Notification{
		Title:    "Torvix Scheduled Ingestion " + titleForScheduledIngestion(run.Status),
		Severity: severity,
		Message:  message,
		Fields: []alerting.NotificationField{
			{Name: "Files processed", Value: strconv.Itoa(filesProcessed)},
			{Name: "Files skipped", Value: strconv.Itoa(filesSkipped)},
			{Name: "Old files skipped", Value: strconv.Itoa(skippedOld)},
			{Name: "Records parsed", Value: strconv.Itoa(recordsParsed)},
			{Name: "Records within lookback", Value: strconv.Itoa(recordsWithinLookback)},
			{Name: "Old records skipped", Value: strconv.Itoa(recordsSkippedOld)},
			{Name: "Records inserted", Value: strconv.Itoa(recordsInserted)},
			{Name: "Duration", Value: fmt.Sprintf("%.1fs", run.DurationSeconds)},
		},
	}
}

func titleForScheduledIngestion(status string) string {
	switch status {
	case "success":
		return "Succeeded"
	case "partial_failure":
		return "Partially Failed"
	case "failed":
		return "Failed"
	default:
		return status
	}
}

func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func domainProvider(provider string) domain.Provider {
	return domain.Provider(provider)
}

func (a *App) Shutdown(ctx context.Context) error {
	a.cancelScheduler()
	a.stopOnce.Do(func() { close(a.stop) })
	return a.server.Shutdown(ctx)
}

func (a *App) Close() error {
	a.repo.Close()
	return nil
}

func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).String())
	})
}
