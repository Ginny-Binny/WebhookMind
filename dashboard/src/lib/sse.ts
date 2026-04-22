import { createSignal } from 'solid-js';

export function createSSEConnection(url: string) {
  const [connected, setConnected] = createSignal(false);
  let es: EventSource | null = null;

  function connect() {
    es = new EventSource(url);

    es.addEventListener('connected', () => {
      setConnected(true);
    });

    es.onopen = () => setConnected(true);

    es.onerror = () => {
      setConnected(false);
      // Browser auto-reconnects EventSource
    };
  }

  function on<T = unknown>(eventType: string, handler: (data: T) => void) {
    if (!es) connect();
    es!.addEventListener(eventType, (e: Event) => {
      const me = e as MessageEvent;
      try {
        handler(JSON.parse(me.data) as T);
      } catch {
        // ignore parse errors
      }
    });
  }

  function close() {
    es?.close();
    setConnected(false);
  }

  connect();

  return { connected, on, close };
}
