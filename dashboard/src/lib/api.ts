import type {
  Source, WebhookListItem, WebhookDetail, DeadLetterEntry,
  DriftEvent, PayloadSchema, RoutingRule, ReplaySession, MetricPoint,
} from './types';

const BASE = '/api';

async function get<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`);
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

async function post<T>(path: string, body?: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

async function put<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

async function del<T>(path: string): Promise<T> {
  const res = await fetch(`${BASE}${path}`, { method: 'DELETE' });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

// Sources & Webhooks
export const fetchSources = () => get<Source[]>('/sources');
export const fetchWebhooks = (sourceId: string, limit = 50, offset = 0) =>
  get<WebhookListItem[]>(`/sources/${sourceId}/webhooks?limit=${limit}&offset=${offset}`);
export const fetchWebhookDetail = (eventId: string, sourceId?: string) =>
  get<WebhookDetail>(
    sourceId
      ? `/webhooks/${eventId}?source_id=${encodeURIComponent(sourceId)}`
      : `/webhooks/${eventId}`,
  );

// Schema & Drift
export const fetchSchema = (sourceId: string) =>
  get<PayloadSchema>(`/sources/${sourceId}/schema`);
export const fetchDrifts = (sourceId: string, limit = 50) =>
  get<DriftEvent[]>(`/sources/${sourceId}/drifts?limit=${limit}`);
export const fetchDiffs = (sourceId: string, limit = 50) =>
  get<unknown[]>(`/sources/${sourceId}/diffs?limit=${limit}`);

// DLQ
export const fetchDLQ = (sourceId?: string) =>
  get<DeadLetterEntry[]>(`/dlq${sourceId ? `?source_id=${sourceId}` : ''}`);
export const retryDLQ = (eventId: string) =>
  post<{ status: string }>(`/dlq/${eventId}/retry`);
export const discardDLQ = (eventId: string) =>
  post<{ status: string }>(`/dlq/${eventId}/discard`);

// Metrics
export const fetchThroughput = (rangeMin = 60) =>
  get<MetricPoint[]>(`/metrics/throughput?range=${rangeMin}`);
export const fetchLatency = (rangeMin = 60) =>
  get<MetricPoint[]>(`/metrics/latency?range=${rangeMin}`);

// Routing Rules
export const fetchRules = (sourceId: string) =>
  get<RoutingRule[]>(`/sources/${sourceId}/rules`);
export const createRule = (sourceId: string, rule: Partial<RoutingRule>) =>
  post<{ id: string }>(`/sources/${sourceId}/rules`, rule);
export const updateRule = (ruleId: string, rule: Partial<RoutingRule>) =>
  put<{ status: string }>(`/rules/${ruleId}`, rule);
export const deleteRule = (ruleId: string) =>
  del<{ status: string }>(`/rules/${ruleId}`);

// Replay
export const startReplay = (sourceId: string, destinationUrl: string, fromTimestamp: string, initiatedBy = 'dashboard') =>
  post<{ id: string }>(`/sources/${sourceId}/replay`, {
    destination_url: destinationUrl,
    from_timestamp: fromTimestamp,
    initiated_by: initiatedBy,
  });
export const fetchReplay = (sessionId: string) =>
  get<ReplaySession>(`/replay/${sessionId}`);
export const pauseReplay = (sessionId: string) =>
  post<{ status: string }>(`/replay/${sessionId}/pause`);
export const resumeReplay = (sessionId: string) =>
  post<{ status: string }>(`/replay/${sessionId}/resume`);
