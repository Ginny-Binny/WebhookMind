package pubsub

const (
	Channel = "webhookmind:events"

	EventWebhookReceived    = "webhook.received"
	EventWebhookDelivered   = "webhook.delivered"
	EventExtractionComplete = "extraction.complete"
	EventSchemaDrift        = "schema.drift"
	EventDeliveryFailed     = "delivery.failed"
	EventDLQAdded           = "dlq.added"
	EventMetricsUpdate      = "metrics.update"
	EventReplayProgress     = "replay.progress"

	MetricsThroughputKey = "webhookmind:metrics:tput"
	MetricsLatencyKey    = "webhookmind:metrics:lat"
)
