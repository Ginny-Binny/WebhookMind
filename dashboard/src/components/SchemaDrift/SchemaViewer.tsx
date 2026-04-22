import { type Component, createResource, For, Show } from 'solid-js';
import { fetchSchema } from '../../lib/api';

const SchemaViewer: Component<{ sourceId: string }> = (props) => {
  const [schema] = createResource(() => props.sourceId, fetchSchema);

  return (
    <div class="bg-card border border-border rounded-lg p-3 sm:p-4">
      <div class="flex items-center justify-between mb-3">
        <h3 class="text-sm font-semibold text-white">Inferred Schema</h3>
        <Show when={schema()}>
          <span class={`text-xs px-2 py-0.5 rounded ${schema()!.is_locked ? 'bg-success/20 text-success' : 'bg-warning/20 text-warning'}`}>
            {schema()!.is_locked ? 'LOCKED' : `${schema()!.sample_count} samples`}
          </span>
        </Show>
      </div>
      <Show when={schema()} fallback={<div class="text-sm text-muted">No schema inferred yet</div>}>
        <div class="space-y-1">
          <For each={Object.entries((schema() as any)!.fields || (schema() as any)!.schema_data || {})}>
            {([name, field]) => (
              <div class="flex items-center gap-2 text-xs font-mono py-1 border-b border-border/50">
                <span class="text-white flex-1">{name}</span>
                <span class="text-accent">{(field as any).type}</span>
                {(field as any).nullable && <span class="text-warning">?</span>}
              </div>
            )}
          </For>
        </div>
      </Show>
    </div>
  );
};

export default SchemaViewer;
