# WebhookMind — Startup Guide

## Prerequisites

Make sure all Docker containers are running:

```powershell
docker start webhookmind-redis webhookmind-scylla webhookmind-postgres webhookmind-minio webhookmind-extractor
```

If the extractor was removed, recreate it (reduced context for <8GB RAM machines):

```powershell
docker run -d --name webhookmind-extractor -p 50051:50051 -e REDIS_HOST=host.docker.internal -e LLAMA_CONTEXT_SIZE=2048 -e LLAMA_MAX_TOKENS=256 -v C:\opt\models:/opt/models:ro webhookmind-extractor
```

Verify:

```powershell
docker ps
```

You should see 5 containers: redis, scylla, postgres, minio, extractor.

---

## Environment Variables

Paste this block in **every** PowerShell terminal before running the Go service:

```powershell
cd C:\Users\gaura\OneDrive\Desktop\WebhookMind
$env:REDIS_ADDR="127.0.0.1:6379"
$env:POSTGRES_DSN="postgres://webhookmind:password@127.0.0.1:5432/webhookmind?sslmode=disable"
$env:SCYLLA_HOSTS="127.0.0.1"
$env:MINIO_ENDPOINT="127.0.0.1:9000"
$env:MINIO_ACCESS_KEY="webhookmind"
$env:MINIO_SECRET_KEY="webhookmind123"
$env:MINIO_INTERNAL_ENDPOINT="host.docker.internal:9000"
$env:EXTRACTOR_GRPC_ADDR="127.0.0.1:50051"
$env:EXTRACTOR_GRPC_TIMEOUT_SECONDS="300"
```

---

## Terminal 1 — Ingestion (port 8080)

```powershell
go run ./cmd/ingestion
```

Receives incoming webhooks at `POST http://localhost:8080/webhook/{source_id}`

## Terminal 2 — Orchestrator

```powershell
go run ./cmd/orchestrator
```

Routes events to delivery or extraction queues.

## Terminal 3 — Extractor Bridge

```powershell
go run ./cmd/extractor-bridge
```

Downloads files, uploads to MinIO, calls C++ extraction engine via gRPC.

## Terminal 4 — Delivery (port 8080 destinations)

```powershell
go run ./cmd/delivery
```

Delivers webhooks to destinations with retries and exponential backoff.

## Terminal 5 — API Server (port 8082)

```powershell
go run ./cmd/api
```

REST API for dashboard: sources, webhooks, schema, drift, DLQ, rules, replay, metrics.

## Terminal 6 — SSE Server (port 8081)

```powershell
go run ./cmd/sse
```

Server-Sent Events for real-time dashboard updates.

---

## Terminal 7 — Dashboard (port 3000)

```powershell
cd dashboard
npm run dev
```

Open **http://localhost:3000** in your browser.

---

## Quick Test

Send a test webhook:

```powershell
curl.exe -X POST http://localhost:8080/webhook/test-source -H "Content-Type: application/json" -d "{\"order_id\": \"TEST-1\", \"amount\": 500}"
```

Send a PDF webhook:

```powershell
curl.exe -X POST http://localhost:8080/webhook/test-source -H "Content-Type: application/json" -d "{\"file_url\": \"https://www.w3.org/WAI/ER/tests/xhtml/testfiles/resources/pdf/dummy.pdf\"}"
```

---

## Ports Summary

| Service    | Port  |
|------------|-------|
| Ingestion  | 8080  |
| SSE        | 8081  |
| API        | 8082  |
| Dashboard  | 3000  |
| Redis      | 6379  |
| ScyllaDB   | 9042  |
| PostgreSQL | 5432  |
| MinIO      | 9000  |
| Extractor  | 50051 |
