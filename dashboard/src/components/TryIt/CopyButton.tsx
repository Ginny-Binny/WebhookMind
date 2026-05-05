import { type Component, createSignal } from 'solid-js';

const CopyButton: Component<{ value: string; label?: string }> = (props) => {
  const [copied, setCopied] = createSignal(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(props.value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Older browsers / insecure contexts — fall back to a tiny textarea trick.
      const ta = document.createElement('textarea');
      ta.value = props.value;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      try {
        document.execCommand('copy');
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
      } finally {
        document.body.removeChild(ta);
      }
    }
  };

  return (
    <button
      type="button"
      onClick={handleCopy}
      class="text-xs px-2.5 py-1 rounded bg-accent/20 text-accent hover:bg-accent/40 transition-colors whitespace-nowrap"
    >
      {copied() ? 'Copied ✓' : (props.label ?? 'Copy')}
    </button>
  );
};

export default CopyButton;
