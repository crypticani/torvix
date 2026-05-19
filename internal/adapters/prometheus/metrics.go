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
	InsertedBatchesTotal  *prometheus.CounterVec
	RecordsPerSecond      *prometheus.GaugeVec
	RecordsDeletedTotal   prometheus.Counter
	CompressedChunksTotal prometheus.Counter
	IngestionFailures     *prometheus.CounterVec
	IngestionDuration     *prometheus.HistogramVec
	DatabaseBackendInfo   *prometheus.GaugeVec
	CostTotal             *prometheus.GaugeVec
	CostServiceCount      *prometheus.GaugeVec
	CostAnomalyCount      *prometheus.GaugeVec
}

func New(namespace string, reg prometheus.Registerer, costStatsEnabled bool) *Metrics {
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
		InsertedBatchesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "inserted_batches_total",
			Help:      "Total ingestion batches inserted.",
		}, []string{"provider"}),
		RecordsPerSecond: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "ingestion_records_per_second",
			Help:      "Last observed ingestion record rate.",
		}, []string{"provider"}),
		RecordsDeletedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "records_deleted_total",
			Help:      "Total cost records deleted by rolling retention maintenance.",
		}),
		CompressedChunksTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "compressed_chunks_total",
			Help:      "Total TimescaleDB chunks compressed by rolling-window maintenance.",
		}),
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
	collectors := []prometheus.Collector{
		m.CollectRuns,
		m.CollectLatency,
		m.AnomaliesFound,
		m.ProcessedFilesTotal,
		m.ProcessedRecordsTotal,
		m.InsertedBatchesTotal,
		m.RecordsPerSecond,
		m.RecordsDeletedTotal,
		m.CompressedChunksTotal,
		m.IngestionFailures,
		m.IngestionDuration,
		m.DatabaseBackendInfo,
	}
	if costStatsEnabled {
		m.CostTotal = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cost_total",
			Help:      "Last observed total cost for a Grafana aggregate query.",
		}, []string{"window"})
		m.CostServiceCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cost_services",
			Help:      "Last observed distinct service count for a Grafana aggregate query.",
		}, []string{"window"})
		m.CostAnomalyCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "cost_anomalies",
			Help:      "Last observed anomaly count for a Grafana aggregate query.",
		}, []string{"window"})
		collectors = append(collectors, m.CostTotal, m.CostServiceCount, m.CostAnomalyCount)
	}
	reg.MustRegister(collectors...)
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

func (m *Metrics) ObserveBatches(provider string, count int) {
	if count <= 0 {
		return
	}
	m.InsertedBatchesTotal.WithLabelValues(provider).Add(float64(count))
}

func (m *Metrics) ObserveRecordsPerSecond(provider string, rate float64) {
	if rate < 0 {
		return
	}
	m.RecordsPerSecond.WithLabelValues(provider).Set(rate)
}

func (m *Metrics) ObserveRecordsDeleted(count int64) {
	if count <= 0 {
		return
	}
	m.RecordsDeletedTotal.Add(float64(count))
}

func (m *Metrics) ObserveCompressedChunks(count int64) {
	if count <= 0 {
		return
	}
	m.CompressedChunksTotal.Add(float64(count))
}

func (m *Metrics) ObserveFailure(provider, stage string, count int) {
	if count <= 0 {
		return
	}
	m.IngestionFailures.WithLabelValues(provider, stage).Add(float64(count))
}

func (m *Metrics) ObserveGrafanaCostStats(window string, totalCost float64, serviceCount, anomalyCount int) {
	if m.CostTotal == nil || window == "" {
		return
	}
	m.CostTotal.WithLabelValues(window).Set(totalCost)
	m.CostServiceCount.WithLabelValues(window).Set(float64(serviceCount))
	m.CostAnomalyCount.WithLabelValues(window).Set(float64(anomalyCount))
}
