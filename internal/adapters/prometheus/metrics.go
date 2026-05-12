package prometheus

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	CollectRuns           *prometheus.CounterVec
	CollectLatency        *prometheus.HistogramVec
	AnomaliesFound        prometheus.Counter
	ProcessedFilesTotal   *prometheus.CounterVec
	ProcessedRecordsTotal *prometheus.CounterVec
	IngestionFailures     *prometheus.CounterVec
	IngestionDuration     *prometheus.HistogramVec
	DatabaseBackendInfo   *prometheus.GaugeVec
}

func New(namespace string, reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		CollectRuns: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "collector_runs_total",
			Help:      "Total collector runs.",
		}, []string{"provider", "status"}),
		CollectLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "collector_duration_seconds",
			Help:      "Collector execution time.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"provider"}),
		AnomaliesFound: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "anomalies_found_total",
			Help:      "Total anomalies detected.",
		}),
		ProcessedFilesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "processed_files_total",
			Help:      "Total billing report files processed.",
		}, []string{"provider", "status"}),
		ProcessedRecordsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "processed_records_total",
			Help:      "Total billing records processed.",
		}, []string{"provider"}),
		IngestionFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "ingestion_failures_total",
			Help:      "Total ingestion failures.",
		}, []string{"provider", "stage"}),
		IngestionDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "ingestion_duration_seconds",
			Help:      "Billing ingestion duration in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"provider"}),
		DatabaseBackendInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "database_backend_info",
			Help:      "Database backend information.",
		}, []string{"backend"}),
	}
	reg.MustRegister(
		m.CollectRuns,
		m.CollectLatency,
		m.AnomaliesFound,
		m.ProcessedFilesTotal,
		m.ProcessedRecordsTotal,
		m.IngestionFailures,
		m.IngestionDuration,
		m.DatabaseBackendInfo,
	)
	m.DatabaseBackendInfo.WithLabelValues("postgresql").Set(1)
	return m
}

func (m *Metrics) ObserveCollector(provider, status string, duration time.Duration) {
	m.CollectRuns.WithLabelValues(provider, status).Inc()
	m.CollectLatency.WithLabelValues(provider).Observe(duration.Seconds())
	m.IngestionDuration.WithLabelValues(provider).Observe(duration.Seconds())
}

func (m *Metrics) ObserveFiles(provider, status string, count int) {
	if count <= 0 {
		return
	}
	m.ProcessedFilesTotal.WithLabelValues(provider, status).Add(float64(count))
}

func (m *Metrics) ObserveRecords(provider string, count int) {
	if count <= 0 {
		return
	}
	m.ProcessedRecordsTotal.WithLabelValues(provider).Add(float64(count))
}

func (m *Metrics) ObserveFailure(provider, stage string, count int) {
	if count <= 0 {
		return
	}
	m.IngestionFailures.WithLabelValues(provider, stage).Add(float64(count))
}
