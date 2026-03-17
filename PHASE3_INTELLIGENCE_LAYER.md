# WebhookMind — Phase 3: Intelligence Layer

## Prerequisites

Phase 2 must be fully complete and accepted:
- File download, MinIO storage, and C++ extraction engine working
- PDF (Poppler) and Image (Tesseract + OpenCV) extraction returning raw text
- Template fingerprinting storing to Redis and PostgreSQL
- gRPC bridge between Go and C++ stable
- Enriched payload forwarding to destinations

---

## Goal

Make the system intelligent. Phase 2 gave us raw text extraction. Phase 3 gives us:

1. **Llama.cpp** — local LLM that labels extracted fields ("this text is an invoice number", "this is an amount")
2. **Whisper.cpp** — local audio transcription
3. **Schema Inference** — after N webhooks from a source, infer the expected payload structure
4. **Drift Detection** — alert when a source's payload schema changes
5. **Payload Diffing** — field-by-field comparison of consecutive webhooks from the same source
6. **Condition-Based Routing** — route to different destinations based on extracted field values

At the end of Phase 3, the system is feature-complete on the backend. A webhook with an invoice PDF gets fully structured extraction. Audio gets transcribed. Schema drift fires alerts. Routing conditions work on file content.

---

## Absolute Constraints

- Llama.cpp runs fully locally. No API calls to OpenAI, Anthropic, or any cloud.
- Whisper.cpp runs fully locally. No cloud STT.
- Model files live on the VPS filesystem. Do NOT download at runtime — download during setup.
- Recommended models: `llama-3.2-3b` (quantized GGUF, ~2GB) for extraction, `whisper-base.en` for audio
- All new features are additive — they must not break Phase 1 or Phase 2 behavior
- Diffing and schema inference must never block the delivery pipeline

---

## Changes to C++ Extraction Engine

### Add: Llama.cpp Integration (`cpp/src/llm_labeler.cpp`)

This is the slow path — only called when template cache misses (first encounter of a new document type).

```cpp
// llm_labeler.h
class LLMLabeler {
public:
    explicit LLMLabeler(const std::string& model_path);
    
    // Takes raw extracted text + layout, returns JSON of labeled fields
    std::string LabelFields(
        const std::string& raw_text,
        const std::vector<TextBlock>& layout,
        const std::string& file_type
    );
    
private:
    // llama_context*, llama_model* — RAII wrappers
};
```

**Prompt design for field labeling:**

```
System: You are a structured data extractor. Given text extracted from a document, 
identify and extract key fields as JSON. Return ONLY valid JSON, no explanation.

Common fields to look for: invoice_number, amount, currency, vendor, customer, 
date, due_date, po_number, tax_amount, total_amount, description.

For other document types, infer appropriate field names from context.
Always return snake_case field names. Amounts should be numeric, not strings.

User: Extract fields from this document text:
{raw_text}

Return format: {"field_name": value, ...}
```

**Integration into extraction flow:**
```
Cache miss path (Phase 2 previously returned raw_text):
  raw_text → LLMLabeler::LabelFields() → structured JSON
  structured JSON + layout → fingerprinter stores (fingerprint → field_position_map)
  Next encounter: cache hit, skip Llama entirely
```

### Add: Whisper.cpp Integration (`cpp/src/audio_handler.cpp`)

```cpp
class AudioHandler {
public:
    explicit AudioHandler(const std::string& model_path);
    
    struct TranscriptionResult {
        std::string full_text;
        std::vector<Segment> segments;  // {start_ms, end_ms, text}
        std::string language;
        float confidence;
    };
    
    TranscriptionResult Transcribe(const std::string& file_path);
};
```

After transcription, pass `full_text` to `LLMLabeler` to extract entities (same labeling pipeline as documents).

### Update: gRPC Proto (`proto/extraction.proto`)

Add audio support:

```protobuf
// Add to ExtractionResponse:
message TranscriptionSegment {
    int64  start_ms = 1;
    int64  end_ms   = 2;
    string text     = 3;
}

message ExtractionResponse {
    // ... existing fields ...
    repeated TranscriptionSegment segments = 7;  // for audio only
    string detected_language               = 8;  // for audio only
}
```

### Update CMakeLists.txt

