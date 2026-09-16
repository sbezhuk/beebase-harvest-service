CREATE TABLE harvest_backfill_failures (
    harvest_id UUID PRIMARY KEY,
    hive_id UUID NOT NULL,
    reason TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 1,
    last_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);
