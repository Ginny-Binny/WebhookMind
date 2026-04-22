import { type Component, createSignal, Show } from 'solid-js';
import { startReplay, fetchReplay } from '../../lib/api';
import type { ReplaySession } from '../../lib/types';

const ReplayPanel: Component<{ sourceId: string }> = (props) => {
  const [destUrl, setDestUrl] = createSignal('');
  const [fromTs, setFromTs] = createSignal('');
  const [session, setSession] = createSignal<ReplaySession | null>(null);
  const [loading, setLoading] = createSignal(false);

  const handleStart = async () => {
    if (!destUrl() || !fromTs()) return;
    setLoading(true);
    try {
      const { id } = await startReplay(props.sourceId, destUrl(), new Date(fromTs()).toISOString());
      const poll = setInterval(async () => {
        const s = await fetchReplay(id);
        setSession(s);
        if (s.status === 'completed' || s.status === 'failed' || s.status === 'cancelled') {
          clearInterval(poll);
        }
      }, 2000);
    } catch (e) {
      console.error(e);
    }
    setLoading(false);
  };

  const progress = () => {
    const s = session();
    if (!s || !s.events_total) return 0;
    return Math.round((s.events_replayed / s.events_total) * 100);
  };

  return (
    <div class="bg-card border border-border rounded-lg p-3 sm:p-4">
      <h3 class="text-sm font-semibold text-white mb-4">Time Machine Replay</h3>
      <div class="space-y-3">
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label class="text-xs text-muted block mb-1">Destination URL</label>
            <input class="w-full bg-bg border border-border rounded px-2 py-1.5 text-xs text-white" placeholder="https://..." value={destUrl()} onInput={(e) => setDestUrl(e.target.value)} />
          </div>
          <div>
            <label class="text-xs text-muted block mb-1">Replay From</label>
            <input type="datetime-local" class="w-full bg-bg border border-border rounded px-2 py-1.5 text-xs text-white" value={fromTs()} onInput={(e) => setFromTs(e.target.value)} />
          </div>
        </div>
        <button
          class="w-full py-2 rounded bg-accent/20 text-accent hover:bg-accent/40 text-xs font-medium transition-colors disabled:opacity-50"
          onClick={handleStart}
          disabled={loading() || !destUrl() || !fromTs()}
        >
          {loading() ? 'Starting...' : 'Start Replay'}
        </button>

        <Show when={session()}>
          <div class="bg-surface rounded-lg p-3">
            <div class="flex items-center justify-between text-xs mb-2">
              <span class={`font-semibold ${
                session()!.status === 'completed' ? 'text-success' :
                session()!.status === 'failed' ? 'text-danger' : 'text-accent'
              }`}>
                {session()!.status.toUpperCase()}
              </span>
              <span class="text-muted">
                {session()!.events_replayed} / {session()!.events_total || '?'} events
              </span>
            </div>
            <div class="w-full bg-bg rounded-full h-2">
              <div class="bg-accent h-2 rounded-full transition-all" style={{ width: `${progress()}%` }} />
            </div>
          </div>
        </Show>
      </div>
    </div>
  );
};

export default ReplayPanel;
