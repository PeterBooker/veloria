package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_response_time_seconds",
		Help:    "Duration of HTTP requests.",
		Buckets: []float64{.001, .002, .003, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
	}, []string{"method", "route", "status"})

	searchQueueSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "search_queue_size",
		Help: "The search queue size.",
	})

	searchesCompleted = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "searches_completed_total",
		Help: "The total number of completed searches",
	}, []string{"private"})

	pluginCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "plugin_count",
		Help: "The number of active plugins.",
	})

	themeCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "theme_count",
		Help: "The number of active themes.",
	})

	coreCount = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "core_count",
		Help: "The number of active core versions.",
	})
)

func init() {
	prometheus.MustRegister(
		httpDuration,

		searchQueueSize,
		searchesCompleted,

		pluginCount,
		themeCount,
		coreCount,
	)
}

func GetHttpDuration() *prometheus.HistogramVec {
	return httpDuration
}

func GetSearchQueueSize() prometheus.Gauge {
	return searchQueueSize
}

func GetSearchesCompleted() *prometheus.CounterVec {
	return searchesCompleted
}

func GetPluginCount() prometheus.Gauge {
	return pluginCount
}

func GetThemeCount() prometheus.Gauge {
	return themeCount
}

func GetCoreCount() prometheus.Gauge {
	return coreCount
}
