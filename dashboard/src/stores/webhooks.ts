import { createStore, produce } from 'solid-js/store';
import type { SSEWebhookReceived, SSEWebhookDelivered } from '../lib/types';

export interface StreamEvent {
  event_id: string;
  source_id: string;
  received_at: string;
  status: 'received' | 'delivered' | 'failed';
  status_code?: number;
  duration_ms?: number;
}

const MAX_EVENTS = 200;

const [webhooks, setWebhooks] = createStore<StreamEvent[]>([]);

export function addWebhookReceived(data: SSEWebhookReceived) {
  setWebhooks(produce((w) => {
    w.unshift({
      event_id: data.event_id,
      source_id: data.source_id,
      received_at: data.received_at,
      status: 'received',
    });
    if (w.length > MAX_EVENTS) w.pop();
  }));
}

export function updateWebhookDelivered(data: SSEWebhookDelivered) {
  setWebhooks(produce((w) => {
    const idx = w.findIndex((e) => e.event_id === data.event_id);
    if (idx >= 0) {
      w[idx].status = 'delivered';
      w[idx].status_code = data.status_code;
      w[idx].duration_ms = data.duration_ms;
    }
  }));
}

export function markWebhookFailed(eventId: string) {
  setWebhooks(produce((w) => {
    const idx = w.findIndex((e) => e.event_id === eventId);
    if (idx >= 0) w[idx].status = 'failed';
  }));
}

export { webhooks };
