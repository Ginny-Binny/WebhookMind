<p align="center">
  <img src="assets/logo.svg" alt="webhookmind" width="340" />
</p>

<p align="center">
  <a href="https://github.com/Ginny-Binny/WebhookMind/actions/workflows/ci.yml"><img src="https://github.com/Ginny-Binny/WebhookMind/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
</p>

A webhook processor for messy payloads. JSON, PDFs, images, audio: drop any of it on the ingestion endpoint and it extracts the content, infers a schema as events accumulate, flags drift, runs routing rules, and delivers downstream with retries. Comes with a live dashboard.

## Run it

```bash
echo "ANTHROPIC_API_KEY=sk-ant-..." > .env
docker compose up -d
```

That's the setup. First run takes a few minutes to build images; subsequent runs are seconds.

- Dashboard: <http://localhost:3000>
- Webhook in: `POST http://localhost:8080/webhook/{source_id}`
- API: <http://localhost:8082>

```bash
curl -X POST http://localhost:8080/webhook/test-source \
  -H "Content-Type: application/json" \
  -d '{"order_id":"TEST-1","amount":500}'
```

To use a local llama.cpp model instead of Claude, drop `llama-3.2-3b-instruct.Q4_K_M.gguf` into `./models/`, set `EXTRACTOR_BACKEND=local` in `.env`, then `docker compose --profile local-llm up -d`. Tear everything down with `docker compose down -v`.

### Dev loop

When you're iterating on Go code and want goreman's auto-recompile instead of rebuilding images:

```bash
go install github.com/mattn/goreman@latest
cp .env.example .env
( cd dashboard && npm install )
docker start webhookmind-redis webhookmind-postgres webhookmind-scylla webhookmind-minio webhookmind-extractor
goreman start
```

## Architecture

| Service | Port | Job |
|---|---|---|
| `cmd/ingestion` | 8080 | accepts webhooks, queues them in Redis, archives raw bodies in Scylla |
| `cmd/orchestrator` | | routes events between the delivery and extraction queues |
| `cmd/extractor-bridge` | | downloads referenced files, hands them to the extractor backend |
| `cmd/delivery` | | POSTs to destinations with backoff retries; failures go to a DLQ |
| `cmd/api` | 8082 | REST surface for the dashboard |
| `cmd/sse` | 8081 | Server-Sent Events feed |
| `dashboard/` | 3000 | SolidJS frontend |

Scylla holds the raw event firehose (30-day TTL, write-heavy). Postgres holds everything relational: sources, rules, schemas, delivery history, replay sessions. MinIO stores extracted file blobs. The C++ extractor wraps llama.cpp + Tesseract OCR + Whisper for fully local extraction; cloud mode swaps it for Anthropic.

## Bring Your Own Key

The cloud extractor accepts a per-request `X-Anthropic-Key` header that overrides the server's `ANTHROPIC_API_KEY`. Deploy with the env var unset and you have a free public demo: random visitors can't burn your credit, but anyone with their own key can drive a full extraction.

```bash
curl -X POST https://your-host/webhook/test-source \
  -H "X-Anthropic-Key: sk-ant-yourkey" \
  -H "Content-Type: application/json" \
  -d '{"file_url":"https://example.com/invoice.pdf"}'
```

The key gets stripped from stored headers, lives only on the in-flight Redis payload, and is dropped after the API call. Nothing persisted.

## Webhook signing

Unsigned webhooks are accepted by default. To require signatures on a source:

```sql
UPDATE sources SET signing_secret = 'random-string' WHERE id = 'stripe-prod';
```

Now requests need an `X-Signature: t=<unix>,v1=<hex>` header where `v1 = HMAC-SHA256(secret, "<t>.<body>")`. Stripe-style. Stale timestamps (>5 min) are rejected, comparison is constant-time.

```bash
secret="random-string"
body='{"order_id":"OK-1"}'
ts=$(date +%s)
sig=$(printf '%s.%s' "$ts" "$body" | openssl dgst -sha256 -hmac "$secret" -hex | awk '{print $2}')
curl -X POST http://localhost:8080/webhook/stripe-prod \
  -H "X-Signature: t=$ts,v1=$sig" \
  -H "Content-Type: application/json" \
  -d "$body"
```

Set `INGESTION_REQUIRE_SIGNATURE=true` to reject every unsigned webhook globally, even on sources without a secret configured.

## Troubleshooting

- **`goreman: command not found`** — add `$(go env GOPATH)/bin` to PATH.
- **Connection refused on Redis / Postgres / Scylla / MinIO** — that container isn't running.
- **Dashboard stuck on "Disconnected"** — SSE didn't start in time, or the page loaded too early. Refresh.
- **`gocql: EOF` on startup** — Scylla cold-boot takes 30–45s. Services retry for ~67s before giving up.
- **Cloud extraction returns 401** — wrong API key. Set `EXTRACTOR_FALLBACK=local` if you want extractions to keep working while you fix it.
