import { createSignal } from 'solid-js';

// Per-visitor sandbox source ID. Each visitor gets a random `demo-{6 hex}` ID on
// their first dashboard visit, persisted to localStorage. The dashboard's SSE
// connection filters on this source so visitors can't see each other's events,
// and the Try It panel's webhook target uses the same ID.

const STORAGE_KEY = 'webhookmind:sandbox_source';

function generateID(): string {
  // 6 hex chars = 16M combinations — collisions are theoretical, fine for a demo.
  const bytes = new Uint8Array(3);
  crypto.getRandomValues(bytes);
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
  return `demo-${hex}`;
}

function load(): string {
  // Guard against SSR / non-browser environments — Solid components run on the client,
  // but Vite's build step can hit this code during type-checking.
  if (typeof window === 'undefined') return generateID();
  const existing = window.localStorage.getItem(STORAGE_KEY);
  if (existing) return existing;
  const fresh = generateID();
  window.localStorage.setItem(STORAGE_KEY, fresh);
  return fresh;
}

const [sandboxSource, setSandboxSourceSignal] = createSignal<string>(load());

export { sandboxSource };

export function resetSandbox(): string {
  const fresh = generateID();
  if (typeof window !== 'undefined') {
    window.localStorage.setItem(STORAGE_KEY, fresh);
  }
  setSandboxSourceSignal(fresh);
  return fresh;
}
