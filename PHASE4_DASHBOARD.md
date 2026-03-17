# WebhookMind — Phase 4: Dashboard

## Prerequisites

Phase 3 must be fully complete and accepted:
- Full extraction pipeline working (PDF, Image, Audio)
- Schema inference and drift detection active
- Payload diffing running
- Condition-based routing working
- Replay engine operational
- REST API server (`cmd/api`, port 8082) running

---

## Goal

Build the real-time dashboard. A visually stunning, highly performant SolidJS frontend that shows the webhook stream live, visualizes extractions, surfaces schema drifts, displays payload diffs, and lets operators manage routing rules and replay sessions — all updating in real time via SSE.

This is the face of the product. It must be fast AND beautiful. SolidJS was chosen precisely because hundreds of events per minute must animate onto the screen without ever causing a re-render cascade.

---

## Absolute Constraints

- Framework: SolidJS only. No React, no Vue, no Svelte.
- Animations: Motion One (GPU-accelerated, SolidJS-native) for per-element animations. GSAP for complex sequenced animations.
- Charts: D3.js with SolidJS signals. No Chart.js, no Recharts.
- Styling: Tailwind CSS only. No CSS-in-JS, no styled-components.
- Real-time: SSE only for server → browser. No WebSockets, no polling.
- Build: Vite.
- No client-side state management libraries. SolidJS signals are the state layer.
- All API calls: REST to `cmd/api` (port 8082 via Nginx at `/api/*`)

---

## Project Setup

```bash
npm create vite@latest dashboard -- --template solid-ts
cd dashboard
npm install
npm install @motionone/solid gsap d3
npm install tailwindcss @tailwindcss/vite
npm install solid-js
```

Folder structure:
```
dashboard/
├── src/
│   ├── App.tsx
│   ├── index.tsx
│   ├── components/
│   │   ├── WebhookStream/
│   │   │   ├── StreamPanel.tsx      # Live webhook feed
│   │   │   ├── WebhookCard.tsx      # Single event card
│   │   │   └── ExtractionBadge.tsx  # Shows extraction status
│   │   ├── SchemaDrift/
│   │   │   ├── DriftAlert.tsx       # Drift notification toast
│   │   │   └── SchemaViewer.tsx     # Current inferred schema display
│   │   ├── PayloadDiff/
│   │   │   └── DiffViewer.tsx       # Side-by-side or inline diff
│   │   ├── DeliveryLog/
│   │   │   ├── LogTable.tsx         # Delivery attempts table
│   │   │   └── DLQPanel.tsx         # Dead letter queue management
│   │   ├── Charts/
│   │   │   ├── ThroughputChart.tsx  # Events/min over time
│   │   │   └── LatencyChart.tsx     # Extraction + delivery latency
│   │   ├── Routing/
│   │   │   └── RulesEditor.tsx      # Create/edit routing rules
│   │   └── Replay/
│   │       └── ReplayPanel.tsx      # Time machine UI
│   ├── lib/
│   │   ├── sse.ts                   # SSE connection and event dispatch
│   │   ├── api.ts                   # REST API calls
│   │   └── types.ts                 # TypeScript types mirroring Go models
│   └── stores/
│       ├── webhooks.ts              # createStore for webhook stream
│       ├── metrics.ts               # throughput/latency signal stores
│       └── alerts.ts                # drift alerts signal store
├── index.html
├── vite.config.ts
└── tailwind.config.js
```

---

## SSE Implementation (`src/lib/sse.ts`)

### Go Side: SSE Server (`cmd/sse/main.go`) — Port 8081

Build a new binary. This is the only new Go binary in Phase 4.

```go
// SSE endpoint
// GET /events?source_id=stripe-prod   (filter by source, optional)
// GET /events                          (all events)

// Each SSE event has a type and JSON data:
// event: webhook.received
// data: {"event_id":"...","source_id":"...","received_at":"..."}

// event: webhook.delivered
// data: {"event_id":"...","destination_id":"...","status_code":200,"duration_ms":45}

// event: extraction.complete
// data: {"event_id":"...","template_id":"...","cache_hit":true,"duration_ms":38}

// event: schema.drift
// data: {"source_id":"...","drift_type":"FIELD_MISSING","field_name":"amount"}

// event: delivery.failed
// data: {"event_id":"...","attempt":3,"next_retry_at":"..."}

// event: dlq.added
// data: {"event_id":"...","source_id":"...","destination_id":"..."}

// event: metrics.update
// data: {"events_per_min":142,"avg_extraction_ms":38,"avg_delivery_ms":210}
```

The SSE server subscribes to a Redis pub/sub channel `webhookmind:events`. Every other service publishes to this channel when something notable happens. The SSE server fans out to all connected browser clients.

### SolidJS Side (`src/lib/sse.ts`)

