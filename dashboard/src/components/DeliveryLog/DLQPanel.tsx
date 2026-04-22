import { type Component, createResource, For, Show } from 'solid-js';
import { fetchDLQ, retryDLQ, discardDLQ } from '../../lib/api';

const DLQPanel: Component = () => {
  const [entries, { refetch }] = createResource(() => fetchDLQ());

  const handleRetry = async (eventId: string) => {
    try {
      await retryDLQ(eventId);
      refetch();
    } catch (e) {
      console.error('Retry failed:', e);
    }
  };

  const handleDiscard = async (eventId: string) => {
    try {
      await discardDLQ(eventId);
      refetch();
    } catch (e) {
      console.error('Discard failed:', e);
    }
  };

  return (
    <div class="bg-card border border-border rounded-lg p-3 sm:p-4">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-sm font-semibold text-white">Dead Letter Queue</h3>
        <button class="text-xs text-accent hover:text-white transition-colors" onClick={() => refetch()}>
          Refresh
        </button>
      </div>
      <Show when={entries() && entries()!.length > 0} fallback={
        <div class="text-sm text-muted text-center py-8">No failed deliveries</div>
      }>
        {/* Desktop: table */}
        <div class="hidden sm:block overflow-x-auto">
          <table class="w-full text-xs min-w-[600px]">
            <thead>
              <tr class="text-muted border-b border-border">
                <th class="text-left py-2 font-medium">Event ID</th>
                <th class="text-left py-2 font-medium">Source</th>
                <th class="text-left py-2 font-medium">Reason</th>
                <th class="text-left py-2 font-medium">Failed At</th>
                <th class="text-right py-2 font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              <For each={entries()}>
                {(entry) => (
                  <tr class="border-b border-border/50 hover:bg-surface">
                    <td class="py-2 font-mono text-accent">{entry.event_id.slice(0, 8)}</td>
                    <td class="py-2">{entry.source_id}</td>
                    <td class="py-2 text-danger max-w-[200px] truncate">{entry.failure_reason}</td>
                    <td class="py-2 text-muted whitespace-nowrap">{new Date(entry.failed_at).toLocaleTimeString()}</td>
                    <td class="py-2 text-right space-x-2 whitespace-nowrap">
                      <button class="px-2 py-0.5 rounded bg-accent/20 text-accent hover:bg-accent/40 transition-colors" onClick={() => handleRetry(entry.event_id)}>Retry</button>
                      <button class="px-2 py-0.5 rounded bg-danger/20 text-danger hover:bg-danger/40 transition-colors" onClick={() => handleDiscard(entry.event_id)}>Discard</button>
                    </td>
                  </tr>
                )}
              </For>
            </tbody>
          </table>
        </div>

        {/* Mobile: card layout */}
        <div class="sm:hidden space-y-2">
          <For each={entries()}>
            {(entry) => (
              <div class="bg-surface rounded-lg p-3 space-y-2">
                <div class="flex items-center justify-between">
                  <span class="text-xs font-mono text-accent">{entry.event_id.slice(0, 8)}</span>
                  <span class="text-[10px] text-muted">{new Date(entry.failed_at).toLocaleTimeString()}</span>
                </div>
                <div class="text-[10px] text-muted">{entry.source_id}</div>
                <div class="text-[10px] text-danger truncate">{entry.failure_reason}</div>
                <div class="flex gap-2">
                  <button class="flex-1 px-2 py-1 rounded bg-accent/20 text-accent hover:bg-accent/40 text-[10px] transition-colors" onClick={() => handleRetry(entry.event_id)}>Retry</button>
                  <button class="flex-1 px-2 py-1 rounded bg-danger/20 text-danger hover:bg-danger/40 text-[10px] transition-colors" onClick={() => handleDiscard(entry.event_id)}>Discard</button>
                </div>
              </div>
            )}
          </For>
        </div>
      </Show>
    </div>
  );
};

export default DLQPanel;
