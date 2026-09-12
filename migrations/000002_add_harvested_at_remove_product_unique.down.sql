DROP INDEX idx_harvests_hive_harvested_at;

ALTER TABLE harvests DROP COLUMN harvested_at;

ALTER TABLE harvests ADD CONSTRAINT harvests_hive_id_product_key UNIQUE (hive_id, product);
