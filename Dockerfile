# Shared multi-stage Dockerfile for the six Go services.
# Build a specific service with: docker build --build-arg SERVICE=<name> .
# Used by docker-compose, which sets the build arg per service.

FROM golang:1.25-alpine AS builder

WORKDIR /src

# Cache module downloads as their own layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# SERVICE picks which cmd/<name> to build (api, delivery, extractor-bridge,
# ingestion, orchestrator, sse).
ARG SERVICE
RUN test -n "$SERVICE" || (echo "ERROR: --build-arg SERVICE=<name> is required" && false)
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./cmd/${SERVICE}


FROM alpine:3.19

RUN apk add --no-cache ca-certificates

COPY --from=builder /out/app /usr/local/bin/app

ENTRYPOINT ["/usr/local/bin/app"]
