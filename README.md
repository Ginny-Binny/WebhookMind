<p align="center">
  <img src="assets/logo.svg" alt="webhookmind" width="340" />
</p>

An unstructured-webhook processor. Point messy payloads at it — JSON, attached PDFs, images, audio — and it extracts the content, infers a schema on the fly, flags drift when payloads change shape, evaluates routing rules, and streams the whole thing to a live dashboard.

## Running it locally

You need Docker, Go 1.22+, and Node 20+.

First time only:

```powershell
go install github.com/mattn/goreman@latest
Copy-Item .env.example .env
cd dashboard; npm install; cd ..
```

Then every time:

```powershell
docker start webhookmind-redis webhookmind-scylla webhookmind-postgres webhookmind-minio webhookmind-extractor
goreman start
```

Dashboard is at [localhost:3000](http://localhost:3000). Fire a test webhook:

```powershell
curl.exe -X POST http://localhost:8080/webhook/test-source `
  -H "Content-Type: application/json" `
  -d '{"order_id":"TEST-1","amount":500}'
```

Send a PDF through the extraction pipeline:

```powershell
curl.exe -X POST http://localhost:8080/webhook/test-source `
  -H "Content-Type: application/json" `
  -d '{"file_url":"https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf"}'
```

## What's in the box

Six Go services, a SolidJS dashboard, a C++ extractor, and five Docker containers for infra:

- `cmd/ingestion` (:8080) receives webhooks, writes raw events to ScyllaDB, enqueues in Redis
- `cmd/orchestrator` routes events to either the delivery or extraction queue
- `cmd/extractor-bridge` pulls referenced files, uploads to MinIO, calls the C++ extractor over gRPC
- `cmd/delivery` forwards to configured destinations with exponential-backoff retries; failures land in a DLQ
- `cmd/api` (:8082) is the REST surface for DLQ, rules, replay, metrics, schema
- `cmd/sse` (:8081) streams live events to the browser via Server-Sent Events
- `dashboard/` is the SolidJS front-end (:3000) — real-time stream, schema drift, DLQ, replay

Storage is split between Scylla (high-throughput raw event firehose, 30-day TTL) and Postgres (source configs, rules, inferred schemas, delivery history, replay sessions). MinIO holds extracted file payloads. A llama.cpp container does the LLM-based schema labeling.

## Why Scylla *and* Postgres

They do different jobs. Scylla absorbs the raw webhook firehose where write volume dwarfs read volume. Postgres handles the long-lived relational stuff — source configs, routing rules, inferred schema versions, replay sessions, delivery attempt history — where joins and transactions matter. Trying to do either job with the other DB is painful.

## Troubleshooting

**`goreman: command not found`** — `%GOPATH%\bin` isn't on PATH. Run `go env GOPATH` and add `<that>\bin` to your user PATH.

**Connection refused** on any of Redis / Postgres / Scylla / MinIO — the corresponding Docker container isn't running. Re-run the `docker start` line.

**Dashboard stuck on "Disconnected"** — `sse` didn't come up, or the browser loaded before it did. Check the goreman log for SSE errors, then refresh the page.

**Services die on startup with a ScyllaDB `EOF` error** — Scylla takes 30–45s to cold-boot. The Go services retry for about a minute before giving up. If the error persists past that, the container itself is unhealthy.

**Extractor logs show `invalid character '`'`** — the LLM wrapped its JSON output in a markdown code fence. Extraction still succeeds via cached template; the raw field is where you'd clean up the fence.
