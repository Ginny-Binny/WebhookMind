import { type Component, For } from 'solid-js';
import { webhooks } from '../../stores/webhooks';
import WebhookCard from './WebhookCard';

const StreamPanel: Component<{ connected: () => boolean; onTryIt?: () => void }> = (props) => {
  return (
    <div class="flex flex-col h-full">
      <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-2 mb-4">
        <h2 class="text-base sm:text-lg font-semibold text-white">Live Stream</h2>
        <div class="flex items-center gap-2">
          <div class={`w-2 h-2 rounded-full ${props.connected() ? 'bg-success' : 'bg-danger'} animate-pulse`} />
          <span class="text-xs text-muted">{props.connected() ? 'Connected' : 'Disconnected'}</span>
          <span class="text-xs text-muted ml-2">{webhooks.length} events</span>
        </div>
      </div>
      <div class="flex-1 overflow-y-auto space-y-2 pr-1">
        <For each={webhooks}>
          {(event) => <WebhookCard event={event} />}
        </For>
        {webhooks.length === 0 && (
          <div class="text-center text-muted py-12 text-sm">
            <p>Waiting for webhooks...</p>
            {props.onTryIt && (
              <p class="mt-2">
                Send your first one in the{' '}
                <button
                  type="button"
                  onClick={props.onTryIt}
                  class="text-accent hover:underline font-medium"
                >
                  Try it
                </button>{' '}
                tab →
              </p>
            )}
          </div>
        )}
      </div>
    </div>
  );
};

export default StreamPanel;
