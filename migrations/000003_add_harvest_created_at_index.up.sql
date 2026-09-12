-- Sorting by creation date (?sortOrder=asc|desc) is now a first-class
-- option alongside the default HarvestedAt DESC order, which
-- idx_harvests_hive_harvested_at already serves. Add the equivalent
-- composite index on created_at so that request path performs just as
-- well as the default one.
CREATE INDEX idx_harvests_hive_created_at ON harvests (hive_id, created_at, id);
