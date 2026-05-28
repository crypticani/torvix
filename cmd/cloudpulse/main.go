package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/crypticani/cloudpulse/docs"
	"github.com/crypticani/cloudpulse/internal/app"
	"github.com/crypticani/cloudpulse/internal/config"
	"github.com/crypticani/cloudpulse/internal/logging"
)

//	@title			CloudPulse API
//	@version		0.6.0
//	@description	Multi-cloud FinOps cost analytics platform. Collects billing data from AWS, Azure, GCP, and OCI, normalises it into a canonical schema, and provides real-time cost summaries, anomaly detection, forecasting, and automated reporting.

//	@contact.name	CloudPulse Team

//	@host			localhost:8080
//	@BasePath		/
//	@schemes		http

func main() {
	cfgPath := flag.String("config", "configs/config.yaml", "path to YAML config")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		panic(err)
	}

	logManager, err := logging.NewManager(logging.Config{
		Level:         cfg.Logging.Level,
		Dir:           cfg.Logging.Dir,
		RetentionDays: cfg.Logging.RetentionDays,
	})
	if err != nil {
		panic(err)
	}
	defer logManager.Close()
	loggers := logManager.Loggers()
	logger := loggers.App
	slog.SetDefault(logger)

	svc, err := app.NewWithLoggers(cfg, loggers)
	if err != nil {
		logger.Error("bootstrap failed", "error", err)
		_ = logManager.Close()
		os.Exit(1)
	}
	defer svc.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go logManager.RunRetentionCleanup(ctx, 24*time.Hour)

	go func() {
		logger.Info("http server starting", "addr", cfg.HTTP.Address)
		if err := svc.Start(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := svc.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown failed", "error", err)
	}
}
