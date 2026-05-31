package app

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"

	"github.com/crypticani/torvix/internal/adapters/postgres"
	metricsadapter "github.com/crypticani/torvix/internal/adapters/prometheus"
	ociadapter "github.com/crypticani/torvix/internal/adapters/providers/oci"
	"github.com/crypticani/torvix/internal/config"
	"github.com/crypticani/torvix/internal/core/alerting"
	"github.com/crypticani/torvix/internal/core/analytics"
	"github.com/crypticani/torvix/internal/core/collect"
	"github.com/crypticani/torvix/internal/core/forecasting"
	"github.com/crypticani/torvix/internal/core/normalize"
	"github.com/crypticani/torvix/internal/core/reporting"
	"github.com/crypticani/torvix/internal/logging"
	httpapi "github.com/crypticani/torvix/internal/ports/http"
	"github.com/crypticani/torvix/internal/ports/providers"
)

type App struct {
	server          *http.Server
	repo            *postgres.Repository
	alerting        *alerting.Service
	reporting       *reporting.Service
	collector       *collect.Service
	schedulerCfg    config.Scheduler
	logger          *slog.Logger
	schedulerLogger *slog.Logger
	stop            chan struct{}
	stopOnce        sync.Once
	schedulerCtx    context.Context
	cancelScheduler context.CancelFunc
}

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	return NewWithLoggers(cfg, logging.Loggers{
		App:       logger,
		HTTP:      logger,
		Ingestion: logger,
		DB:        logger,
		OCI:       logger,
		Scheduler: logger,
		Alerting:  logger,
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
	migrationCtx, cancelMigration := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancelMigration()
	if err := postgres.NewMigrator(repo.Pool(), "migrations").Run(migrationCtx); err != nil {
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
	reportingSvc := reporting.New(analyticsSvc, forecastingSvc)
	alertingSvc := alerting.NewWithLogger(&http.Client{Timeout: 10 * time.Second}, cfg.Reporting.Webhooks, loggers.Alerting)
	handler := httpapi.NewWithOptions(collectorSvc, analyticsSvc, forecastingSvc, reportingSvc, alertingSvc, reg, httpapi.HandlerOptions{
		LookbackDays:       cfg.Ingestion.LookbackDays,
		RetentionDays:      cfg.Ingestion.RetentionDays,
		GrafanaAuthEnabled: cfg.Grafana.APIAuth.Enabled,
		GrafanaAuthToken:   cfg.Grafana.APIAuth.BearerToken,
		GrafanaMetrics:     metrics,
		Logger:             loggers.HTTP,
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
		schedulerCfg:    cfg.Scheduler,
		logger:          loggers.App,
		schedulerLogger: loggers.Scheduler,
		stop:            make(chan struct{}),
		schedulerCtx:    schedulerCtx,
		cancelScheduler: cancelScheduler,
	}, nil
}

func (a *App) Start() error {
	if a.schedulerCfg.Enabled {
		go a.runScheduler()
	}
	return a.server.ListenAndServe()
}

func (a *App) runScheduler() {
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
			results, err := a.collector.Run(a.schedulerCtx, time.Time{})
			if err != nil {
				a.schedulerLogger.Error("scheduled ingestion failed", "error", err)
			}
			if shouldDeliverScheduledReports(results, err) {
				a.deliverScheduledReports(a.schedulerCtx)
			}
		case <-a.stop:
			return
		}
	}
}

func shouldDeliverScheduledReports(results []collect.ProviderResult, err error) bool {
	return err == nil && len(results) > 0
}

func (a *App) deliverScheduledReports(ctx context.Context) {
	if a.reporting == nil || a.alerting == nil {
		return
	}
	reportCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	for _, result := range a.reporting.DeliverDefaultReports(reportCtx, a.alerting, time.Now().UTC()) {
		if result.Error != nil && a.logger != nil {
			a.logger.Error("failed to deliver scheduled report", "period", result.Period, "from", result.From, "to", result.To, "error", result.Error)
		}
	}
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
