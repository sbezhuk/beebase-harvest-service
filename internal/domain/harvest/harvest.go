// Package harvest holds the Harvest entity and the port through which the
// rest of the application persists and retrieves it. It has no dependency
// on HTTP, PostgreSQL, or any other infrastructure concern.
//
// Harvest belongs to exactly one hive (User -> Apiary -> Hive -> Harvest),
// but - unlike every other nested resource in BeeBase - it does not
// denormalize its owner: hive-service is asked to confirm hive ownership
// on every operation, not just once at creation time. See
// application/harvest.HiveVerifier's doc comment for why.
package harvest

import (
	"time"

	"github.com/google/uuid"
)

// Harvest is one harvest event of one product (honey, pollen, propolis,
// or wax) collected from a hive. A hive may have zero, one, or several
// harvest records for the same product - each represents a separate
// harvest event, distinguished by HarvestedAt, and there is no
// uniqueness constraint between a hive and a product.
type Harvest struct {
	ID     uuid.UUID
	HiveID uuid.UUID // immutable after creation; opaque, owned by hive-service's own database
	// UserID is denormalized for durable account deletion. It is nullable
	// during the legacy backfill window because old rows predate this field.
	UserID *uuid.UUID

	Product Product
	Amount  float64
	Unit    Unit
	// HarvestedAt is when the product was actually collected, as reported
	// by the caller - distinct from CreatedAt/UpdatedAt, which are
	// bookkeeping timestamps for the record's own lifecycle.
	HarvestedAt time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// New constructs a Harvest for hiveID with a freshly generated ID and
// CreatedAt/UpdatedAt set to now. Callers must have already verified that
// hiveID belongs to the caller, and that product, amount, and unit are
// all valid (including the product/unit combination), before calling New.
func New(hiveID uuid.UUID, product Product, amount float64, unit Unit, harvestedAt time.Time) *Harvest {
	now := time.Now().UTC()
	return &Harvest{
		ID:          uuid.New(),
		HiveID:      hiveID,
		Product:     product,
		Amount:      amount,
		Unit:        unit,
		HarvestedAt: harvestedAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}
