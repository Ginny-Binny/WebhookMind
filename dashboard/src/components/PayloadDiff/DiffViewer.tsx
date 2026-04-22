import { type Component, For, Show } from 'solid-js';
import type { DiffData } from '../../lib/types';

const DiffViewer: Component<{ diff: DiffData }> = (props) => {
  return (
    <div class="bg-surface rounded-lg p-3 text-xs font-mono space-y-1">
      <Show when={props.diff.added && Object.keys(props.diff.added).length > 0}>
        <For each={Object.entries(props.diff.added)}>
          {([key, val]) => (
            <div class="flex gap-2 text-success">
              <span class="opacity-60">+</span>
              <span class="flex-1">{key}</span>
              <span>{String(val)}</span>
            </div>
          )}
        </For>
      </Show>
      <Show when={props.diff.removed && Object.keys(props.diff.removed).length > 0}>
        <For each={Object.entries(props.diff.removed)}>
          {([key, val]) => (
            <div class="flex gap-2 text-danger">
              <span class="opacity-60">-</span>
              <span class="flex-1">{key}</span>
              <span>{String(val)}</span>
            </div>
          )}
        </For>
      </Show>
      <Show when={props.diff.changed && props.diff.changed.length > 0}>
        <For each={props.diff.changed}>
          {(c) => (
            <div class="flex gap-2 text-warning">
              <span class="opacity-60">~</span>
              <span class="flex-1">{c.field}</span>
              <span class="text-danger line-through">{String(c.old_value)}</span>
              <span>→</span>
              <span class="text-success">{String(c.new_value)}</span>
            </div>
          )}
        </For>
      </Show>
    </div>
  );
};

export default DiffViewer;
