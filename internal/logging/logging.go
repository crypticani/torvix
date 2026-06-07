package logging

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Subsystem string

const (
	SubsystemApp       Subsystem = "app"
	SubsystemHTTP      Subsystem = "http"
	SubsystemIngestion Subsystem = "ingestion"
	SubsystemDB        Subsystem = "db"
	SubsystemOCI       Subsystem = "oci"
	SubsystemAWS       Subsystem = "aws"
	SubsystemScheduler Subsystem = "scheduler"
	SubsystemAlerting  Subsystem = "alerting"
	SubsystemWaste     Subsystem = "waste"
	SubsystemAI        Subsystem = "ai"
)

type Config struct {
	Level         string
	Dir           string
	RetentionDays int
}

type Loggers struct {
	App       *slog.Logger
	HTTP      *slog.Logger
	Ingestion *slog.Logger
	DB        *slog.Logger
	OCI       *slog.Logger
	AWS       *slog.Logger
	Scheduler *slog.Logger
	Alerting  *slog.Logger
	Waste     *slog.Logger
	AI        *slog.Logger
}

func (l Loggers) WithDefaults() Loggers {
	fallback := l.App
	if fallback == nil {
		fallback = slog.Default()
	}
	if l.App == nil {
		l.App = fallback
	}
	if l.HTTP == nil {
		l.HTTP = fallback
	}
	if l.Ingestion == nil {
		l.Ingestion = fallback
	}
	if l.DB == nil {
		l.DB = fallback
	}
	if l.OCI == nil {
		l.OCI = fallback
	}
	if l.AWS == nil {
		l.AWS = fallback
	}
	if l.Scheduler == nil {
		l.Scheduler = fallback
	}
	if l.Alerting == nil {
		l.Alerting = fallback
	}
	if l.Waste == nil {
		l.Waste = fallback
	}
	if l.AI == nil {
		l.AI = fallback
	}
	return l
}

type Manager struct {
	dir           string
	retentionDays int
	files         map[Subsystem]*os.File
	loggers       map[Subsystem]*slog.Logger
	closeOnce     sync.Once
	closeErr      error
}

func NewManager(cfg Config) (*Manager, error) {
	level := parseLevel(cfg.Level)
	dir := strings.TrimSpace(cfg.Dir)
	if dir == "" {
		dir = "logs"
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 14
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	if err := CleanupExpired(dir, cfg.RetentionDays, time.Now()); err != nil {
		return nil, err
	}

	m := &Manager{
		dir:           dir,
		retentionDays: cfg.RetentionDays,
		files:         make(map[Subsystem]*os.File),
		loggers:       make(map[Subsystem]*slog.Logger),
	}
	for _, subsystem := range []Subsystem{SubsystemApp, SubsystemHTTP, SubsystemIngestion, SubsystemDB, SubsystemOCI, SubsystemAWS, SubsystemScheduler, SubsystemAlerting, SubsystemWaste, SubsystemAI} {
		file, err := os.OpenFile(filepath.Join(dir, string(subsystem)+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			_ = m.Close()
			return nil, fmt.Errorf("open %s log: %w", subsystem, err)
		}
		m.files[subsystem] = file
		m.loggers[subsystem] = slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{Level: level})).With("subsystem", subsystem)
	}
	return m, nil
}

func (m *Manager) Logger(subsystem Subsystem) *slog.Logger {
	if logger, ok := m.loggers[subsystem]; ok {
		return logger
	}
	return m.loggers[SubsystemApp]
}

func (m *Manager) Loggers() Loggers {
	return Loggers{
		App:       m.Logger(SubsystemApp),
		HTTP:      m.Logger(SubsystemHTTP),
		Ingestion: m.Logger(SubsystemIngestion),
		DB:        m.Logger(SubsystemDB),
		OCI:       m.Logger(SubsystemOCI),
		AWS:       m.Logger(SubsystemAWS),
		Scheduler: m.Logger(SubsystemScheduler),
		Alerting:  m.Logger(SubsystemAlerting),
		Waste:     m.Logger(SubsystemWaste),
		AI:        m.Logger(SubsystemAI),
	}
}

func (m *Manager) RunRetentionCleanup(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := CleanupExpired(m.dir, m.retentionDays, time.Now()); err != nil {
				m.Logger(SubsystemApp).Error("log retention cleanup failed", "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		for _, file := range m.files {
			if err := file.Close(); err != nil && m.closeErr == nil {
				m.closeErr = err
			}
		}
	})
	return m.closeErr
}

func CleanupExpired(dir string, retentionDays int, now time.Time) error {
	if retentionDays <= 0 {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read log dir: %w", err)
	}
	cutoff := now.AddDate(0, 0, -retentionDays)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat log file %s: %w", entry.Name(), err)
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return fmt.Errorf("delete expired log file %s: %w", entry.Name(), err)
			}
		}
	}
	return nil
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
