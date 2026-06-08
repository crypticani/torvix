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

	"github.com/crypticani/torvix/docs"
	"github.com/crypticani/torvix/internal/app"
	"github.com/crypticani/torvix/internal/config"
	"github.com/crypticani/torvix/internal/logging"
	"github.com/crypticani/torvix/internal/version"
)

//	@title			Torvix API
//	@version		1.0.0
//	@description	Torvix is an open-source FinOps platform for cloud cost visibility, anomaly detection, forecasting, and unused-resource detection across cloud providers.

//	@contact.name	Torvix Team

//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Use `Bearer <token>` when `api.auth.enabled` is true. `/healthz` and `/swagger/*` remain public.

//	@host			localhost:8080
//	@BasePath		/
//	@schemes		http

func main() {
	docs.SwaggerInfo.Version = version.Version

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
		Stdout:        cfg.Logging.Stdout,
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
