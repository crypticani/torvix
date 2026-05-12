package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	CollectRuns    *prometheus.CounterVec
	CollectLatency *prometheus.HistogramVec
	AnomaliesFound prometheus.Counter
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
	}
	reg.MustRegister(m.CollectRuns, m.CollectLatency, m.AnomaliesFound)
	return m
}
