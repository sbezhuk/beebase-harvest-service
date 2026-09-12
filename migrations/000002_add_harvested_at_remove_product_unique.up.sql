-- A hive may now have any number of harvest records for the same
-- product, one per harvest event, distinguished by harvested_at - drop
-- the constraint that used to cap it at one record per product.
ALTER TABLE harvests DROP CONSTRAINT harvests_hive_id_product_key;

-- harvested_at is the date the product was actually collected, distinct
-- from created_at/updated_at (technical record-lifecycle timestamps).
-- Added nullable first so existing rows can be backfilled, then made
-- required; backfilling from created_at is only a placeholder for
-- pre-existing rows; new rows always supply their own harvested_at.
ALTER TABLE harvests ADD COLUMN harvested_at TIMESTAMPTZ;
UPDATE harvests SET harvested_at = created_at WHERE harvested_at IS NULL;
ALTER TABLE harvests ALTER COLUMN harvested_at SET NOT NULL;

-- ListByHive now orders by (harvested_at DESC, id DESC) and paginates
-- with LIMIT/OFFSET; index the columns in that same order so the sort
-- and the WHERE hive_id = $1 filter are both served directly.
CREATE INDEX idx_harvests_hive_harvested_at ON harvests (hive_id, harvested_at DESC, id DESC);
