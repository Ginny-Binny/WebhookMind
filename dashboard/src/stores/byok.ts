import { createSignal } from 'solid-js';

// Visitor's per-provider LLM API key and model selection for BYOK extraction. Stored in
// localStorage on their browser only — never sent to our server except as the relevant
// X-{Provider}-Key / X-LLM-Model headers on the specific webhook request that needs them
// (and the backend strips those headers from event.Headers before any storage).

export type LLMProvider = 'anthropic' | 'openai';

const ANTHROPIC_KEY = 'webhookmind:anthropic_key';
const OPENAI_KEY = 'webhookmind:openai_key';
const PROVIDER_KEY = 'webhookmind:llm_provider';
const MODEL_KEY = 'webhookmind:llm_model';

function loadString(key: string): string {
  if (typeof window === 'undefined') return '';
  return window.localStorage.getItem(key) ?? '';
}

function loadProvider(): LLMProvider {
  const v = loadString(PROVIDER_KEY);
  return v === 'openai' ? 'openai' : 'anthropic';
}

const [anthropicKey, setAnthropicKeySignal] = createSignal<string>(loadString(ANTHROPIC_KEY));
const [openaiKey, setOpenaiKeySignal] = createSignal<string>(loadString(OPENAI_KEY));
const [provider, setProviderSignal] = createSignal<LLMProvider>(loadProvider());
const [model, setModelSignal] = createSignal<string>(loadString(MODEL_KEY));

export { anthropicKey, openaiKey, provider, model };

function writeString(key: string, value: string) {
  if (typeof window === 'undefined') return;
  if (value === '') {
    window.localStorage.removeItem(key);
  } else {
    window.localStorage.setItem(key, value);
  }
}

export function setAnthropicKey(value: string) {
  const trimmed = value.trim();
  writeString(ANTHROPIC_KEY, trimmed);
  setAnthropicKeySignal(trimmed);
}

export function clearAnthropicKey() {
  setAnthropicKey('');
}

export function setOpenaiKey(value: string) {
  const trimmed = value.trim();
  writeString(OPENAI_KEY, trimmed);
  setOpenaiKeySignal(trimmed);
}

export function clearOpenaiKey() {
  setOpenaiKey('');
}

export function setProvider(value: LLMProvider) {
  writeString(PROVIDER_KEY, value);
  setProviderSignal(value);
}

export function setModel(value: string) {
  const trimmed = value.trim();
  writeString(MODEL_KEY, trimmed);
  setModelSignal(trimmed);
}

// activeKey returns the key for whichever provider is currently selected.
export function activeKey(): string {
  return provider() === 'openai' ? openaiKey() : anthropicKey();
}
