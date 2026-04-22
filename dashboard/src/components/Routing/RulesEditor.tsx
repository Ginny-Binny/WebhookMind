import { type Component, createSignal, createResource, For, Show } from 'solid-js';
import { fetchRules, createRule, deleteRule } from '../../lib/api';

const OPERATORS = ['eq', 'neq', 'gt', 'gte', 'lt', 'lte', 'contains', 'exists'];

const RulesEditor: Component<{ sourceId: string }> = (props) => {
  const [rules, { refetch }] = createResource(() => props.sourceId, fetchRules);
  const [showForm, setShowForm] = createSignal(false);
  const [name, setName] = createSignal('');
  const [destId, setDestId] = createSignal('');
  const [field, setField] = createSignal('');
  const [operator, setOperator] = createSignal('eq');
  const [value, setValue] = createSignal('');
  const [priority, setPriority] = createSignal(10);

  const handleCreate = async () => {
    let condValue: unknown = value();
    const num = Number(value());
    if (!isNaN(num) && value() !== '') condValue = num;

    await createRule(props.sourceId, {
      name: name(),
      destination_id: destId(),
      priority: priority(),
      logic_operator: 'AND',
      conditions: [{ field: field(), operator: operator(), value: condValue }] as any,
    });
    setShowForm(false);
    setName(''); setDestId(''); setField(''); setValue('');
    refetch();
  };

  const handleDelete = async (ruleId: string) => {
    await deleteRule(ruleId);
    refetch();
  };

  return (
    <div class="bg-card border border-border rounded-lg p-3 sm:p-4">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-sm font-semibold text-white">Routing Rules</h3>
        <button
          class="text-xs px-3 py-1 rounded bg-accent/20 text-accent hover:bg-accent/40 transition-colors"
          onClick={() => setShowForm(!showForm())}
        >
          {showForm() ? 'Cancel' : '+ New Rule'}
        </button>
      </div>

      <Show when={showForm()}>
        <div class="bg-surface rounded-lg p-3 mb-4 space-y-3">
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
            <input class="bg-bg border border-border rounded px-2 py-1.5 text-xs text-white w-full" placeholder="Rule Name" value={name()} onInput={(e) => setName(e.target.value)} />
            <input class="bg-bg border border-border rounded px-2 py-1.5 text-xs text-white w-full" placeholder="Destination ID" value={destId()} onInput={(e) => setDestId(e.target.value)} />
          </div>
          <div class="flex flex-col sm:flex-row gap-2 sm:items-center">
            <span class="text-xs text-muted shrink-0">IF</span>
            <input class="bg-bg border border-border rounded px-2 py-1.5 text-xs text-white flex-1 min-w-0" placeholder="field (e.g. amount)" value={field()} onInput={(e) => setField(e.target.value)} />
            <select class="bg-bg border border-border rounded px-2 py-1.5 text-xs text-white" value={operator()} onChange={(e) => setOperator(e.target.value)}>
              <For each={OPERATORS}>{(op) => <option value={op}>{op}</option>}</For>
            </select>
            <input class="bg-bg border border-border rounded px-2 py-1.5 text-xs text-white w-full sm:w-24" placeholder="value" value={value()} onInput={(e) => setValue(e.target.value)} />
          </div>
          <div class="flex flex-col sm:flex-row gap-2 sm:items-center">
            <div class="flex items-center gap-2">
              <span class="text-xs text-muted">Priority:</span>
              <input type="number" class="bg-bg border border-border rounded px-2 py-1.5 text-xs text-white w-16" value={priority()} onInput={(e) => setPriority(Number(e.target.value))} />
            </div>
            <button class="sm:ml-auto w-full sm:w-auto px-3 py-1.5 rounded bg-success/20 text-success hover:bg-success/40 text-xs transition-colors" onClick={handleCreate}>Save Rule</button>
          </div>
        </div>
      </Show>

      <Show when={rules() && rules()!.length > 0} fallback={
        <div class="text-sm text-muted text-center py-4">No routing rules</div>
      }>
        <div class="space-y-2">
          <For each={rules()}>
            {(rule) => (
              <div class="flex flex-col sm:flex-row sm:items-center justify-between bg-surface rounded-lg p-3 text-xs gap-2">
                <div class="min-w-0">
                  <span class="text-white font-medium">{rule.name}</span>
                  <span class="text-muted ml-2">→ {rule.destination_id.slice(0, 8)}</span>
                  <span class="text-muted ml-2">P{rule.priority}</span>
                </div>
                <button class="text-danger hover:text-white transition-colors text-left sm:text-right shrink-0" onClick={() => handleDelete(rule.id)}>Delete</button>
              </div>
            )}
          </For>
        </div>
      </Show>
    </div>
  );
};

export default RulesEditor;
