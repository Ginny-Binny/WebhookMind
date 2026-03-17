# WebhookMind — Phase 2: File Processing

## Prerequisites

Phase 1 must be fully complete and accepted before starting this phase:
- Go ingestion server running and accepting webhooks
- Redis queue operational
- Orchestrator routing events to delivery
- Delivery engine with retry + DLQ working
- ScyllaDB and PostgreSQL schemas live

---

## Goal

Add file-awareness to the pipeline. When a webhook payload contains a `file_url` field, download the file, store it in MinIO, pass it to the C++ extraction engine via gRPC, merge the extracted structured data back into the payload, and forward the enriched payload to the destination.

At the end of Phase 2, a webhook arriving with `{"file_url": "https://example.com/invoice.pdf"}` is forwarded as:
```json
{
  "file_url": "https://example.com/invoice.pdf",
  "extracted": {
    "invoice_number": "INV-2024-0892",
    "amount": 48500,
    "currency": "INR",
    "vendor": "Tata Consultancy Services",
    "due_date": "2024-04-15"
  }
}
```

---

## Absolute Constraints — Never Violate These

- C++ engine exposed only via gRPC. No subprocess calls, no REST, no pipes.
- No external OCR APIs, no cloud services. Everything runs locally.
- All C++ libraries statically linked where possible. Target: single deployable binary.
- Use CMake as the C++ build system. No other build system.
- Memory management in C++: use RAII throughout. No raw `new`/`delete`.
- Go side: never call C++ directly. Always go through gRPC.
- MinIO is the only file store. No local disk storage of files beyond temp processing.

---

## What Changes in Existing Phase 1 Code

### Orchestrator (`cmd/orchestrator/main.go`)

Add file detection logic before pushing to delivery queue:

```
Pull event from webhookmind:queue:incoming
  ↓
Parse raw body as JSON
  ↓
Does JSON contain "file_url" key with a non-empty string value?
  NO  → push to webhookmind:queue:delivery (same as Phase 1)
  YES → push to webhookmind:queue:extraction (NEW queue)
```

### WebhookEvent model (`internal/models/webhook.go`)

Add fields:
```go
type WebhookEvent struct {
    // ... all Phase 1 fields ...
    FileURL       string          // populated if payload contains file_url
    FileStorePath string          // MinIO path after download
    ExtractedData map[string]any  // populated after C++ extraction
    ExtractionMs  int64           // how long extraction took
}
```

---

## New Folder Structure (additions to Phase 1)

```
webhookmind/
├── cmd/
│   └── extractor-bridge/
│       └── main.go          # Go gRPC client that calls C++ engine
│
├── internal/
│   ├── filestore/
│   │   └── minio.go         # MinIO client: upload, download, presign
│   ├── extraction/
│   │   └── client.go        # gRPC client wrapper for C++ engine
│   └── queue/
│       └── redis.go         # ADD: extraction queue operations (same file)
│
├── proto/
│   └── extraction.proto     # gRPC contract between Go and C++
│
└── cpp/
    ├── CMakeLists.txt
    ├── src/
    │   ├── main.cpp          # gRPC server entry point
    │   ├── extractor.cpp     # Dispatcher: routes to PDF/OCR/audio handler
    │   ├── pdf_handler.cpp   # Poppler-based PDF extraction
    │   ├── ocr_handler.cpp   # Tesseract OCR for images
    │   ├── fingerprinter.cpp # Document fingerprinting (Template Learning Phase 1)
    │   └── template_cache.cpp # Redis template cache lookup/store
    ├── include/
    │   ├── extractor.h
    │   ├── pdf_handler.h
    │   ├── ocr_handler.h
    │   ├── fingerprinter.h
    │   └── template_cache.h
    └── proto/
        └── extraction.proto  # Copy of /proto/extraction.proto
```

---

## gRPC Contract (`proto/extraction.proto`)

This is the interface contract between Go and C++. Define it precisely — both sides must implement exactly this.

