import { type Component, For } from 'solid-js';
import { alerts, dismissAlert } from '../../stores/alerts';

const driftColors: Record<string, string> = {
  FIELD_MISSING: 'border-danger bg-danger/10 text-danger',
  TYPE_CHANGED: 'border-danger bg-danger/10 text-danger',
  NEW_FIELD: 'border-warning bg-warning/10 text-warning',
};

const DriftAlert: Component = () => {
  return (
    <div class="fixed top-4 right-2 sm:right-4 z-50 space-y-2 w-64 sm:w-80">
      <For each={alerts()}>
        {(alert) => (
          <div
            class={`border rounded-lg p-3 shadow-lg backdrop-blur-sm transition-all ${
              driftColors[alert.drift_type] || 'border-muted bg-muted/10'
            }`}
          >
            <div class="flex items-start justify-between">
              <div>
                <div class="text-xs font-semibold uppercase tracking-wider">
                  {alert.drift_type.replace('_', ' ')}
                </div>
                <div class="text-sm mt-1 font-mono opacity-90">{alert.field_name}</div>
                <div class="text-xs mt-1 opacity-60">{alert.source_id}</div>
              </div>
              <button
                class="text-xs opacity-50 hover:opacity-100 ml-2"
                onClick={() => dismissAlert(alert.id)}
              >
                ✕
              </button>
            </div>
          </div>
        )}
      </For>
    </div>
  );
};

export default DriftAlert;
