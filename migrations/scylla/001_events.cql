CREATE KEYSPACE IF NOT EXISTS webhookmind
    WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};

USE webhookmind;

CREATE TABLE IF NOT EXISTS webhook_events (
    source_id   TEXT,
    received_at TIMESTAMP,
    event_id    UUID,
    raw_body    BLOB,
    headers     MAP<TEXT, TEXT>,
    PRIMARY KEY ((source_id), received_at, event_id)
) WITH CLUSTERING ORDER BY (received_at DESC, event_id ASC)
  AND default_time_to_live = 2592000;  -- 30 days TTL