```protobuf
syntax = "proto3";

package extraction;
option go_package = "github.com/yourusername/webhookmind/internal/extraction/pb";

service ExtractionService {
    rpc Extract(ExtractionRequest) returns (ExtractionResponse);
}

message ExtractionRequest {
    string event_id    = 1;  // for logging/tracing
    string file_path   = 2;  // MinIO path to the file
    string file_type   = 3;  // "pdf" | "image" | "audio" | "csv" | "xml"
    string source_id   = 4;  // for template cache lookup
}

message ExtractionResponse {
    bool   success          = 1;
    string error_message    = 2;  // populated only if success = false
    string extracted_json   = 3;  // JSON string of extracted fields
    string template_id      = 4;  // fingerprint hash, for cache tracking
    bool   cache_hit        = 5;  // was this a fast-path cache hit?
    int64  duration_ms      = 6;
}
```

---

## C++ Extraction Engine

### Build System (`cpp/CMakeLists.txt`)

```cmake
cmake_minimum_required(VERSION 3.16)
project(webhookmind_extractor)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

find_package(PkgConfig REQUIRED)

# Poppler
pkg_check_modules(POPPLER REQUIRED poppler-cpp)

# Tesseract
find_package(Tesseract REQUIRED)

# OpenCV (for image preprocessing before OCR)
find_package(OpenCV REQUIRED)

# Hiredis (Redis C client for template cache)
find_package(hiredis REQUIRED)

# gRPC + Protobuf
find_package(gRPC REQUIRED)
find_package(Protobuf REQUIRED)

# Generate gRPC/protobuf code
get_filename_component(PROTO_FILE "../proto/extraction.proto" ABSOLUTE)
# ... protobuf generation commands ...

add_executable(extractor
    src/main.cpp
    src/extractor.cpp
    src/pdf_handler.cpp
    src/ocr_handler.cpp
    src/fingerprinter.cpp
    src/template_cache.cpp
    ${PROTO_SRCS}
    ${GRPC_SRCS}
)

target_link_libraries(extractor
    ${POPPLER_LIBRARIES}
    Tesseract::libtesseract
    ${OpenCV_LIBS}
    hiredis::hiredis
    gRPC::grpc++
    protobuf::libprotobuf
)
```

### PDF Handler Logic (`src/pdf_handler.cpp`)

Uses Poppler's `cpp` API (not the Qt API):

```
1. Open document: poppler::document::load_from_file(path)
2. For each page: page->text_list() — returns text with bounding boxes
3. Collect: all text blocks with their (x, y, width, height) positions
4. Pass to fingerprinter → get template_id
5. If cache hit: use stored field position map to extract values directly
6. If cache miss: pass raw text + layout to Llama.cpp for field labeling
7. Return extracted fields as JSON string
```

### OCR Handler Logic (`src/ocr_handler.cpp`)

```
1. Load image with OpenCV: cv::imread(path)
2. Preprocess: convert to grayscale, threshold (Otsu's method), deskew
3. Pass preprocessed image to Tesseract: api->SetImage(...)
4. Get text: api->GetUTF8Text()
5. Also get word-level bounding boxes for fingerprinting
6. Same fingerprint + cache path as PDF handler
```

### Fingerprinter Logic (`src/fingerprinter.cpp`)

This is the core IP. Implement exactly this algorithm:

```
Input: vector of TextBlock { string text, float x, float y, float w, float h }

Step 1 — Structural normalization
  - Normalize coordinates to [0, 1] range (divide by page width/height)
  - Round to 2 decimal places (removes minor position jitter between same-template docs)
  - Sort blocks by (y, x) — top-to-bottom, left-to-right reading order

Step 2 — Content classification per block
  - Is this block a label? (ends with ':', all caps, short length < 30 chars)
  - Is this block a value? (adjacent to a label block, contains numbers/dates)
  - Is this block a header? (top 15% of page, larger bounding box)
  - Is this block a footer? (bottom 10% of page)

Step 3 — Structural signature
  Concatenate for each block:
    "{classification}:{norm_x:.2f}:{norm_y:.2f}:{norm_w:.2f}"
  Example: "label:0.05:0.12:0.15|value:0.25:0.12:0.30|header:0.05:0.02:0.90"

Step 4 — Hash
  SHA-256 of the structural signature string
  Take first 32 bytes → hex string → this is the template_id

Output: 64-character hex string (32 bytes SHA-256 as hex)
```

