package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var meter = otel.Meter("veloria")

// Application metrics — registered via InitMetrics().
var (
	HTTPDuration      metric.Float64Histogram
	SearchQueueSize   metric.Int64UpDownCounter
	SearchesCompleted metric.Int64Counter
	PluginCount       metric.Int64Gauge
	ThemeCount        metric.Int64Gauge
	CoreCount         metric.Int64Gauge

	// Custom metrics
	SearchCount        metric.Int64Counter
	SearchDuration     metric.Float64Histogram
	MCPToolUseCount    metric.Int64Counter
	MCPToolUseDuration metric.Float64Histogram

	// Indexer metrics
	IndexingTasksTotal     metric.Int64Counter
	IndexingTaskDuration   metric.Float64Histogram
	CircuitBreakerChanges  metric.Int64Counter
	AdhocQueueLength       metric.Int64Gauge
	ConsecutiveFailures    metric.Int64Gauge
	LastSuccessfulUpdateTS metric.Float64Gauge
)

// InitMetrics registers all application metric instruments.
func InitMetrics() error {
	var err error

	HTTPDuration, err = meter.Float64Histogram("http_response_time_seconds",
		metric.WithDescription("Duration of HTTP requests."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	SearchQueueSize, err = meter.Int64UpDownCounter("search_queue_size",
		metric.WithDescription("The search queue size."),
	)
	if err != nil {
		return err
	}

	SearchesCompleted, err = meter.Int64Counter("searches_completed_total",
		metric.WithDescription("The total number of completed searches."),
	)
	if err != nil {
		return err
	}

	PluginCount, err = meter.Int64Gauge("plugin_count",
		metric.WithDescription("The number of active plugins."),
	)
	if err != nil {
		return err
	}

	ThemeCount, err = meter.Int64Gauge("theme_count",
		metric.WithDescription("The number of active themes."),
	)
	if err != nil {
		return err
	}

	CoreCount, err = meter.Int64Gauge("core_count",
		metric.WithDescription("The number of active core versions."),
	)
	if err != nil {
		return err
	}

	SearchCount, err = meter.Int64Counter("search_count",
		metric.WithDescription("Total number of search requests."),
	)
	if err != nil {
		return err
	}

	SearchDuration, err = meter.Float64Histogram("search_duration_seconds",
		metric.WithDescription("Duration of search operations."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	MCPToolUseCount, err = meter.Int64Counter("mcp_tool_use_count",
		metric.WithDescription("Total number of MCP tool invocations."),
	)
	if err != nil {
		return err
	}

	MCPToolUseDuration, err = meter.Float64Histogram("mcp_tool_use_duration_seconds",
		metric.WithDescription("Duration of MCP tool invocations."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	IndexingTasksTotal, err = meter.Int64Counter("indexing_tasks_total",
		metric.WithDescription("Total number of indexing tasks by status and repo type."),
	)
	if err != nil {
		return err
	}

	IndexingTaskDuration, err = meter.Float64Histogram("indexing_task_duration_seconds",
		metric.WithDescription("Duration of individual indexing tasks."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}

	CircuitBreakerChanges, err = meter.Int64Counter("circuit_breaker_state_changes_total",
		metric.WithDescription("Number of circuit breaker state transitions."),
	)
	if err != nil {
		return err
	}

	AdhocQueueLength, err = meter.Int64Gauge("indexer_adhoc_queue_length",
		metric.WithDescription("Current number of pending ad-hoc reindex requests."),
	)
	if err != nil {
		return err
	}

	ConsecutiveFailures, err = meter.Int64Gauge("datasource_consecutive_failures",
		metric.WithDescription("Number of consecutive PrepareUpdates failures per data source."),
	)
	if err != nil {
		return err
	}

	LastSuccessfulUpdateTS, err = meter.Float64Gauge("datasource_last_success_timestamp",
		metric.WithDescription("Unix timestamp of last successful update per data source."),
	)
	if err != nil {
		return err
	}

	return nil
}
