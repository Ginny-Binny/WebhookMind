import { createSignal } from 'solid-js';

export interface DriftAlert {
  id: string;
  source_id: string;
  drift_type: string;
  field_name: string;
  timestamp: number;
}

const MAX_ALERTS = 5;
const DISMISS_MS = 8000;
let alertCounter = 0;

const [alerts, setAlerts] = createSignal<DriftAlert[]>([]);

export function addDriftAlert(data: { source_id: string; drift_type: string; field_name: string }) {
  const alert: DriftAlert = {
    id: `drift-${++alertCounter}`,
    source_id: data.source_id,
    drift_type: data.drift_type,
    field_name: data.field_name,
    timestamp: Date.now(),
  };

  setAlerts((prev) => {
    const next = [alert, ...prev];
    if (next.length > MAX_ALERTS) next.pop();
    return next;
  });

  // Auto-dismiss after 8s.
  setTimeout(() => {
    setAlerts((prev) => prev.filter((a) => a.id !== alert.id));
  }, DISMISS_MS);
}

export function dismissAlert(id: string) {
  setAlerts((prev) => prev.filter((a) => a.id !== id));
}

export { alerts };
