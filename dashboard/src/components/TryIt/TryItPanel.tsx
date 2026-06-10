import { type Component, createSignal, Show, For } from 'solid-js';
import { sandboxSource, resetSandbox } from '../../stores/sandbox';
import {
  anthropicKey,
  openaiKey,
  provider,
  model,
  setAnthropicKey,
  setOpenaiKey,
  setProvider,
  setModel,
  clearAnthropicKey,
  clearOpenaiKey,
  activeKey,
  type LLMProvider,
} from '../../stores/byok';
import CopyButton from './CopyButton';

const SAMPLE_PAYLOAD = `{
  "order_id": "TRY-1",
  "amount": 500,
  "currency": "USD",
  "customer": "Demo Co"
}`;

// Per-provider model catalog. Cheapest model first — that's also the server-side default,
// so leaving the picker on the first entry sends no X-LLM-Model header.
const MODELS: Record<LLMProvider, { id: string; label: string }[]> = {
  anthropic: [
    { id: 'claude-haiku-4-5', label: 'Claude Haiku 4.5 (cheap/fast)' },
    { id: 'claude-sonnet-4-6', label: 'Claude Sonnet 4.6' },
    { id: 'claude-opus-4-7', label: 'Claude Opus 4.7' },
  ],
  openai: [
    { id: 'gpt-4.1-mini', label: 'GPT-4.1 mini (cheap/fast)' },
    { id: 'gpt-4o-mini', label: 'GPT-4o mini' },
    { id: 'gpt-4o', label: 'GPT-4o' },
    { id: 'gpt-4.1', label: 'GPT-4.1' },
  ],
};

const PROVIDER_META: Record<LLMProvider, { header: string; placeholder: string; label: string; signupURL: string }> = {
  anthropic: {
    header: 'X-Anthropic-Key',
    placeholder: 'sk-ant-...',
    label: 'Anthropic',
    signupURL: 'https://console.anthropic.com/settings/keys',
  },
  openai: {
    header: 'X-OpenAI-Key',
    placeholder: 'sk-...',
    label: 'OpenAI',
    signupURL: 'https://platform.openai.com/api-keys',
  },
};

const TryItPanel: Component = () => {
  const [payload, setPayload] = createSignal(SAMPLE_PAYLOAD);
  const [keyDraft, setKeyDraft] = createSignal(activeKey());
  const [keyVisible, setKeyVisible] = createSignal(false);
  const [sending, setSending] = createSignal(false);
  const [response, setResponse] = createSignal<{ ok: boolean; status: number; body: string } | null>(null);

  // Default model for the currently-selected provider (first entry in the catalog).
  const defaultModel = () => MODELS[provider()][0].id;

  // The effective model that will actually travel on the request — saved override
  // when it exists, otherwise the provider's default.
  const effectiveModel = () => model() || defaultModel();

  // Send X-LLM-Model only when the user picked something other than the cheapest default.
  const shouldSendModel = () => model() !== '' && model() !== defaultModel();

  // Fully-qualified webhook URL for display + copy + curl. Origin = wherever the dashboard is served.
  const webhookURL = () => `${window.location.origin}/webhook/${sandboxSource()}`;

  const curlCommand = () => {
    const lines = [
      `curl -X POST ${webhookURL()} \\`,
      `  -H "Content-Type: application/json" \\`,
    ];
    if (activeKey()) {
      lines.push(`  -H "${PROVIDER_META[provider()].header}: ${activeKey()}" \\`);
    }
    if (shouldSendModel()) {
      lines.push(`  -H "X-LLM-Model: ${effectiveModel()}" \\`);
    }
    lines.push(`  -d '${payload().replace(/\n\s*/g, ' ').trim()}'`);
    return lines.join('\n');
  };

  const handleSend = async () => {
    setSending(true);
    setResponse(null);
    try {
      const headers: Record<string, string> = { 'Content-Type': 'application/json' };
      if (activeKey()) {
        headers[PROVIDER_META[provider()].header] = activeKey();
      }
      if (shouldSendModel()) {
        headers['X-LLM-Model'] = effectiveModel();
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
    if (provider() === 'openai') {
      setOpenaiKey(keyDraft());
    } else {
      setAnthropicKey(keyDraft());
    }
  };

  const handleForgetKey = () => {
    if (provider() === 'openai') {
      clearOpenaiKey();
    } else {
      clearAnthropicKey();
    }
    setKeyDraft('');
  };

  const handleProviderChange = (p: LLMProvider) => {
    setProvider(p);
    setKeyDraft(p === 'openai' ? openaiKey() : anthropicKey());
    // Reset model when switching providers — saved value is provider-specific.
    setModel('');
  };

  const handleResetSandbox = () => {
    if (confirm('Generate a new sandbox ID? Your current view will be cleared.')) {
      resetSandbox();
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
          <h3 class="text-sm font-semibold text-white">Bring your own LLM key</h3>
          <Show when={activeKey()}>
            <span class="text-xs text-success">Saved ✓</span>
          </Show>
        </div>
        <p class="text-xs text-muted mb-3">
          Optional. Without a key, JSON-only webhooks work fine. With a key set, requests with a <code class="text-accent">file_url</code> (PDF, image, or DOCX) can be extracted by the model.
        </p>

        {/* Provider picker */}
        <div class="flex gap-2 mb-3">
          <For each={(['anthropic', 'openai'] as LLMProvider[])}>
            {(p) => (
              <button
                type="button"
                onClick={() => handleProviderChange(p)}
                class={`text-xs px-3 py-1.5 rounded transition-colors ${
                  provider() === p
                    ? 'bg-accent/30 text-accent border border-accent/50'
                    : 'bg-surface text-muted hover:text-white border border-border'
                }`}
              >
                {PROVIDER_META[p].label}
              </button>
            )}
          </For>
        </div>

        {/* Key input */}
        <div class="flex flex-col sm:flex-row gap-2">
          <input
            type={keyVisible() ? 'text' : 'password'}
            placeholder={PROVIDER_META[provider()].placeholder}
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
              disabled={!keyDraft() || keyDraft() === activeKey()}
              class="text-xs px-3 py-1 rounded bg-accent/20 text-accent hover:bg-accent/40 transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
            >
              Save
            </button>
            <Show when={activeKey()}>
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

        {/* Model picker */}
        <div class="flex items-center gap-2 mt-3">
          <label class="text-xs text-muted whitespace-nowrap">Model:</label>
          <select
            value={effectiveModel()}
            onChange={(e) => setModel(e.currentTarget.value === defaultModel() ? '' : e.currentTarget.value)}
            class="bg-bg border border-border rounded px-2 py-1 text-xs text-white font-mono flex-1"
          >
            <For each={MODELS[provider()]}>
              {(m) => <option value={m.id}>{m.label}</option>}
            </For>
          </select>
        </div>

        <p class="text-xs text-muted mt-3">
          Stored in your browser only. Never persisted on our server, never logged. The key is sent only as a per-request header on the specific webhook that needs it; the server strips it before storing the event. Get a key from{' '}
          <a href={PROVIDER_META[provider()].signupURL} target="_blank" rel="noopener" class="text-accent hover:underline">
            {PROVIDER_META[provider()].label}
          </a>
          . Source is open at{' '}
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
          Want to see file extraction? Send a JSON body with a <code class="text-accent">file_url</code> pointing at a PDF, image, or DOCX URL — don't post the file bytes directly. Example: <code class="text-accent">{`{"file_url":"https://example.com/invoice.pdf"}`}</code>
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
