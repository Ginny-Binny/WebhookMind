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
