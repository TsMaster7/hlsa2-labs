-- Postgres bootstrap for lab 3-2.
--
-- events_audit is the consumer's side-effect store. event_id is the
-- dedupe key the topic guide refers to in step 6 ("idempotent mode is
-- safe under replay because it upserts on event_id"). Naive mode does
-- a plain INSERT and lets duplicates land - intentionally - so
-- assert-idempotent can show the state hash changing on the second
-- replay.

CREATE TABLE IF NOT EXISTS events_audit (
  event_id     TEXT PRIMARY KEY,
  order_id     TEXT NOT NULL,
  amount       BIGINT NOT NULL,
  emitted_at   BIGINT NOT NULL,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  source       TEXT,
  duplicate_of TEXT
);

CREATE INDEX IF NOT EXISTS events_audit_order_id_idx ON events_audit (order_id);
CREATE INDEX IF NOT EXISTS events_audit_processed_at_idx ON events_audit (processed_at);

-- naive mode writes to events_audit_naive which has no PK so duplicate
-- inserts are accepted (one row per event copy). assert-idempotent
-- compares hashes of this table separately.
CREATE TABLE IF NOT EXISTS events_audit_naive (
  pk           BIGSERIAL PRIMARY KEY,
  event_id     TEXT NOT NULL,
  order_id     TEXT NOT NULL,
  amount       BIGINT NOT NULL,
  emitted_at   BIGINT NOT NULL,
  processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  source       TEXT
);

CREATE INDEX IF NOT EXISTS events_audit_naive_event_id_idx ON events_audit_naive (event_id);

-- Helper: a row-count + checksum view that the assertion script can
-- snapshot. Hashing array_agg of (event_id, amount) ordered by
-- event_id makes the hash stable across replays.
CREATE OR REPLACE FUNCTION events_audit_hash() RETURNS TEXT AS $$
  SELECT md5(string_agg(event_id || ':' || amount, ',' ORDER BY event_id))
  FROM events_audit;
$$ LANGUAGE SQL STABLE;

CREATE OR REPLACE FUNCTION events_audit_naive_hash() RETURNS TEXT AS $$
  SELECT md5(string_agg(event_id || ':' || amount, ',' ORDER BY event_id, pk))
  FROM events_audit_naive;
$$ LANGUAGE SQL STABLE;
