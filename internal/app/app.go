package app

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"

	"github.com/crypticani/cloudpulse/internal/adapters/postgres"
	metricsadapter "github.com/crypticani/cloudpulse/internal/adapters/prometheus"
	awsadapter "github.com/crypticani/cloudpulse/internal/adapters/providers/aws"
	azureadapter "github.com/crypticani/cloudpulse/internal/adapters/providers/azure"
	gcpadapter "github.com/crypticani/cloudpulse/internal/adapters/providers/gcp"
	ociadapter "github.com/crypticani/cloudpulse/internal/adapters/providers/oci"
	"github.com/crypticani/cloudpulse/internal/config"
	"github.com/crypticani/cloudpulse/internal/core/alerting"
	"github.com/crypticani/cloudpulse/internal/core/analytics"
	"github.com/crypticani/cloudpulse/internal/core/collect"
	"github.com/crypticani/cloudpulse/internal/core/forecasting"
	"github.com/crypticani/cloudpulse/internal/core/normalize"
	"github.com/crypticani/cloudpulse/internal/core/reporting"
	httpapi "github.com/crypticani/cloudpulse/internal/ports/http"
	"github.com/crypticani/cloudpulse/internal/ports/providers"
)

type App struct {
	server          *http.Server
	repo            *postgres.Repository
	alerting        *alerting.Service
	collector       *collect.Service
	schedulerCfg    config.Scheduler
	logger          *slog.Logger
	stop            chan struct{}
	stopOnce        sync.Once
	schedulerCtx    context.Context
	cancelScheduler context.CancelFunc
}

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	repo, err := postgres.New(ctx, cfg.DB.DSN, cfg.DB.MaxConns, cfg.DB.MinConns)
	if err != nil {
		return nil, err
	}
	if err := postgres.NewMigrator(repo.Pool(), "migrations").Run(ctx); err != nil {
		repo.Close()
		return nil, err
	}

	var collectors []providers.Collector
	if cfg.Providers.AWS.Enabled {
		collectors = append(collectors, awsadapter.New(cfg.Providers.AWS))
	}
	if cfg.Providers.Azure.Enabled {
		collectors = append(collectors, azureadapter.New(cfg.Providers.Azure))
	}
	if cfg.Providers.GCP.Enabled {
		collectors = append(collectors, gcpadapter.New(cfg.Providers.GCP))
	}
	if cfg.Providers.OCI.Enabled {
		ociCollector, err := ociadapter.New(cfg.Providers.OCI, logger, repo)
		if err != nil {
			return nil, err
		}
		collectors = append(collectors, ociCollector)
	}

	reg := prom.NewRegistry()
	metrics := metricsadapter.New(cfg.Metrics.Namespace, reg)

	normalizer := normalize.New()
	collectorSvc := collect.New(logger, repo, normalizer, collectors, metrics)
	analyticsSvc := analytics.New(repo)
	forecastingSvc := forecasting.New(repo)
	reportingSvc := reporting.New(analyticsSvc, forecastingSvc)
	alertingSvc := alerting.New(&http.Client{Timeout: 10 * time.Second}, cfg.Reporting.Webhooks)
	handler := httpapi.New(collectorSvc, analyticsSvc, forecastingSvc, reportingSvc, alertingSvc, reg)

	schedulerCtx, cancelScheduler := context.WithCancel(context.Background())
	return &App{
		server: &http.Server{
			Addr:              cfg.HTTP.Address,
			Handler:           requestLogger(logger, handler),
			ReadHeaderTimeout: 5 * time.Second,
		},
		repo:            repo,
		alerting:        alertingSvc,
		collector:       collectorSvc,
		schedulerCfg:    cfg.Scheduler,
		logger:          logger,
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
		a.logger.Error("invalid ingest interval", "error", err)
		return
	}
	a.logger.Info("starting ingestion scheduler", "interval", d.String())
	ticker := time.NewTicker(d)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.logger.Info("scheduler triggered ingestion")
			_, err := a.collector.Run(a.schedulerCtx, time.Now().UTC().AddDate(0, 0, -7))
			if err != nil {
				a.logger.Error("scheduled ingestion failed", "error", err)
			}
		case <-a.stop:
			return
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
