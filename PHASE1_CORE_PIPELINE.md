# WebhookMind — Phase 1: Core Pipeline

## Goal

Build the foundational webhook pipeline: accept a webhook POST, acknowledge immediately with 202, push to a Redis queue, have a worker pick it up, attempt delivery to a destination with exponential backoff retry, log the raw event to ScyllaDB, and track delivery status in PostgreSQL. No file processing. No enrichment. Just a reliable, production-grade pipe with retry logic and dead letter queue.

At the end of Phase 1, you have a working webhook relay system that can receive, queue, deliver, retry, and log — with all storage layers wired up and a working Nginx config.

---

## Absolute Constraints — Never Violate These

- Language: Go only. No Node.js, no Python.
- No third-party APIs of any kind.
- Every error must be logged. No silent failures. Never ignore an `err` return.
- Graceful shutdown on SIGINT/SIGTERM — drain in-flight work before exit.
- All config via environment variables. No hardcoded values anywhere.
- Production-quality code. Proper logging with structured fields (use `log/slog`, Go's standard structured logger).

---

## Monorepo Folder Structure

Create exactly this structure. Do not deviate.

```
webhookmind/
├── cmd/
│   ├── ingestion/
│   │   └── main.go          # Ingestion server binary
│   ├── orchestrator/
│   │   └── main.go          # Orchestrator binary
│   └── delivery/
│       └── main.go          # Delivery engine binary
│
├── internal/
│   ├── config/
│   │   └── config.go        # Env var loading, Config struct
│   ├── models/
│   │   └── webhook.go       # Shared data structs (WebhookEvent, DeliveryAttempt, etc.)
│   ├── queue/
│   │   └── redis.go         # Redis queue: enqueue, dequeue, DLQ operations
│   ├── store/
│   │   ├── scylla.go        # ScyllaDB: raw event write
│   │   └── postgres.go      # PostgreSQL: delivery status CRUD
│   ├── delivery/
│   │   └── engine.go        # HTTP delivery, retry logic, backoff
│   └── ingestion/
│       └── handler.go       # HTTP handler for POST /webhook/:source_id
│
├── migrations/
│   ├── scylla/
│   │   └── 001_events.cql   # ScyllaDB keyspace + table
│   └── postgres/
│       └── 001_init.sql     # PostgreSQL tables
│
├── deployments/
│   └── nginx/
│       └── webhookmind.conf # Nginx config
│
├── go.mod
├── go.sum
└── .env.example
```

---

## Services — What Each Binary Does

### 1. Ingestion Server (`cmd/ingestion`) — Port 8080

**Single responsibility:** Accept webhook POST requests and immediately enqueue them. That's it.

- Listens on `0.0.0.0:8080`
- Route: `POST /webhook/:source_id`
- On receive:
  1. Parse body (raw bytes — do NOT assume JSON, payloads can be anything)
  2. Capture all headers (many senders put auth/metadata in headers)
  3. Build a `WebhookEvent` struct with a generated UUID, source_id, timestamp, raw body, headers
  4. Push to Redis queue (key: `webhookmind:queue:incoming`)
  5. Return `202 Accepted` with `{"id": "<event_uuid>"}` — **before** any processing
- If Redis push fails: return `503 Service Unavailable`, log the error
- No auth on ingestion endpoint in Phase 1 (added in Phase 3)
- Request timeout: 5 seconds max to read body

### 2. Orchestrator (`cmd/orchestrator`)

**Single responsibility:** Pull events from Redis queue and route them to the delivery engine. In Phase 1, all events go directly to delivery (no file processing yet).

- Runs a configurable worker pool (default: 10 workers, controlled by `ORCHESTRATOR_WORKERS` env var)
- Each worker: BLPOP from `webhookmind:queue:incoming` (blocking pop, 5s timeout)
- In Phase 1: no file detection, just push every event to `webhookmind:queue:delivery`
- Worker pool must scale: if queue depth > 1000, spin up additional workers up to max cap (`ORCHESTRATOR_MAX_WORKERS`, default: 50)
- Check queue depth every 10 seconds using `LLEN` command

### 3. Delivery Engine (`cmd/delivery`) — Internal only, no public port

**Single responsibility:** Pull from delivery queue, attempt HTTP delivery, retry with backoff, move to DLQ after exhaustion.

- Workers pull from `webhookmind:queue:delivery`
- For each event, look up destinations from PostgreSQL (`destinations` table, filtered by `source_id`)
- Fan-out: if a source has 3 destinations, fire 3 goroutines, one per destination
- Retry schedule: 5s → 30s → 2m → 10m (4 total attempts, then DLQ)
- A delivery attempt is a success if destination returns any 2xx status
- On success: update `delivery_attempts` table, write raw event to ScyllaDB
- On exhaustion: push to `webhookmind:dlq` Redis key, update status to `failed`
- Per-destination timeout: configurable per row in `destinations` table, default 30s
- Do NOT retry on 4xx responses (permanent failure — destination rejected it)
- DO retry on 5xx, timeouts, and connection errors

---

## Data Models (`internal/models/webhook.go`)

```go
// WebhookEvent — the canonical event object that flows through the entire pipeline
type WebhookEvent struct {
    ID        string            // UUID v4
    SourceID  string            // from URL param :source_id
    ReceivedAt time.Time        // when ingestion server received it
    RawBody   []byte            // untouched original payload
    Headers   map[string]string // all incoming headers
    // Phase 2 will add: FileURL, ExtractedData
}

// DeliveryAttempt — one attempt to deliver to one destination
type DeliveryAttempt struct {
    ID              string
    EventID         string
    DestinationID   string
    AttemptNumber   int
    AttemptedAt     time.Time
    StatusCode      int       // 0 if connection failed
    Success         bool
    ErrorMessage    string
    DurationMs      int64
}
```

---

## ScyllaDB Schema (`migrations/scylla/001_events.cql`)

```cql
CREATE KEYSPACE IF NOT EXISTS webhookmind
    WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};

USE webhookmind;

CREATE TABLE IF NOT EXISTS webhook_events (
    source_id   TEXT,
    received_at TIMESTAMP,
    event_id    UUID,
    raw_body    BLOB,
    headers     MAP<TEXT, TEXT>,
    PRIMARY KEY ((source_id), received_at, event_id)
) WITH CLUSTERING ORDER BY (received_at DESC, event_id ASC)
  AND default_time_to_live = 2592000;  -- 30 days TTL
```

**Partition key reasoning:** `source_id` — all events from one source live on the same partition. Most queries will be "give me all events from source X in time range Y to Z". This makes that query a single-partition scan. Never scatter-gather.

**Clustering key reasoning:** `received_at DESC` — latest events come first in storage order, matching the most common read pattern (dashboard shows newest first). `event_id` breaks ties for events with same millisecond timestamp.

**TTL:** Raw events expire after 30 days. Time Machine replay (Phase 3) works within this window.

---

## PostgreSQL Schema (`migrations/postgres/001_init.sql`)

```sql
-- Sources: registered webhook senders
CREATE TABLE sources (
    id          TEXT PRIMARY KEY,         -- e.g. 'stripe-prod', 'typeform-leads'
    name        TEXT NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Destinations: where to forward webhooks
CREATE TABLE destinations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id       TEXT NOT NULL REFERENCES sources(id),
    name            TEXT NOT NULL,
    url             TEXT NOT NULL,
    timeout_seconds INT NOT NULL DEFAULT 30,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_destinations_source_id ON destinations(source_id);

-- Delivery attempts: full audit log of every delivery try
CREATE TABLE delivery_attempts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id        TEXT NOT NULL,
    source_id       TEXT NOT NULL,
    destination_id  UUID NOT NULL REFERENCES destinations(id),
    attempt_number  INT NOT NULL,
    attempted_at    TIMESTAMPTZ DEFAULT NOW(),
    status_code     INT,                  -- NULL if connection failed
    success         BOOLEAN NOT NULL,
    error_message   TEXT,
    duration_ms     BIGINT
);

CREATE INDEX idx_delivery_attempts_event_id ON delivery_attempts(event_id);
CREATE INDEX idx_delivery_attempts_source_id ON delivery_attempts(source_id);
CREATE INDEX idx_delivery_attempts_attempted_at ON delivery_attempts(attempted_at DESC);

-- Dead letter queue entries
CREATE TABLE dead_letter_queue (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id        TEXT NOT NULL,
    source_id       TEXT NOT NULL,
    destination_id  UUID NOT NULL REFERENCES destinations(id),
    raw_body        BYTEA NOT NULL,
    headers         JSONB NOT NULL DEFAULT '{}',
    failed_at       TIMESTAMPTZ DEFAULT NOW(),
    failure_reason  TEXT,
    resolved        BOOLEAN NOT NULL DEFAULT FALSE,
    resolved_at     TIMESTAMPTZ
);

CREATE INDEX idx_dlq_source_id ON dead_letter_queue(source_id);
CREATE INDEX idx_dlq_resolved ON dead_letter_queue(resolved) WHERE resolved = FALSE;
```

---

## Redis Key Schema

Use these exact key names. Consistency matters because Phase 2 and 3 add more keys.

```
webhookmind:queue:incoming     — LIST  — raw WebhookEvent JSON, ingestion pushes here
webhookmind:queue:delivery     — LIST  — WebhookEvent JSON ready for delivery
webhookmind:dlq                — LIST  — exhausted events (also written to Postgres)
webhookmind:queue:depth        — (derived via LLEN, not stored)
```

All values stored as JSON-serialized `WebhookEvent`. Use `RPUSH` to enqueue, `BLPOP` to dequeue (blocking, 5s timeout so workers don't spin).

---

## Environment Variables (`.env.example`)

```env
# Ingestion
INGESTION_PORT=8080
INGESTION_MAX_BODY_BYTES=10485760   # 10MB max webhook body

# Orchestrator
ORCHESTRATOR_WORKERS=10
ORCHESTRATOR_MAX_WORKERS=50
ORCHESTRATOR_QUEUE_SCALE_THRESHOLD=1000

# Delivery
DELIVERY_WORKERS=20
DELIVERY_MAX_WORKERS=100
DELIVERY_MAX_RETRIES=4

# Redis
REDIS_ADDR=127.0.0.1:6379
REDIS_PASSWORD=
REDIS_DB=0

# ScyllaDB
SCYLLA_HOSTS=127.0.0.1
SCYLLA_KEYSPACE=webhookmind

# PostgreSQL
POSTGRES_DSN=postgres://webhookmind:password@127.0.0.1:5432/webhookmind?sslmode=disable

# Logging
LOG_LEVEL=info    # debug | info | warn | error
```

---

## Go Dependencies

```
github.com/google/uuid              — UUID v4 generation
github.com/redis/go-redis/v9        — Redis client (official, v9)
github.com/gocql/gocql              — ScyllaDB/Cassandra driver
github.com/jackc/pgx/v5             — PostgreSQL driver (pgx, not database/sql)
github.com/go-chi/chi/v5            — HTTP router (lightweight, idiomatic)
```

Initialize with: `go mod init github.com/yourusername/webhookmind`

---

## Nginx Config (`deployments/nginx/webhookmind.conf`)

```nginx
server {
    listen 80;
    server_name _;

    # Webhook ingestion
    location /webhook/ {
        proxy_pass         http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header   Host $host;
        proxy_set_header   X-Real-IP $remote_addr;
        proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 10s;
        client_max_body_size 10m;
    }

    # API (Phase 4 — placeholder)
    location /api/ {
        proxy_pass http://127.0.0.1:8082;
    }

    # SSE stream (Phase 4 — placeholder)
    location /events {
        proxy_pass             http://127.0.0.1:8081;
        proxy_http_version     1.1;
        proxy_set_header       Connection '';
        proxy_buffering        off;
        proxy_cache            off;
        chunked_transfer_encoding on;
    }
}
```

SSL via Certbot will be added at deployment time. This config is for local dev and initial deploy.

---

## Retry Logic — Exact Implementation

The delivery engine must implement this precisely:

```
Attempt 1 — immediate
Attempt 2 — wait 5 seconds
Attempt 3 — wait 30 seconds
Attempt 4 — wait 2 minutes
After attempt 4 fails → move to DLQ (Redis + PostgreSQL)
```

Implementation: use `time.Sleep` inside the goroutine handling that specific delivery. Do NOT use a cron or scheduler — each delivery manages its own retry timeline. Store attempt state in PostgreSQL so retries survive process restart.

On process restart, the delivery engine must query PostgreSQL for `delivery_attempts` where `success = false` AND `attempt_number < 4` AND `attempted_at < NOW() - retry_delay` and re-enqueue them.

---

## Logging Standard

Use Go's `log/slog` package. Every log line must include:
- `event_id` — on any log related to a specific webhook
- `source_id` — on any log related to a specific source
- `component` — which service/file is logging (e.g. `"ingestion"`, `"delivery"`)
- `error` — on any error log

Example:
```go
slog.Error("delivery failed",
    "component", "delivery",
    "event_id", event.ID,
    "source_id", event.SourceID,
    "destination_id", dest.ID,
    "attempt", attemptNum,
    "error", err,
)
```

---

## Graceful Shutdown Pattern

Every binary must handle this:

```go
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
// pass ctx to all workers
// on ctx.Done(): stop accepting new work, finish in-flight work, then exit
```

Workers must check `ctx.Done()` and finish their current job before exiting. Do not kill in-flight deliveries.

---

## Out of Scope for Phase 1

Do NOT build these — they belong to later phases:

- File URL detection or downloading
- C++ extraction engine or gRPC
- MinIO or any file storage
- Schema inference or drift detection
- Payload diffing
- Condition-based routing
- Time Machine replay
- SSE real-time stream
- SolidJS frontend
- Authentication / API keys
- Dashboard or any UI
- Webhook signature verification (e.g. Stripe HMAC)

---

## Acceptance Criteria — Phase 1 is Done When:

1. `POST /webhook/test-source` with any JSON body returns `202` within 50ms
2. The event appears in Redis `webhookmind:queue:incoming`
3. Orchestrator picks it up and moves it to `webhookmind:queue:delivery`
4. Delivery engine POSTs it to a configured destination URL
5. A successful delivery is logged in `delivery_attempts` table with `success = true`
6. If destination returns 500, retry happens at 5s, 30s, 2m, 10m intervals
7. After 4 failed attempts, event appears in `webhookmind:dlq` AND `dead_letter_queue` table
8. Raw event is written to ScyllaDB `webhook_events` table
9. All three binaries shut down gracefully on SIGTERM (drain in-flight, then exit)
10. All config is via env vars — zero hardcoded values