Add Whisper.cpp and Llama.cpp (both are header-heavy, source-included libraries):

```cmake
# Whisper.cpp — include as subdirectory
add_subdirectory(vendor/whisper.cpp)

# Llama.cpp — include as subdirectory  
add_subdirectory(vendor/llama.cpp)

target_link_libraries(extractor
    # ... existing ...
    whisper
    llama
)
```

Clone both into `cpp/vendor/` directory:
- `git clone https://github.com/ggerganov/whisper.cpp cpp/vendor/whisper.cpp`
- `git clone https://github.com/ggerganov/llama.cpp cpp/vendor/llama.cpp`

---

## Schema Inference Engine (`internal/schema/inference.go`)

### What It Does

After receiving N webhooks from the same source (default N=10, configurable), infer the expected schema. Store it. Validate every future webhook against it.

### Schema Representation

```go
type FieldSchema struct {
    Name     string   `json:"name"`
    Type     string   `json:"type"`      // "string" | "number" | "boolean" | "object" | "array" | "null"
    Nullable bool     `json:"nullable"`  // was it absent in any observed webhook?
    Examples []any    `json:"examples"`  // up to 3 sample values
}

type PayloadSchema struct {
    SourceID        string                 `json:"source_id"`
    Fields          map[string]FieldSchema `json:"fields"`
    SampleCount     int                    `json:"sample_count"`
    InferredAt      time.Time              `json:"inferred_at"`
    Version         int                    `json:"version"`       // increments on schema update
}
```

### Inference Algorithm

```
For each new webhook from source_id:
  1. Parse payload as JSON
  2. Flatten all fields (including nested, use dot notation: "invoice.amount")
  3. For each field: determine type, record presence/absence
  4. Accumulate into running schema (stored in PostgreSQL)
  
After sample_count >= SCHEMA_INFERENCE_MIN_SAMPLES (default 10):
  Schema is "locked" — all future webhooks validated against it
  A field is "required" if it was present in 100% of samples
  A field is "nullable" if it was absent in any sample
```

### Drift Detection

```
On every webhook after schema is locked:
  1. Parse payload
  2. For each expected required field: present? type match?
  3. For each observed field: is it in schema? (new field = drift)
  
Drift types:
  FIELD_MISSING    — required field not present
  TYPE_CHANGED     — field present but different type than schema
  NEW_FIELD        — field not in schema (may indicate API version change)
  
On drift detected:
  - Log with severity based on drift type
  - Push drift event to SSE stream (Phase 4 will display it)
  - Store in drift_events table
  - Do NOT block delivery — still forward the webhook
```

### PostgreSQL Tables (new)

```sql
-- Stored schemas per source
CREATE TABLE payload_schemas (
    source_id       TEXT PRIMARY KEY REFERENCES sources(id),
    schema_data     JSONB NOT NULL,
    sample_count    INT NOT NULL DEFAULT 0,
    version         INT NOT NULL DEFAULT 1,
    is_locked       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Drift events: every detected schema violation
CREATE TABLE drift_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id        TEXT NOT NULL,
    source_id       TEXT NOT NULL REFERENCES sources(id),
    drift_type      TEXT NOT NULL,   -- FIELD_MISSING | TYPE_CHANGED | NEW_FIELD
    field_name      TEXT NOT NULL,
    expected_type   TEXT,
    actual_type     TEXT,
    details         JSONB,
    detected_at     TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_drift_events_source_id ON drift_events(source_id, detected_at DESC);
```

---

## Payload Diffing Engine (`internal/diff/engine.go`)

### What It Does

Every webhook from a source is compared to the previous one from the same source. The diff shows exactly what changed.

### Algorithm

```
Input: current WebhookEvent, previous WebhookEvent (same source_id)
Output: DiffResult

DiffResult:
  - Added   map[string]any  — fields in current but not previous
  - Removed map[string]any  — fields in previous but not current
  - Changed []FieldChange    — fields present in both but value/type differs
  - Unchanged []string       — field names with identical values (for completeness)

FieldChange:
  - FieldPath  string  — dot-notation path, e.g. "invoice.amount"
  - OldValue   any
  - NewValue   any
  - ChangeType string  — "value_changed" | "type_changed"
```

### Implementation Notes