```typescript
import { createSignal } from "solid-js";

export type SSEEvent = {
  type: string;
  data: Record<string, unknown>;
};

export function createSSEConnection(url: string) {
  const [connected, setConnected] = createSignal(false);
  const handlers = new Map<string, ((data: unknown) => void)[]>();

  const es = new EventSource(url);
  
  es.onopen = () => setConnected(true);
  es.onerror = () => setConnected(false); // browser auto-reconnects
  
  // Register typed event handlers
  // e.g. on("webhook.received", handler)
  function on(eventType: string, handler: (data: unknown) => void) {
    es.addEventListener(eventType, (e: MessageEvent) => {
      handler(JSON.parse(e.data));
    });
  }

  return { connected, on };
}
```

---

## Core Components

### WebhookStream Panel (`src/components/WebhookStream/StreamPanel.tsx`)

The main feature. A live feed of incoming webhooks, newest on top.

**Behavior:**
- Shows last 200 events (circular buffer in SolidJS store — `createStore` with a fixed-length array)
- New event animates in from top using Motion One `animate()` — slide down + fade in, 200ms
- Each card shows: source_id, timestamp, payload preview (first 80 chars), extraction status badge, delivery status
- Clicking a card expands it to show full payload, extracted fields, and diff from previous

**SolidJS pattern to use:**
```typescript
// In stores/webhooks.ts
const [webhooks, setWebhooks] = createStore<WebhookEvent[]>([]);

// On SSE event:
setWebhooks(produce(w => {
  w.unshift(newEvent);       // add to front
  if (w.length > 200) w.pop(); // drop oldest
}));
```

**Why this is correct:** `produce` from `solid-js/store` gives an Immer-like API. Only the one new card's DOM node is created. Nothing else re-renders. This is why we chose SolidJS.

**Animation:**
```typescript
import { animate } from "@motionone/dom";

// In onMount of each WebhookCard:
onMount(() => {
  animate(cardRef, 
    { opacity: [0, 1], y: [-20, 0] }, 
    { duration: 0.2, easing: "ease-out" }
  );
});
```

### Schema Drift Alert (`src/components/SchemaDrift/DriftAlert.tsx`)

Toast notification system for drift events. Red for `FIELD_MISSING`/`TYPE_CHANGED`, yellow for `NEW_FIELD`.

- Appears in top-right corner
- Auto-dismisses after 8 seconds
- Maximum 5 visible at once (oldest dismissed if overflow)
- Uses GSAP for stacking animation (new alert pushes others down)

### Payload Diff Viewer (`src/components/PayloadDiff/DiffViewer.tsx`)

Inline diff display inside expanded webhook card.

```
Visual format:
  ─────────────────────────────
  amount         48500  →  52000   [value changed]
  currency       "INR"             [unchanged]  
  due_date       (absent) → "2024-05-01"  [added]
  po_number      "PO-991"  →  (absent)    [removed]
  ─────────────────────────────
```

Color coding: green for added, red for removed, yellow for changed, grey for unchanged.

### Throughput Chart (`src/components/Charts/ThroughputChart.tsx`)

D3.js line chart showing events-per-minute over the last 60 minutes.

**SolidJS + D3 integration pattern:**
```typescript
// D3 owns the SVG DOM. SolidJS owns the data signal.
// Never mix them — let D3 do all SVG mutations.

const [metrics, setMetrics] = createSignal<MetricPoint[]>([]);

// In createEffect:
createEffect(() => {
  const data = metrics();
  // D3 enters/updates/exits here
  // This effect re-runs only when metrics() changes
});
```

The `metrics.update` SSE event pushes new data points. The D3 chart updates on each point. The line animates rightward.

### Dead Letter Queue Panel (`src/components/DeliveryLog/DLQPanel.tsx`)

Table of failed events with:
- Event ID, source, destination, failure reason, timestamp
- "Retry" button → `POST /api/dlq/:event_id/retry`
- "Discard" button → `POST /api/dlq/:event_id/discard`
- Bulk retry: select multiple → "Retry Selected"

### Routing Rules Editor (`src/components/Routing/RulesEditor.tsx`)

Visual rule builder. No raw JSON editing.

```
Source: [stripe-prod ▼]

Rule: Finance Approval
  IF   extracted.amount    [ > ▼ ]  [ 50000 ]
  AND  extracted.currency  [ = ▼ ]  [ "INR" ]
  
  ROUTE TO: [Finance Approval Webhook ▼]
  
  Priority: [10]
  
  [Test Against Last 5 Webhooks]  [Save Rule]
```

### Replay Panel (`src/components/Replay/ReplayPanel.tsx`)

```
Source: [stripe-prod ▼]
From:   [datetime picker]
To:     [datetime picker  or  "Now"]
Destination: [existing destination ▼]  or  [Custom URL input]

[Start Replay]

── Active Replays ──────────────────────────
stripe-prod replay  |  47 / 203 events  |  ████░░░░  23%  |  [Pause] [Cancel]
```

Progress updates via SSE `event: replay.progress` events.

---

## REST API Endpoints (new additions to `cmd/api`)

