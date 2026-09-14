-- Restore the previous timestamp representation at midnight UTC.
ALTER TABLE harvests
    ALTER COLUMN harvested_at TYPE TIMESTAMPTZ
    USING harvested_at::timestamp AT TIME ZONE 'UTC';
