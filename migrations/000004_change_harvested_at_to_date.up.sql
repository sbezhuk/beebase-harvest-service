-- Harvests are recorded for a calendar day, not a time instant. Preserve
-- the existing UTC calendar date while removing the time-of-day component.
ALTER TABLE harvests
    ALTER COLUMN harvested_at TYPE DATE
    USING (harvested_at AT TIME ZONE 'UTC')::date;