- Flatten both JSONs to dot-notation maps before diffing (handles nested objects uniformly)
- For arrays: compare element by element; if lengths differ, report as "array_length_changed"
- This is fundamentally a recursive JSON comparison — your competitive programming background applies directly here (think of it as a tree diff problem)
- Store diff in PostgreSQL `webhook_diffs` table
- Do NOT block delivery while computing diff — run diff in a goroutine, write async

```sql
CREATE TABLE webhook_diffs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id        TEXT NOT NULL,
    source_id       TEXT NOT NULL,
    prev_event_id   TEXT,            -- NULL if first webhook from this source
    diff_data       JSONB NOT NULL,
    computed_at     TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_webhook_diffs_source_id ON webhook_diffs(source_id, computed_at DESC);
```

### Previous Event Lookup

To find the previous event for diffing:
- Query PostgreSQL: `SELECT event_id, extracted_data FROM delivery_attempts WHERE source_id = $1 AND success = true ORDER BY attempted_at DESC LIMIT 1 OFFSET 1`
- Cache the latest event_id per source in Redis: `webhookmind:lastevt:{source_id}` → `event_id`

---

## Condition-Based Routing Engine (`internal/routing/engine.go`)

### What It Does

Instead of all webhooks from a source going to all configured destinations, routing rules decide which destination receives which webhook based on the enriched payload (including extracted file content).

### Rule Schema

```go
type RoutingRule struct {
    ID            string
    SourceID      string
    DestinationID string
    Priority      int        // lower number = higher priority, evaluated first
    Conditions    []Condition
    LogicOperator string     // "AND" | "OR"
}

type Condition struct {
    Field    string  // dot-notation path, e.g. "extracted.amount" or "headers.x-event-type"
    Operator string  // "eq" | "neq" | "gt" | "gte" | "lt" | "lte" | "contains" | "exists" | "not_exists"
    Value    any
}
```

### Evaluation Algorithm

```
Input: enriched WebhookEvent, []RoutingRule for this source_id

For each rule (sorted by priority ASC):
  Evaluate all conditions:
    AND logic: all conditions must be true
    OR logic:  at least one condition must be true
  
  If rule matches: add destination_id to delivery targets

If no rules match: fall back to default destinations (is_default=true in destinations table)
```

### PostgreSQL Table

```sql
CREATE TABLE routing_rules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id       TEXT NOT NULL REFERENCES sources(id),
    destination_id  UUID NOT NULL REFERENCES destinations(id),
    name            TEXT NOT NULL,
    priority        INT NOT NULL DEFAULT 100,
    logic_operator  TEXT NOT NULL DEFAULT 'AND',
    conditions      JSONB NOT NULL DEFAULT '[]',
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_routing_rules_source_id ON routing_rules(source_id, priority ASC);

-- Add is_default to destinations table
ALTER TABLE destinations ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT TRUE;
```

### REST API for Rule Management (new endpoints)

```
POST   /api/sources/:source_id/rules       — create routing rule
GET    /api/sources/:source_id/rules       — list routing rules
PUT    /api/rules/:rule_id                 — update rule
DELETE /api/rules/:rule_id                 — delete rule
POST   /api/rules/:rule_id/test            — test rule against a past webhook event
```

Add a new Go binary `cmd/api` for REST API handling (port 8082). Nginx already has this placeholder.

---

## Time Machine Replay (`internal/replay/engine.go`)

### What It Does

Replay all webhooks from a specific point in time forward, in exact original order, to any destination.

### Algorithm

```
Input: source_id, from_timestamp, to_destination_url

1. Query ScyllaDB for all events:
   SELECT * FROM webhook_events 
   WHERE source_id = ? AND received_at >= ? 
   ORDER BY received_at ASC, event_id ASC

2. For each event in order:
   a. Re-enrich (re-run extraction if needed, or use cached extracted_data from PostgreSQL)
   b. Deliver to to_destination_url
   c. Maintain ordering guarantee: wait for ACK before sending next
   d. Record replay attempt in replay_log table
   e. Respect original timing (optional: replay at 1x or Nx speed)
```

### Key Design Constraint: Order Preservation

ScyllaDB's clustering key `(received_at ASC, event_id ASC)` guarantees events come back in exact original order. This is why the ScyllaDB schema was designed this way in Phase 1.

