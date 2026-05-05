<p align="center">
  <img src="assets/logo.svg" alt="webhookmind" width="340" />
</p>

<p align="center">
  <a href="https://github.com/Ginny-Binny/WebhookMind/actions/workflows/ci.yml"><img src="https://github.com/Ginny-Binny/WebhookMind/actions/workflows/ci.yml/badge.svg" alt="CI" /></a>
</p>

**Live demo:** [webhookmind.psyduck.in](https://webhookmind.psyduck.in). The Try It tab in the dashboard fires a webhook into your own private sandbox source so visitors don't see each other's events. Bring your own Anthropic key if you want file extraction to actually run; everything else works without one.

I built this because I kept dealing with webhook integrations where the payload could be JSON, a PDF link, an image, and sometimes audio, and the systems consuming them had no way to make sense of any of it. WebhookMind takes whatever you POST at the ingestion endpoint, infers a schema as events accumulate, flags drift when a sender starts shipping new fields, and forwards downstream with retries. The dashboard shows it as it happens.

## Run it

```bash
echo "ANTHROPIC_API_KEY=sk-ant-..." > .env
docker compose up -d
```

First boot is a couple of minutes (image builds). After that:

- Dashboard at http://localhost:3000
- Webhooks at `POST http://localhost:8080/webhook/{source_id}`
- API at http://localhost:8082

Fire one to check it works:

```bash
curl -X POST http://localhost:8080/webhook/test-source \
  -H "Content-Type: application/json" \
  -d '{"order_id":"TEST-1","amount":500}'
```

Refresh the dashboard and the event lands on the Stream tab.

To use the local llama.cpp container instead of Claude, drop a gguf model into `./models/`, set `EXTRACTOR_BACKEND=local` in `.env`, and `docker compose --profile local-llm up -d`. `docker compose down -v` wipes everything.

### Dev loop

When you're iterating on Go and want hot-recompile instead of rebuilding images each time:

```bash
go install github.com/mattn/goreman@latest
cp .env.example .env
( cd dashboard && npm install )
docker start webhookmind-redis webhookmind-postgres webhookmind-scylla webhookmind-minio webhookmind-extractor
goreman start
```

## What's running

Six Go services, a SolidJS dashboard, a C++ extractor wrapping llama.cpp + Tesseract OCR + Whisper, plus Redis, Postgres, ScyllaDB, MinIO.

| Service | Port | Job |
|---|---|---|
| `cmd/ingestion` | 8080 | accepts webhooks, queues, archives raw bodies |
| `cmd/orchestrator` | | routes events to the delivery or extraction queue |
| `cmd/extractor-bridge` | | downloads referenced files, hands them to the extractor backend |
| `cmd/delivery` | | POSTs to destinations, retries on failure, DLQ |
| `cmd/api` | 8082 | REST surface for the dashboard |
| `cmd/sse` | 8081 | live event feed for the dashboard |
| `dashboard/` | 3000 | SolidJS UI |

Scylla absorbs the raw event firehose with a 30-day TTL. Postgres holds everything relational: sources, rules, inferred schemas, delivery history, replay sessions. MinIO holds the extracted file blobs.

## Bring your own key

The cloud extractor takes a per-request `X-Anthropic-Key` header that overrides the server's own `ANTHROPIC_API_KEY`. Deploy with that env var blank and visitors can't bill you, but anyone who brings their own key gets a working extraction. The hosted demo runs in this mode.

```bash
curl -X POST https://webhookmind.psyduck.in/webhook/test-source \
  -H "X-Anthropic-Key: sk-ant-yourkey" \
  -H "Content-Type: application/json" \
  -d '{"file_url":"https://example.com/invoice.pdf"}'
```

The key gets stripped from `event.Headers` before the row hits Scylla and only lives on the in-flight Redis payload until the extractor fires. Nothing persists.

## Webhook signing

Unsigned webhooks are accepted by default. To require signatures on a source, set a secret:

```sql
UPDATE sources SET signing_secret = 'random-string' WHERE id = 'stripe-prod';
```

After that, requests need `X-Signature: t=<unix>,v1=<hex>` where `v1 = HMAC-SHA256(secret, "<t>.<body>")`. Five-minute timestamp window, constant-time compare.

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

Setting `INGESTION_REQUIRE_SIGNATURE=true` rejects every webhook globally unless its source has a secret and the request is signed.

## Rate limits

10 requests per minute per client IP, 300 per minute per source. Backed by Redis using the GCRA algorithm. Over the limit gets `429 Too Many Requests` with `Retry-After` and `X-RateLimit-Remaining` headers. Tune with `INGESTION_RATE_LIMIT_PER_IP` and `INGESTION_RATE_LIMIT_PER_SOURCE`; set either to 0 to disable.

Behind a reverse proxy the limiter keys off `X-Forwarded-For`, so users behind the same proxy each get their own bucket.

## Deploy

`.github/workflows/ci.yml` runs tests, builds the dashboard, and on push to `main` SSHes into the configured VPS to `git reset --hard origin/main && docker compose up -d`. The deploy job skips itself if the `VPS_HOST` secret isn't set, so contributors and PRs from forks see green CI.

To wire it up to your own server, add three repo secrets: `VPS_HOST`, `VPS_USER`, and `VPS_SSH_KEY` (an ed25519 private key whose public half is in the VPS's `authorized_keys`).

## Troubleshooting

`goreman: command not found` means `$(go env GOPATH)/bin` isn't on PATH.

Connection refused on Redis / Postgres / Scylla / MinIO means that container isn't running.

Dashboard stuck on "Disconnected" usually means SSE didn't come up before the page loaded. Refresh.

`gocql: EOF` on startup is Scylla still cold-booting. Services retry for about a minute.

Cloud extraction returning 401 means a bad API key. If you've set `EXTRACTOR_FALLBACK=local`, extractions silently fall back to llama.cpp while you fix it.