```
# Webhook data
GET  /api/sources                              — list all sources
GET  /api/sources/:source_id/webhooks          — paginated webhook history
GET  /api/webhooks/:event_id                   — single event details with diff + extraction

# Schema
GET  /api/sources/:source_id/schema            — current inferred schema
GET  /api/sources/:source_id/drift             — drift events history

# DLQ
GET  /api/dlq                                  — list DLQ entries (filterable by source)
POST /api/dlq/:event_id/retry                  — re-enqueue for delivery
POST /api/dlq/:event_id/discard                — mark resolved, do not retry

# Metrics (for charts)
GET  /api/metrics/throughput?range=1h          — events/min datapoints
GET  /api/metrics/latency?range=1h             — extraction + delivery latency

# Routing rules (Phase 3 already built)
GET/POST   /api/sources/:source_id/rules
PUT/DELETE /api/rules/:rule_id
POST       /api/rules/:rule_id/test

# Replay (Phase 3 already built)
POST   /api/sources/:source_id/replay
GET    /api/replay/:session_id
POST   /api/replay/:session_id/pause
POST   /api/replay/:session_id/resume
```

---

## Redis Keys (new)

```
webhookmind:events          — PUBSUB channel — all services publish here, SSE server subscribes
webhookmind:metrics:tput    — ZSET — (timestamp, events_count) for throughput chart
webhookmind:metrics:lat     — ZSET — (timestamp, avg_ms) for latency chart
```

---

## New Go Binary: SSE Server (`cmd/sse/main.go`) — Port 8081

```go
// Nginx already routes /events to port 8081

// On client connect:
//   Start Redis SUBSCRIBE webhookmind:events
//   Forward each message as typed SSE event to client
//   On disconnect: unsubscribe, cleanup

// CRITICAL: set these headers for SSE to work through Nginx
// Content-Type: text/event-stream
// Cache-Control: no-cache
// X-Accel-Buffering: no   ← disables Nginx buffering for this connection
```

---

## New Environment Variables

```env
# SSE Server
SSE_PORT=8081

# Dashboard
DASHBOARD_PORT=3000   # Vite dev server, Nginx serves static build in prod
```

---

## Nginx Config Update

Update `deployments/nginx/webhookmind.conf` to serve the built SolidJS app:

```nginx
server {
    listen 80;
    server_name _;

    # SolidJS static build
    root /var/www/webhookmind/dist;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;  # SPA routing
    }

    location /webhook/ {
        proxy_pass http://127.0.0.1:8080;
        # ... same as before
    }

    location /api/ {
        proxy_pass http://127.0.0.1:8082;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
    }

    location /events {
        proxy_pass             http://127.0.0.1:8081;
        proxy_http_version     1.1;
        proxy_set_header       Connection '';
        proxy_buffering        off;
        proxy_cache            off;
        proxy_set_header       X-Accel-Buffering no;
        chunked_transfer_encoding on;
        proxy_read_timeout     3600s;  # keep SSE connections alive for 1 hour
    }

    location /files/ {
        proxy_pass http://127.0.0.1:9000/;
    }
}
```

---

## Build & Deploy

```bash
# Build SolidJS for production
cd dashboard
npm run build
# Output: dist/

# Copy to Nginx serve path
cp -r dist/* /var/www/webhookmind/dist/
```

---

## Design Requirements

This section is non-negotiable for a product-quality dashboard.

**Color Scheme:** Dark theme. Background `#0a0a0f`. Cards `#12121a`. Borders `#1e1e2e`. Accent: electric blue `#3b82f6` for active states, green `#22c55e` for success, red `#ef4444` for failures, yellow `#eab308` for warnings.

**Typography:** `JetBrains Mono` for all payload/code content. `Inter` for UI text.

**Webhook cards:** Show a left-border color that indicates status: blue = in-flight, green = delivered, red = failed, yellow = queued.

**Density:** Information-dense. This is a developer tool, not a marketing page. Every pixel should carry data.

**Loading states:** Skeleton screens (pulsing grey blocks) while data loads. Never show a spinner over empty space.

---

## Out of Scope for Phase 4

- User authentication / multi-tenancy
- Email or Slack notifications for drift events (future)
- Mobile-responsive layout (desktop-first for now)
- White-labeling
- Billing / subscription management

---

## Acceptance Criteria — Phase 4 (and full product) is Done When:

1. Dashboard loads and connects to SSE — `connected` indicator shows green
2. Sending `POST /webhook/test-source` with a JSON body causes a card to animate into the stream within 500ms
3. Sending a webhook with a PDF file_url shows extraction progress, then extracted fields on the card
4. Triggering schema drift (removing a required field) causes a red toast in the top-right corner
5. Expanding a webhook card shows the diff from the previous webhook
6. Throughput chart updates in real time as webhooks arrive
7. DLQ panel shows failed deliveries, retry button re-enqueues successfully
8. Routing rule editor saves a rule, and subsequent webhooks route correctly per the rule
9. Replay session starts, progress bar advances, completes
10. Full pipeline test: PDF invoice webhook → extracted fields → delivered to destination with structured data → logged → visible in dashboard in real time
