<p align="center">
  <img src="assets/logo.svg" alt="webhookmind" width="340" />
</p>

<p align="center">
  <a href="https://github.com/Ginny-Binny/WebhookMind/actions/workflows/ci.yml"><img src="https://github.com/Ginny-Binny/WebhookMind/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
</p>

An unstructured-webhook processor. Point messy payloads at it — JSON, attached PDFs, images, audio — and it extracts the content, infers a schema on the fly, flags drift when payloads change shape, evaluates routing rules, and streams the whole thing to a live dashboard.

## Quickest start: `docker compose up`

If you just want to run the whole thing, you need Docker and an Anthropic API key (the default extractor backend uses Claude). Two commands:

```bash
echo "ANTHROPIC_API_KEY=sk-ant-..." > .env
docker compose up -d
```

That builds and starts: redis, postgres, scylla, minio + a one-shot migrator that applies all schema, the six Go services, and the dashboard behind nginx.

Wait ~60 seconds for Scylla to cold-boot (the Go services retry until it's ready). Then:

- Dashboard: <http://localhost:3000>
- Webhook ingestion: <http://localhost:8080/webhook/{source_id}>
- REST API: <http://localhost:8082>
- MinIO console (debug bucket browser): <http://localhost:9001> — login `webhookmind` / `webhookmind123`

Send a test webhook:

```bash
curl -X POST http://localhost:8080/webhook/test-source \
  -H "Content-Type: application/json" \
  -d '{"order_id":"TEST-1","amount":500}'
```

**Want the local llama.cpp extractor instead?** Drop a `llama-3.2-3b-instruct.Q4_K_M.gguf` into `./models/`, then:

```bash
echo "EXTRACTOR_BACKEND=local" >> .env
docker compose --profile local-llm up -d
```

Tear down with `docker compose down` (keeps volumes) or `docker compose down -v` (nukes them).

---

## Running it for development (faster iteration)

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

Storage is split between Scylla (high-throughput raw event firehose, 30-day TTL) and Postgres (source configs, rules, inferred schemas, delivery history, replay sessions). MinIO holds extracted file payloads. A llama.cpp container does the LLM-based schema labeling — or you can swap it for a cloud LLM (see below).

## Cloud extraction (optional)

By default, extraction runs through the local llama.cpp container — fully offline, no API keys, but you do have to download a 2 GB model. If you'd rather skip that and use Claude instead, flip three lines in `.env`:

```
EXTRACTOR_BACKEND=cloud
ANTHROPIC_API_KEY=sk-ant-...
ANTHROPIC_MODEL=claude-haiku-4-5
```

Restart goreman. The `webhookmind-extractor` container is no longer needed — PDFs and images go straight to Anthropic's `/v1/messages` API. Audio is the one exception: Claude doesn't do audio, so the cloud backend returns a clear "unsupported" failure for audio file types.

Want both? Cloud as primary with local as a safety net for outages, rate limits, or auth problems:

```
EXTRACTOR_FALLBACK=local
```

Cloud calls retry up to 3 times with exponential backoff on transient errors (network, 429 with Retry-After, 5xx). Non-retryable failures (400, 401, 403) fail fast so you see the real issue. If retries exhaust and a fallback is configured, it transparently kicks in for that one event — the pipeline doesn't stall.

## Webhook signing (HMAC)

Unsigned webhooks are accepted by default — fine for internal sources you trust. For anything reachable on the public internet, give each source a signing secret:

```sql
-- inside the postgres container or via psql
UPDATE sources SET signing_secret = 'a-long-random-string' WHERE id = 'stripe-prod';
```

From that moment on, any request to `/webhook/stripe-prod` must include an `X-Signature` header in Stripe-style format:

```
X-Signature: t=<unix_seconds>,v1=<hex_sha256>
```

Where the signed string is `<t>.<body>` and the algorithm is HMAC-SHA256 with the source's secret. The server rejects with 401 if the header is missing, the timestamp is more than 5 minutes off, or the signature doesn't match (constant-time compared).

Compute one with PowerShell:

```powershell
$secret = "a-long-random-string"
$body   = '{"order_id":"OK-1"}'
$ts     = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
$payload = "$ts.$body"
$hmac = [System.Security.Cryptography.HMACSHA256]::new([Text.Encoding]::UTF8.GetBytes($secret))
$sig  = -join ($hmac.ComputeHash([Text.Encoding]::UTF8.GetBytes($payload)) | ForEach-Object { $_.ToString('x2') })
curl.exe -X POST http://localhost:8080/webhook/stripe-prod `
  -H "X-Signature: t=$ts,v1=$sig" `
  -H "Content-Type: application/json" `
  -d $body
```

Or in bash:

```bash
secret="a-long-random-string"
body='{"order_id":"OK-1"}'
ts=$(date +%s)
sig=$(printf '%s.%s' "$ts" "$body" | openssl dgst -sha256 -hmac "$secret" -hex | awk '{print $2}')
curl -X POST http://localhost:8080/webhook/stripe-prod \
  -H "X-Signature: t=$ts,v1=$sig" \
  -H "Content-Type: application/json" \
  -d "$body"
```

To require *every* webhook to be signed (reject sources that haven't been configured with a secret), set `INGESTION_REQUIRE_SIGNATURE=true` in `.env`.

## Why Scylla *and* Postgres

They do different jobs. Scylla absorbs the raw webhook firehose where write volume dwarfs read volume. Postgres handles the long-lived relational stuff — source configs, routing rules, inferred schema versions, replay sessions, delivery attempt history — where joins and transactions matter. Trying to do either job with the other DB is painful.

## Troubleshooting

**`goreman: command not found`** — `%GOPATH%\bin` isn't on PATH. Run `go env GOPATH` and add `<that>\bin` to your user PATH.

**Connection refused** on any of Redis / Postgres / Scylla / MinIO — the corresponding Docker container isn't running. Re-run the `docker start` line.

**Dashboard stuck on "Disconnected"** — `sse` didn't come up, or the browser loaded before it did. Check the goreman log for SSE errors, then refresh the page.

**Services die on startup with a ScyllaDB `EOF` error** — Scylla takes 30–45s to cold-boot. The Go services retry for about a minute before giving up. If the error persists past that, the container itself is unhealthy.

**Cloud extraction returns 401** — bad `ANTHROPIC_API_KEY`. Fail-fast on auth errors is intentional; retries can't fix a wrong key. If you've configured `EXTRACTOR_FALLBACK=local`, extractions will silently route through llama.cpp until you fix the key.

**`grpc` errors right after startup with `EOF` / `server preface`** — extractor-bridge connected to the C++ extractor before its gRPC server was fully ready. Wait for the extractor container to log `extraction engine listening` and `LLM labeler active` before sending webhooks; subsequent calls reconnect cleanly.
