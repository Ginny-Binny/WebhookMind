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
