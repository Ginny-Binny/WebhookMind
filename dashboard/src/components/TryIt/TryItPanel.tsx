import { type Component, createSignal, Show } from 'solid-js';
import { sandboxSource, resetSandbox } from '../../stores/sandbox';
import { anthropicKey, setAnthropicKey, clearAnthropicKey } from '../../stores/byok';
import CopyButton from './CopyButton';

const SAMPLE_PAYLOAD = `{
  "order_id": "TRY-1",
  "amount": 500,
  "currency": "USD",
  "customer": "Demo Co"
}`;

const TryItPanel: Component = () => {
  const [payload, setPayload] = createSignal(SAMPLE_PAYLOAD);
  const [keyDraft, setKeyDraft] = createSignal(anthropicKey());
  const [keyVisible, setKeyVisible] = createSignal(false);
  const [sending, setSending] = createSignal(false);
  const [response, setResponse] = createSignal<{ ok: boolean; status: number; body: string } | null>(null);

  // Fully-qualified webhook URL for display + copy + curl. Origin = wherever the dashboard is served.
  const webhookURL = () => `${window.location.origin}/webhook/${sandboxSource()}`;

  const curlCommand = () => {
    const lines = [
      `curl -X POST ${webhookURL()} \\`,
      `  -H "Content-Type: application/json" \\`,
    ];
    if (anthropicKey()) {
      lines.push(`  -H "X-Anthropic-Key: ${anthropicKey()}" \\`);
    }
    lines.push(`  -d '${payload().replace(/\n\s*/g, ' ').trim()}'`);
    return lines.join('\n');
  };

  const handleSend = async () => {
    setSending(true);
    setResponse(null);
    try {
      const headers: Record<string, string> = { 'Content-Type': 'application/json' };
      if (anthropicKey()) {
        headers['X-Anthropic-Key'] = anthropicKey();
      }
      const res = await fetch(`/webhook/${sandboxSource()}`, {
        method: 'POST',
        headers,
        body: payload(),
      });
      const text = await res.text();
      setResponse({ ok: res.ok, status: res.status, body: text });
    } catch (err) {
      setResponse({
        ok: false,
        status: 0,
        body: err instanceof Error ? err.message : 'network error',
      });
    } finally {
      setSending(false);
    }
  };

  const handleSaveKey = () => {
    setAnthropicKey(keyDraft());
  };

  const handleForgetKey = () => {
    clearAnthropicKey();
    setKeyDraft('');
  };

  const handleResetSandbox = () => {
    if (confirm('Generate a new sandbox ID? Your current view will be cleared.')) {
      resetSandbox();
      // SSE stream auto-reconnects with the new source via the App-level effect; no reload needed.
      // Webhooks store doesn't auto-clear, but visitor will only see fresh events from now on.
      window.location.reload();
    }
  };

  return (
    <div class="flex flex-col gap-4 max-w-3xl">
      {/* --- Webhook URL --- */}
      <div class="bg-card border border-border rounded-lg p-3 sm:p-4">
        <div class="flex items-center justify-between mb-2">
          <h3 class="text-sm font-semibold text-white">Your webhook URL</h3>
          <button
            type="button"
            onClick={handleResetSandbox}
            class="text-xs text-muted hover:text-danger transition-colors"
          >
            Reset sandbox
          </button>
        </div>
        <div class="flex items-center gap-2 bg-bg border border-border rounded px-2 py-2">
          <code class="text-xs text-accent flex-1 break-all font-mono">{webhookURL()}</code>
          <CopyButton value={webhookURL()} />
        </div>
        <p class="text-xs text-muted mt-2">
          Each visitor gets a private sandbox source. Events you fire here are visible only in <em>your</em> dashboard view —
          other visitors see their own.
        </p>
      </div>

      {/* --- BYOK --- */}
      <div class="bg-card border border-border rounded-lg p-3 sm:p-4">
        <div class="flex items-center justify-between mb-2">
          <h3 class="text-sm font-semibold text-white">Bring your own Anthropic key</h3>
          <Show when={anthropicKey()}>
            <span class="text-xs text-success">Saved ✓</span>
          </Show>
        </div>
        <p class="text-xs text-muted mb-3">
          Optional. Without a key, JSON-only webhooks work fine. With a key set, requests with a <code class="text-accent">file_url</code> can be extracted by Claude.
        </p>
        <div class="flex flex-col sm:flex-row gap-2">
          <input
            type={keyVisible() ? 'text' : 'password'}
            placeholder="sk-ant-..."
            value={keyDraft()}
            onInput={(e) => setKeyDraft(e.currentTarget.value)}
            class="bg-bg border border-border rounded px-2 py-1.5 text-xs text-white flex-1 font-mono"
          />
          <div class="flex gap-2">
            <button
              type="button"
              onClick={() => setKeyVisible(!keyVisible())}
              class="text-xs px-2.5 py-1 rounded bg-surface text-muted hover:text-white transition-colors"
            >
              {keyVisible() ? 'Hide' : 'Show'}
            </button>
            <button
              type="button"
              onClick={handleSaveKey}
              disabled={!keyDraft() || keyDraft() === anthropicKey()}
              class="text-xs px-3 py-1 rounded bg-accent/20 text-accent hover:bg-accent/40 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
            >
              Save
            </button>
            <Show when={anthropicKey()}>
              <button
                type="button"
                onClick={handleForgetKey}
                class="text-xs px-3 py-1 rounded bg-danger/20 text-danger hover:bg-danger/40 transition-colors"
              >
                Forget
              </button>
            </Show>
          </div>
        </div>
        <p class="text-xs text-muted mt-2">
          Stored in your browser only. Never persisted on our server, never logged. The key is sent only as a per-request header on the specific webhook that needs it; the server strips it before storing the event. Source is open at{' '}
          <a href="https://github.com/Ginny-Binny/WebhookMind" target="_blank" rel="noopener" class="text-accent hover:underline">github.com/Ginny-Binny/WebhookMind</a>.
        </p>
      </div>

      {/* --- In-browser firer --- */}
      <div class="bg-card border border-border rounded-lg p-3 sm:p-4">
        <h3 class="text-sm font-semibold text-white mb-2">Send a test webhook</h3>
        <p class="text-xs text-muted mb-1">
          Edit the JSON below and click Send. Your event will appear on the Stream tab in real time.
        </p>
        <p class="text-xs text-muted mb-3">
          Want to see file extraction? Send a JSON body with a <code class="text-accent">file_url</code> pointing at a PDF/image URL — don't post the file bytes directly. Example: <code class="text-accent">{`{"file_url":"https://example.com/invoice.pdf"}`}</code>
        </p>
        <textarea
          value={payload()}
          onInput={(e) => setPayload(e.currentTarget.value)}
          rows={6}
          spellcheck={false}
          class="bg-bg border border-border rounded px-2 py-2 text-xs text-white w-full font-mono resize-y"
        />
        <div class="flex items-center gap-2 mt-3">
          <button
            type="button"
            onClick={handleSend}
            disabled={sending()}
            class="text-xs px-4 py-1.5 rounded bg-accent text-white hover:bg-accent/80 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {sending() ? 'Sending…' : 'Send'}
          </button>
          <Show when={response()}>
            {(r) => (
              <span class={`text-xs ${r().ok ? 'text-success' : 'text-danger'} font-mono`}>
                HTTP {r().status} — {r().body.length > 80 ? r().body.slice(0, 80) + '…' : r().body}
              </span>
            )}
          </Show>
        </div>
      </div>

      {/* --- Curl --- */}
      <div class="bg-card border border-border rounded-lg p-3 sm:p-4">
        <div class="flex items-center justify-between mb-2">
          <h3 class="text-sm font-semibold text-white">Or use curl</h3>
          <CopyButton value={curlCommand()} />
        </div>
        <pre class="bg-bg border border-border rounded px-3 py-2 text-xs text-white font-mono overflow-x-auto whitespace-pre-wrap break-all">{curlCommand()}</pre>
      </div>
    </div>
  );
};

export default TryItPanel;
