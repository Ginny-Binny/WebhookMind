export interface Source {
  id: string;
  name: string;
}

export interface WebhookListItem {
  event_id: string;
  source_id: string;
  received_at: string;
  status_code: number;
  success: boolean;
  duration_ms: number;
  has_extraction: boolean;
  extraction_cache_hit: boolean;
}

export interface DeliveryAttempt {
  id: string;
  event_id: string;
  source_id: string;
  destination_id: string;
  attempt_number: number;
  attempted_at: string;
  status_code: number;
  success: boolean;
  error_message: string;
  duration_ms: number;
}

export interface ExtractionRecord {
  id: string;
  event_id: string;
  source_id: string;
  file_url: string;
  file_type: string;
  template_id: string;
  cache_hit: boolean;
  extracted_data: Record<string, unknown>;
  duration_ms: number;
  success: boolean;
  error_message: string;
}

export interface WebhookDetail {
  event_id: string;
  source_id: string;
  raw_body?: string;
  attempts: DeliveryAttempt[];
  extraction?: ExtractionRecord;
  diff?: DiffData;
}

export interface DiffData {
  added: Record<string, unknown>;
  removed: Record<string, unknown>;
  changed: { field: string; old_value: unknown; new_value: unknown }[];
}

export interface DriftEvent {
  id: string;
  event_id: string;
  source_id: string;
  drift_type: 'FIELD_MISSING' | 'TYPE_CHANGED' | 'NEW_FIELD';
  field_name: string;
  expected_type: string;
  actual_type: string;
  detected_at: string;
}

export interface PayloadSchema {
  source_id: string;
  schema_data: Record<string, FieldSchema>;
  sample_count: number;
  is_locked: boolean;
  version: number;
}

export interface FieldSchema {
  name: string;
  type: string;
  nullable: boolean;
  examples: string[];
}

export interface DeadLetterEntry {
  id: string;
  event_id: string;
  source_id: string;
  destination_id: string;
  failed_at: string;
  failure_reason: string;
  resolved: boolean;
}

export interface RoutingRule {
  id: string;
  source_id: string;
  destination_id: string;
  name: string;
  priority: number;
  logic_operator: string;
  conditions: RuleCondition[];
  is_active: boolean;
}

export interface RuleCondition {
  field: string;
  operator: string;
  value: unknown;
}

export interface ReplaySession {
  id: string;
  source_id: string;
  destination_url: string;
  from_timestamp: string;
  status: 'running' | 'paused' | 'completed' | 'cancelled' | 'failed';
  events_replayed: number;
  events_total: number;
  started_at: string;
  initiated_by: string;
}

export interface MetricPoint {
  timestamp: string;
  value: number;
}

// SSE event payloads
export interface SSEWebhookReceived {
  event_id: string;
  source_id: string;
  received_at: string;
}

export interface SSEWebhookDelivered {
  event_id: string;
  source_id: string;
  destination_id: string;
  status_code: number;
  duration_ms: number;
}

export interface SSEDrift {
  source_id: string;
  event_id: string;
  drift_type: string;
  field_name: string;
}
