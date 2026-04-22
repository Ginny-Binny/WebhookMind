import { type Component, createSignal, onMount, Show, For } from 'solid-js';
import { animate } from '@motionone/dom';
import { fetchWebhookDetail } from '../../lib/api';
import type { StreamEvent } from '../../stores/webhooks';
import type { WebhookDetail } from '../../lib/types';

const statusColors: Record<string, string> = {
  received: 'border-l-accent',
  delivered: 'border-l-success',
  failed: 'border-l-danger',
};

function parseExtracted(data: unknown): Record<string, string> {
  if (!data || typeof data !== 'object') return {};
  const obj = data as Record<string, unknown>;

  // If it has a "raw" key with JSON inside, parse that
  if (obj.raw && typeof obj.raw === 'string') {
    try {
      // The LLM sometimes returns multiple JSON blocks — grab the first valid one
      const jsonMatch = (obj.raw as string).match(/\{[\s\S]*?\n\}/);
      if (jsonMatch) {
        const parsed = JSON.parse(jsonMatch[0]);
        if (typeof parsed === 'object') return flattenForDisplay(parsed);
      }
    } catch { /* fall through */ }
    return { raw_text: (obj.raw as string).slice(0, 500) };
  }

  return flattenForDisplay(obj);
}

function flattenForDisplay(obj: Record<string, unknown>, prefix = ''): Record<string, string> {
  const result: Record<string, string> = {};
  for (const [key, val] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${key}` : key;
    if (val && typeof val === 'object' && !Array.isArray(val)) {
      Object.assign(result, flattenForDisplay(val as Record<string, unknown>, fullKey));
    } else {
      result[fullKey] = String(val ?? '—');
    }
  }
  return result;
}

function parsePayload(raw: unknown): Record<string, string> {
  if (!raw) return {};
  try {
    const obj = typeof raw === 'string' ? JSON.parse(raw) : raw;
    if (typeof obj !== 'object') return { value: String(obj) };
    // Skip "extracted" key — shown separately
    const filtered: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(obj as Record<string, unknown>)) {
      if (k !== 'extracted') filtered[k] = v;
    }
    return flattenForDisplay(filtered);
  } catch {
    return { raw: String(raw).slice(0, 300) };
  }
}

const WebhookCard: Component<{ event: StreamEvent }> = (props) => {
  let cardRef!: HTMLDivElement;
  const [expanded, setExpanded] = createSignal(false);
  const [detail, setDetail] = createSignal<WebhookDetail | null>(null);
  const [loading, setLoading] = createSignal(false);
  const [activeSection, setActiveSection] = createSignal<'payload' | 'extraction' | 'diff' | 'delivery'>('payload');

  onMount(() => {
    animate(cardRef, { opacity: [0, 1], y: [-20, 0] }, { duration: 0.2, easing: 'ease-out' });
  });

  const timeAgo = () => {
    const diff = Date.now() - new Date(props.event.received_at).getTime();
    if (diff < 60000) return `${Math.floor(diff / 1000)}s ago`;
    if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
    return `${Math.floor(diff / 3600000)}h ago`;
  };

  const toggleExpand = async () => {
    const wasExpanded = expanded();
    setExpanded(!wasExpanded);
    if (!wasExpanded && !detail()) {
      setLoading(true);
      try {
        const d = await fetchWebhookDetail(props.event.event_id);
        setDetail(d);
        // Auto-select the most interesting section
        if (d.extraction?.success) setActiveSection('extraction');
        else if (d.diff) setActiveSection('diff');
      } catch { /* ignore */ }
      setLoading(false);
    }
  };

  const extractedFields = () => {
    const d = detail();
    if (!d?.extraction?.extracted_data) return {};
    return parseExtracted(d.extraction.extracted_data);
  };

  const payloadFields = () => {
    const d = detail();
    if (!d?.raw_body) return {};
    return parsePayload(d.raw_body);
  };

  const diffData = () => {
    const d = detail();
    if (!d?.diff) return null;
    return d.diff;
  };

  const sectionTabs = () => {
    const tabs: { id: typeof activeSection extends () => infer T ? T : never; label: string; badge?: string }[] = [
      { id: 'payload', label: 'Payload' },
    ];
    const d = detail();
    if (d?.extraction) {
      tabs.push({
        id: 'extraction',
        label: 'Extracted',
        badge: d.extraction.cache_hit ? 'CACHE' : d.extraction.file_type?.toUpperCase(),
      });
    }
    if (d?.diff) tabs.push({ id: 'diff', label: 'Diff' });
    tabs.push({ id: 'delivery', label: `Delivery (${d?.attempts?.length || 0})` });
    return tabs;
  };

  return (
    <div ref={cardRef} class={`bg-card border-l-4 ${statusColors[props.event.status] || 'border-l-muted'} border border-border rounded-r-lg overflow-hidden transition-colors`}>
      {/* Header */}
      <div
        class="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-1 sm:gap-3 p-2.5 sm:p-3 cursor-pointer hover:bg-surface"
        onClick={toggleExpand}
      >
        <div class="flex items-center justify-between sm:justify-start gap-2 sm:gap-3 min-w-0">
          <span class="text-xs font-mono text-accent truncate">{props.event.event_id.slice(0, 8)}</span>
          <span class="text-[10px] sm:text-xs text-muted truncate">{props.event.source_id}</span>
        </div>
        <div class="flex items-center justify-between sm:justify-end gap-2 sm:gap-3">
          <span class="text-[10px] sm:text-xs text-muted">{timeAgo()}</span>
          <span class={`text-[10px] sm:text-xs font-semibold ${
            props.event.status === 'delivered' ? 'text-success' :
            props.event.status === 'failed' ? 'text-danger' : 'text-accent'
          }`}>
            {props.event.status.toUpperCase()}
          </span>
          {props.event.duration_ms != null && (
            <span class="text-[10px] sm:text-xs font-mono text-muted">{props.event.duration_ms}ms</span>
          )}
          <span class="text-[10px] text-muted">{expanded() ? '▲' : '▼'}</span>
        </div>
      </div>

      {/* Expanded Content */}
      <Show when={expanded()}>
        <div class="border-t border-border" onClick={(e) => e.stopPropagation()}>
          <Show when={loading()}>
            <div class="p-4 space-y-2">
              <div class="h-3 bg-border/30 rounded animate-pulse w-3/4" />
              <div class="h-3 bg-border/30 rounded animate-pulse w-1/2" />
              <div class="h-3 bg-border/30 rounded animate-pulse w-2/3" />
            </div>
          </Show>

          <Show when={detail()}>
            {/* Section Tabs */}
            <div class="flex border-b border-border overflow-x-auto">
              <For each={sectionTabs()}>
                {(tab) => (
                  <button
                    class={`px-3 py-2 text-[10px] sm:text-xs font-medium whitespace-nowrap transition-colors ${
                      activeSection() === tab.id
                        ? 'text-accent border-b-2 border-accent bg-accent/5'
                        : 'text-muted hover:text-white'
                    }`}
                    onClick={() => setActiveSection(tab.id as any)}
                  >
                    {tab.label}
                    {tab.badge && (
                      <span class="ml-1.5 px-1 py-0.5 rounded text-[8px] bg-accent/20 text-accent">{tab.badge}</span>
                    )}
                  </button>
                )}
              </For>
            </div>

            {/* Payload Section */}
            <Show when={activeSection() === 'payload'}>
              <div class="p-3">
                <FieldTable fields={payloadFields()} />
              </div>
            </Show>

            {/* Extraction Section */}
            <Show when={activeSection() === 'extraction' && detail()!.extraction}>
              <div class="p-3 space-y-3">
                {/* Extraction Meta */}
                <div class="flex flex-wrap gap-2 text-[10px]">
                  <span class={`px-2 py-0.5 rounded ${detail()!.extraction!.success ? 'bg-success/20 text-success' : 'bg-danger/20 text-danger'}`}>
                    {detail()!.extraction!.success ? 'SUCCESS' : 'FAILED'}
                  </span>
                  <span class={`px-2 py-0.5 rounded ${detail()!.extraction!.cache_hit ? 'bg-accent/20 text-accent' : 'bg-surface text-muted'}`}>
                    {detail()!.extraction!.cache_hit ? 'CACHE HIT' : 'NEW EXTRACTION'}
                  </span>
                  <span class="px-2 py-0.5 rounded bg-surface text-muted font-mono">
                    {detail()!.extraction!.duration_ms}ms
                  </span>
                  <Show when={detail()!.extraction!.file_type}>
                    <span class="px-2 py-0.5 rounded bg-warning/20 text-warning uppercase">
                      {detail()!.extraction!.file_type}
                    </span>
                  </Show>
                </div>

                <Show when={detail()!.extraction!.success} fallback={
                  <div class="bg-danger/10 border border-danger/20 rounded-lg p-3 text-xs text-danger">
                    {detail()!.extraction!.error_message}
                  </div>
                }>
                  <FieldTable fields={extractedFields()} highlight />
                </Show>
              </div>
            </Show>

            {/* Diff Section */}
            <Show when={activeSection() === 'diff' && diffData()}>
              <div class="p-3 space-y-1">
                <Show when={diffData()!.added && Object.keys(diffData()!.added).length > 0}>
                  <For each={Object.entries(diffData()!.added)}>
                    {([key, val]) => (
                      <div class="flex items-start gap-2 py-1.5 px-2 rounded bg-success/5 text-xs">
                        <span class="text-success font-bold shrink-0">+</span>
                        <span class="font-mono text-white min-w-[120px] sm:min-w-[160px]">{key}</span>
                        <span class="font-mono text-success break-all">{String(val)}</span>
                      </div>
                    )}
                  </For>
                </Show>
                <Show when={diffData()!.removed && Object.keys(diffData()!.removed).length > 0}>
                  <For each={Object.entries(diffData()!.removed)}>
                    {([key, val]) => (
                      <div class="flex items-start gap-2 py-1.5 px-2 rounded bg-danger/5 text-xs">
                        <span class="text-danger font-bold shrink-0">−</span>
                        <span class="font-mono text-white min-w-[120px] sm:min-w-[160px]">{key}</span>
                        <span class="font-mono text-danger break-all">{String(val)}</span>
                      </div>
                    )}
                  </For>
                </Show>
                <Show when={diffData()!.changed && diffData()!.changed.length > 0}>
                  <For each={diffData()!.changed}>
                    {(c) => (
                      <div class="flex items-start gap-2 py-1.5 px-2 rounded bg-warning/5 text-xs">
                        <span class="text-warning font-bold shrink-0">~</span>
                        <span class="font-mono text-white min-w-[120px] sm:min-w-[160px]">{c.field}</span>
                        <div class="font-mono break-all">
                          <span class="text-danger line-through">{String(c.old_value)}</span>
                          <span class="text-muted mx-1">→</span>
                          <span class="text-success">{String(c.new_value)}</span>
                        </div>
                      </div>
                    )}
                  </For>
                </Show>
                <Show when={!diffData()!.added || (Object.keys(diffData()!.added).length === 0 && Object.keys(diffData()!.removed || {}).length === 0 && (!diffData()!.changed || diffData()!.changed.length === 0))}>
                  <div class="text-xs text-muted text-center py-4">No changes from previous webhook</div>
                </Show>
              </div>
            </Show>

            {/* Delivery Section */}
            <Show when={activeSection() === 'delivery'}>
              <div class="p-3 space-y-1.5">
                <For each={detail()!.attempts}>
                  {(attempt) => (
                    <div class={`flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg px-3 py-2 text-xs ${
                      attempt.success ? 'bg-success/5 border border-success/20' : 'bg-danger/5 border border-danger/20'
                    }`}>
                      <span class="font-mono text-muted">Attempt #{attempt.attempt_number}</span>
                      <span class={`font-bold ${attempt.success ? 'text-success' : 'text-danger'}`}>
                        {attempt.status_code || 'ERR'}
                      </span>
                      <span class="font-mono text-muted">{attempt.duration_ms}ms</span>
                      <span class="text-muted text-[10px] truncate max-w-[150px]">→ {attempt.destination_id.slice(0, 12)}</span>
                      <Show when={attempt.error_message}>
                        <span class="text-danger text-[10px] truncate w-full sm:w-auto">{attempt.error_message}</span>
                      </Show>
                    </div>
                  )}
                </For>
                <Show when={!detail()!.attempts || detail()!.attempts.length === 0}>
                  <div class="text-xs text-muted text-center py-4">No delivery attempts yet</div>
                </Show>
              </div>
            </Show>
          </Show>
        </div>
      </Show>
    </div>
  );
};

// Reusable field table component
const FieldTable: Component<{ fields: Record<string, string>; highlight?: boolean }> = (props) => {
  const entries = () => Object.entries(props.fields);

  return (
    <Show when={entries().length > 0} fallback={
      <div class="text-xs text-muted text-center py-4">No data</div>
    }>
      <div class="rounded-lg overflow-hidden border border-border">
        <For each={entries()}>
          {([key, value], i) => (
            <div class={`flex text-xs ${i() % 2 === 0 ? 'bg-surface' : 'bg-card'}`}>
              <div class="px-3 py-2 font-mono text-muted w-[140px] sm:w-[200px] shrink-0 border-r border-border truncate">
                {key}
              </div>
              <div class={`px-3 py-2 font-mono break-all flex-1 min-w-0 ${props.highlight ? 'text-success' : 'text-white'}`}>
                {value}
              </div>
            </div>
          )}
        </For>
      </div>
    </Show>
  );
};

export default WebhookCard;
