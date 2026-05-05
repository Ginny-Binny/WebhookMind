import { createSignal } from 'solid-js';

// Visitor's Anthropic API key for BYOK extraction. Stored in localStorage on their
// browser only — never sent to our server except as the X-Anthropic-Key header on
// the specific webhook request that needs it (and the backend strips that header
// from event.Headers before any storage).

const STORAGE_KEY = 'webhookmind:anthropic_key';

function load(): string {
  if (typeof window === 'undefined') return '';
  return window.localStorage.getItem(STORAGE_KEY) ?? '';
}

const [anthropicKey, setAnthropicKeySignal] = createSignal<string>(load());

export { anthropicKey };

export function setAnthropicKey(value: string) {
  const trimmed = value.trim();
  if (typeof window !== 'undefined') {
    if (trimmed === '') {
      window.localStorage.removeItem(STORAGE_KEY);
    } else {
      window.localStorage.setItem(STORAGE_KEY, trimmed);
    }
  }
  setAnthropicKeySignal(trimmed);
}

export function clearAnthropicKey() {
  setAnthropicKey('');
}
