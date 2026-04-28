-- Per-source HMAC signing secret used to verify webhook ingestion signatures.
-- NULL or empty means "this source isn't signing" (backwards-compatible default).
ALTER TABLE sources ADD COLUMN IF NOT EXISTS signing_secret TEXT;
