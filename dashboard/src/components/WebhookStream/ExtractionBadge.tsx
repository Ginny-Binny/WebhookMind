import type { Component } from 'solid-js';

const ExtractionBadge: Component<{ hasExtraction: boolean; cacheHit?: boolean }> = (props) => {
  return (
    <span
      class={`text-xs px-1.5 py-0.5 rounded font-mono ${
        props.hasExtraction
          ? props.cacheHit
            ? 'bg-accent/20 text-accent'
            : 'bg-success/20 text-success'
          : 'bg-muted/20 text-muted'
      }`}
    >
      {props.hasExtraction ? (props.cacheHit ? 'CACHE' : 'EXTRACT') : '—'}
    </span>
  );
};

export default ExtractionBadge;