**Why this works:** Two invoices from the same company will have the same label positions (`"Invoice Number:"` always at 0.05, 0.12) even though the actual invoice numbers differ. The fingerprint captures structure, not content.

### Template Cache (`src/template_cache.cpp`)

```
Redis key schema:
  webhookmind:template:{template_id}   →  JSON string of FieldPositionMap

FieldPositionMap: {
  "field_name": {
    "label_text": "Invoice Number:",
    "value_x": 0.25,
    "value_y": 0.12,
    "value_w": 0.30,
    "value_h": 0.04
  },
  ...
}

Lookup flow:
  HIREDIS: GET webhookmind:template:{template_id}
  Hit  → deserialize → extract values at stored positions → return
  Miss → return empty (Go side will trigger Llama extraction)
```

Use `hiredis` (the C Redis client) directly. Do not bring in a heavy C++ Redis wrapper.

---

## MinIO Integration (`internal/filestore/minio.go`)

Use the official MinIO Go SDK: `github.com/minio/minio-go/v7`

Operations needed:
- `Upload(ctx, bucketName, objectPath, reader, size, contentType)` → returns MinIO path
- `Download(ctx, bucketName, objectPath)` → returns `io.ReadCloser`
- `GetPresignedURL(ctx, bucketName, objectPath, expiry)` → for dashboard file preview

Object path convention: `{source_id}/{year}/{month}/{day}/{event_id}/{filename}`

Example: `stripe-prod/2024/04/15/evt_abc123/invoice.pdf`

Bucket name: `webhookmind-files` (create on startup if not exists)

---

## New Redis Keys (additions to Phase 1)

```
webhookmind:queue:extraction    — LIST  — WebhookEvent JSON with file_url populated
webhookmind:template:{id}       — STRING — FieldPositionMap JSON (set from C++ via hiredis)
```

---

## New PostgreSQL Tables

Add to migrations:

```sql
-- Extraction records: track every file extraction
CREATE TABLE extraction_records (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id        TEXT NOT NULL,
    source_id       TEXT NOT NULL,
    file_url        TEXT NOT NULL,
    minio_path      TEXT NOT NULL,
    file_type       TEXT NOT NULL,           -- pdf | image | audio | csv | xml
    template_id     TEXT,                    -- fingerprint hash, NULL if first-time
    cache_hit       BOOLEAN NOT NULL,
    extracted_data  JSONB,
    duration_ms     BIGINT,
    success         BOOLEAN NOT NULL,
    error_message   TEXT,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_extraction_event_id ON extraction_records(event_id);
CREATE INDEX idx_extraction_template_id ON extraction_records(template_id);

-- Template store: persists templates beyond Redis TTL
CREATE TABLE templates (
    template_id         TEXT PRIMARY KEY,
    source_id           TEXT NOT NULL,
    file_type           TEXT NOT NULL,
    field_position_map  JSONB NOT NULL,
    sample_event_id     TEXT,           -- event that generated this template
    confidence_score    FLOAT DEFAULT 1.0,
    use_count           BIGINT DEFAULT 1,
    created_at          TIMESTAMPTZ DEFAULT NOW(),
    updated_at          TIMESTAMPTZ DEFAULT NOW()
);
```

---

## New Environment Variables

```env
# MinIO
MINIO_ENDPOINT=127.0.0.1:9000
MINIO_ACCESS_KEY=webhookmind
MINIO_SECRET_KEY=your-secret-key
MINIO_BUCKET=webhookmind-files
MINIO_USE_SSL=false

# C++ Extraction Engine
EXTRACTOR_GRPC_ADDR=127.0.0.1:50051
EXTRACTOR_GRPC_TIMEOUT_SECONDS=120   # large files take time

# File Processing
FILE_MAX_SIZE_BYTES=52428800         # 50MB max file size
FILE_DOWNLOAD_TIMEOUT_SECONDS=30
```