```sql
-- PostgreSQL replay log
CREATE TABLE replay_sessions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id       TEXT NOT NULL,
    destination_url TEXT NOT NULL,
    from_timestamp  TIMESTAMPTZ NOT NULL,
    to_timestamp    TIMESTAMPTZ,          -- NULL means up to now
    status          TEXT NOT NULL,        -- running | completed | failed | paused
    events_replayed INT NOT NULL DEFAULT 0,
    events_total    INT,
    started_at      TIMESTAMPTZ DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    initiated_by    TEXT                  -- for audit
);
```

### REST Endpoints

```
POST /api/sources/:source_id/replay    — start replay session
GET  /api/replay/:session_id           — check replay status
POST /api/replay/:session_id/pause     — pause
POST /api/replay/:session_id/resume    — resume
DELETE /api/replay/:session_id         — cancel
```

---

## Updated Pipeline Flow (Phase 3 complete)

```
Webhook POST arrives
  → 202 immediately
  → Redis queue:incoming
  → Orchestrator picks up
      → Has file_url? 
          YES → queue:extraction → Download → MinIO → C++ gRPC
                   C++ engine:
                     Fingerprint → Redis cache check
                     Cache hit  → Fast extract (<50ms)
                     Cache miss → Poppler/Tesseract → Llama.cpp label → Store template
                   Return extracted_json
          NO  → queue:delivery (fast lane)
  → Enrichment:
      1. Merge extracted_json into payload["extracted"]
      2. Schema inference: update running schema, check drift
      3. Drift detected? → push to drift_events, fire SSE alert
      4. Compute diff against previous webhook (async goroutine)
      5. Evaluate routing rules → determine delivery targets
  → Delivery to matched destinations
  → ScyllaDB: write raw event
  → PostgreSQL: write delivery status, drift events, diff
  → SSE: push update to dashboard
```

---

## New Environment Variables

```env
# LLM
LLAMA_MODEL_PATH=/opt/models/llama-3.2-3b-instruct.Q4_K_M.gguf
LLAMA_CONTEXT_SIZE=4096
LLAMA_MAX_TOKENS=512
LLAMA_GPU_LAYERS=0    # 0 = CPU only, increase if GPU available

# Whisper
WHISPER_MODEL_PATH=/opt/models/whisper-base.en.bin
WHISPER_LANGUAGE=auto  # auto-detect or specify e.g. "en"

# Schema Inference
SCHEMA_INFERENCE_MIN_SAMPLES=10
SCHEMA_DRIFT_ALERT_ENABLED=true

# API Server
API_PORT=8082

# Replay
REPLAY_MAX_EVENTS_PER_SESSION=100000
```

---

## Model Download (run once during VPS setup)

```bash
# Create models directory
mkdir -p /opt/models

# Llama 3.2 3B Instruct (Q4 quantized, ~2GB)
wget https://huggingface.co/bartowski/Llama-3.2-3B-Instruct-GGUF/resolve/main/Llama-3.2-3B-Instruct-Q4_K_M.gguf \
  -O /opt/models/llama-3.2-3b-instruct.Q4_K_M.gguf

# Whisper Base English (~150MB)
wget https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin \
  -O /opt/models/whisper-base.en.bin
```

---

## Out of Scope for Phase 3

- SolidJS frontend — Phase 4
- SSE implementation — Phase 4 (Phase 3 fires events to a channel; Phase 4 consumes them)
- Dashboard UI for drift visualization — Phase 4
- User authentication — Phase 4

---

## Acceptance Criteria — Phase 3 is Done When:

1. Invoice PDF extraction returns fully labeled fields (`invoice_number`, `amount`, `vendor`, etc.) not just raw text
2. Second invoice from same template extracts in <50ms (Llama not called)
3. Audio file (MP3/WAV) is transcribed and key entities extracted
4. After 10 webhooks from a source, `payload_schemas` table has a locked schema
5. Webhook missing a previously-required field creates a row in `drift_events`
6. Two consecutive webhooks from same source produce a diff in `webhook_diffs`
7. Routing rule `extracted.amount > 50000` routes to the correct destination
8. Routing rule with no match falls back to default destination
9. Replay session for a source replays events in exact original order
10. All new features are non-blocking — delivery pipeline performance unchanged for file-less webhooks
