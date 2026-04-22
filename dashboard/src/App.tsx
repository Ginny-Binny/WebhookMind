import { createSignal, onMount, Show } from 'solid-js';
import { createSSEConnection } from './lib/sse';
import { addWebhookReceived, updateWebhookDelivered, markWebhookFailed } from './stores/webhooks';
import { addDriftAlert } from './stores/alerts';
import type { SSEWebhookReceived, SSEWebhookDelivered, SSEDrift } from './lib/types';

import StreamPanel from './components/WebhookStream/StreamPanel';
import DriftAlert from './components/SchemaDrift/DriftAlert';
import SchemaViewer from './components/SchemaDrift/SchemaViewer';
import ThroughputChart from './components/Charts/ThroughputChart';
import LatencyChart from './components/Charts/LatencyChart';
import DLQPanel from './components/DeliveryLog/DLQPanel';
import RulesEditor from './components/Routing/RulesEditor';
import ReplayPanel from './components/Replay/ReplayPanel';

type Tab = 'stream' | 'schema' | 'dlq' | 'rules' | 'replay' | 'metrics';

const tabs: { id: Tab; label: string }[] = [
  { id: 'stream', label: 'Stream' },
  { id: 'metrics', label: 'Metrics' },
  { id: 'schema', label: 'Schema' },
  { id: 'dlq', label: 'DLQ' },
  { id: 'rules', label: 'Rules' },
  { id: 'replay', label: 'Replay' },
];

function App() {
  const [activeTab, setActiveTab] = createSignal<Tab>('stream');
  const [sourceId] = createSignal('test-source');
  const [mobileMenuOpen, setMobileMenuOpen] = createSignal(false);

  const sse = createSSEConnection('/events');

  onMount(() => {
    sse.on<SSEWebhookReceived>('webhook.received', (data) => {
      addWebhookReceived(data);
    });
    sse.on<SSEWebhookDelivered>('webhook.delivered', (data) => {
      updateWebhookDelivered(data);
    });
    sse.on<{ event_id: string }>('delivery.failed', (data) => {
      markWebhookFailed(data.event_id);
    });
    sse.on<SSEDrift>('schema.drift', (data) => {
      addDriftAlert(data);
    });
  });

  const selectTab = (tab: Tab) => {
    setActiveTab(tab);
    setMobileMenuOpen(false);
  };

  return (
    <div class="flex flex-col md:flex-row h-screen w-full overflow-hidden">
      {/* Mobile Header */}
      <header class="md:hidden bg-card border-b border-border flex items-center justify-between px-4 py-3 shrink-0">
        <div>
          <h1 class="text-sm font-bold text-white tracking-wider">WEBHOOKMIND</h1>
        </div>
        <div class="flex items-center gap-3">
          <div class="flex items-center gap-1.5">
            <div class={`w-2 h-2 rounded-full ${sse.connected() ? 'bg-success' : 'bg-danger'}`} />
            <span class="text-[10px] text-muted">{sse.connected() ? 'Live' : 'Off'}</span>
          </div>
          <button
            class="text-muted hover:text-white p-1"
            onClick={() => setMobileMenuOpen(!mobileMenuOpen())}
          >
            <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d={mobileMenuOpen() ? "M6 18L18 6M6 6l12 12" : "M4 6h16M4 12h16M4 18h16"} />
            </svg>
          </button>
        </div>
      </header>

      {/* Mobile Nav Dropdown */}
      <Show when={mobileMenuOpen()}>
        <div class="md:hidden bg-card border-b border-border py-1 shrink-0">
          {tabs.map((tab) => (
            <button
              class={`w-full text-left px-4 py-2.5 text-xs font-medium transition-colors ${
                activeTab() === tab.id
                  ? 'bg-accent/10 text-accent'
                  : 'text-muted hover:text-white hover:bg-surface'
              }`}
              onClick={() => selectTab(tab.id)}
            >
              {tab.label}
            </button>
          ))}
        </div>
      </Show>

      {/* Mobile Tab Bar */}
      <div class="md:hidden flex bg-card border-b border-border overflow-x-auto shrink-0">
        {tabs.map((tab) => (
          <button
            class={`flex-1 min-w-0 px-2 py-2 text-[10px] font-medium text-center transition-colors whitespace-nowrap ${
              activeTab() === tab.id
                ? 'text-accent border-b-2 border-accent bg-accent/5'
                : 'text-muted hover:text-white'
            }`}
            onClick={() => setActiveTab(tab.id)}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Desktop Sidebar */}
      <nav class="hidden md:flex w-48 bg-card border-r border-border flex-col shrink-0">
        <div class="p-4 border-b border-border">
          <h1 class="text-sm font-bold text-white tracking-wider">WEBHOOKMIND</h1>
          <p class="text-[10px] text-muted mt-0.5">Intelligence Layer</p>
        </div>
        <div class="flex-1 py-2">
          {tabs.map((tab) => (
            <button
              class={`w-full text-left px-4 py-2.5 text-xs font-medium transition-colors ${
                activeTab() === tab.id
                  ? 'bg-accent/10 text-accent border-r-2 border-accent'
                  : 'text-muted hover:text-white hover:bg-surface'
              }`}
              onClick={() => setActiveTab(tab.id)}
            >
              {tab.label}
            </button>
          ))}
        </div>
        <div class="p-4 border-t border-border">
          <div class="flex items-center gap-2">
            <div class={`w-2 h-2 rounded-full ${sse.connected() ? 'bg-success' : 'bg-danger'}`} />
            <span class="text-[10px] text-muted">{sse.connected() ? 'Live' : 'Disconnected'}</span>
          </div>
        </div>
      </nav>

      {/* Main Content */}
      <main class="flex-1 overflow-y-auto p-3 md:p-6 min-w-0">
        <Show when={activeTab() === 'stream'}>
          <StreamPanel connected={sse.connected} />
        </Show>
        <Show when={activeTab() === 'metrics'}>
          <div class="space-y-4">
            <ThroughputChart />
            <LatencyChart />
          </div>
        </Show>
        <Show when={activeTab() === 'schema'}>
          <SchemaViewer sourceId={sourceId()} />
        </Show>
        <Show when={activeTab() === 'dlq'}>
          <DLQPanel />
        </Show>
        <Show when={activeTab() === 'rules'}>
          <RulesEditor sourceId={sourceId()} />
        </Show>
        <Show when={activeTab() === 'replay'}>
          <ReplayPanel sourceId={sourceId()} />
        </Show>
      </main>

      <DriftAlert />
    </div>
  );
}

export default App;