---

## Go Dependencies (additions)

```
github.com/minio/minio-go/v7        — MinIO SDK
google.golang.org/grpc              — gRPC
google.golang.org/protobuf          — Protobuf
```

---

## System Dependencies (install on VPS)

```bash
# Poppler
apt-get install -y libpoppler-cpp-dev

# Tesseract
apt-get install -y libtesseract-dev tesseract-ocr

# OpenCV
apt-get install -y libopencv-dev

# Hiredis
apt-get install -y libhiredis-dev

# gRPC (build from source or use package)
apt-get install -y libgrpc-dev libprotobuf-dev protobuf-compiler-grpc

# CMake
apt-get install -y cmake build-essential pkg-config
```

---

## File Type Detection

Do NOT trust file extension or Content-Type header from the sender. Detect by magic bytes:

```go
func DetectFileType(data []byte) string {
    // PDF: starts with %PDF
    // JPEG: FF D8 FF
    // PNG: 89 50 4E 47
    // TIFF: 49 49 2A 00 or 4D 4D 00 2A
    // MP3: ID3 header or FF FB
    // WAV: RIFF....WAVE
    // M4A: ftyp box in first 12 bytes
    // CSV/XML: try UTF-8 decode, check first non-whitespace char
}
```

Read first 16 bytes only for detection. Do not load the entire file.

---

## Extraction Pipeline Flow (updated end-to-end)

```
1. Orchestrator detects file_url in payload
2. Push to webhookmind:queue:extraction
3. Extraction bridge worker picks up event
4. Download file from file_url
5. Detect file type from magic bytes
6. Upload to MinIO → get minio_path
7. Call C++ engine via gRPC: Extract(event_id, minio_path, file_type, source_id)
8. C++ engine:
   a. Downloads file from MinIO path (C++ reads directly from MinIO via HTTP)
   b. Runs appropriate handler (PDF/OCR)
   c. Fingerprints the document
   d. Checks Redis template cache
   e. Cache hit  → fast extract using position map → return
   f. Cache miss → [Phase 3: Llama.cpp] → for now, return raw text in extracted_json
9. Go side: merge extracted_json into original payload under "extracted" key
10. Write extraction_record to PostgreSQL
11. Push enriched event to webhookmind:queue:delivery
12. Continue with Phase 1 delivery flow
```

**Note on Phase 2 vs Phase 3 boundary:** In Phase 2, cache-miss documents will return raw extracted text (not labeled fields). Llama.cpp for intelligent field labeling is Phase 3. The pipeline is fully functional in Phase 2 — you just get `{"extracted": {"raw_text": "Invoice Number: INV-001..."}}` instead of structured fields on first encounter.

---

## Out of Scope for Phase 2

- Whisper.cpp (audio transcription) — Phase 3
- Llama.cpp (LLM-based field labeling) — Phase 3
- Schema inference / drift detection — Phase 3
- Payload diffing — Phase 3
- Condition-based routing — Phase 3
- SSE / frontend — Phase 4

---

## Acceptance Criteria — Phase 2 is Done When:

1. `POST /webhook/test-source` with `{"file_url": "https://example.com/invoice.pdf"}` triggers file download
2. The file is stored in MinIO at the correct path convention
3. C++ gRPC server receives the extraction request and returns without crashing
4. PDF text is extracted via Poppler and returned in `extracted_json`
5. Image text is extracted via Tesseract + OpenCV preprocessing
6. Document fingerprint is computed and stored in Redis + PostgreSQL templates table
7. Second identical document type hits the template cache (cache_hit = true in response)
8. Enriched payload (with `extracted` key) is forwarded to destination
9. `extraction_records` table is populated with correct metadata
10. File-less webhooks still go through fast lane without touching C++ engine
11. C++ engine handles corrupt/unreadable files gracefully — returns error, does not crash
12. MinIO bucket is created on startup if it doesn't exist
