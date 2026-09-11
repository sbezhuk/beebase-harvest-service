-- Harvest belongs to a hive (owned by hive-service, a different service
-- with its own database) - hive_id is opaque here, deliberately with no
-- foreign key, and ownership is never denormalized onto this row: every
-- operation confirms hive ownership against hive-service itself, on
-- every call, not just once at creation time (see
-- application/harvest.HiveVerifier).
CREATE TABLE harvests (
    id         UUID PRIMARY KEY,
    hive_id    UUID NOT NULL,
    product    TEXT NOT NULL
        CHECK (product IN ('HONEY', 'POLLEN', 'PROPOLIS', 'WAX')),
    amount     DOUBLE PRECISION NOT NULL CHECK (amount >= 0),
    unit       TEXT NOT NULL CHECK (unit IN ('g', 'kg', 'l')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- At most one harvest record per product per hive; also serves every
    -- hive_id-only lookup (ListByHive) as its leftmost column, so a
    -- separate single-column index would be redundant.
    UNIQUE (hive_id, product)
);
