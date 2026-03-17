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

-- Webhook diffs: field-by-field comparison of consecutive webhooks
CREATE TABLE webhook_diffs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id        TEXT NOT NULL,
    source_id       TEXT NOT NULL,
    prev_event_id   TEXT,            -- NULL if first webhook from this source
    diff_data       JSONB NOT NULL,
    computed_at     TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_webhook_diffs_source_id ON webhook_diffs(source_id, computed_at DESC);

-- Routing rules: condition-based routing
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

-- Replay sessions: time machine replay state
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

-- Add is_default column to destinations
ALTER TABLE destinations ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT TRUE;
