package harvest

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the port through which the application persists and
// retrieves harvest records. Unlike every other repository port in
// BeeBase, methods here are scoped only by hiveID, never by a userID:
// harvest-service never stores hive ownership data of its own (see the
// package doc comment) - the application layer confirms hive ownership
// against hive-service, on every call, before a Repository method is
// ever invoked.
type Repository interface {
	// Create persists h. Returns ErrDuplicateProduct if the hive already
	// has a harvest record for h.Product.
	Create(ctx context.Context, h *Harvest) error
	// GetByID returns the harvest identified by harvestID under hiveID.
	// ErrNotFound covers both an unknown id and a harvestID that belongs
	// to a different hive.
	GetByID(ctx context.Context, hiveID, harvestID uuid.UUID) (*Harvest, error)
	// ListByHive returns every harvest record for hiveID, ordered by
	// created_at then id. Empty (never nil) when there are none.
	ListByHive(ctx context.Context, hiveID uuid.UUID) ([]*Harvest, error)
	// Update persists h.Product, h.Amount, h.Unit, and h.UpdatedAt for the
	// harvest identified by h.ID under h.HiveID. Returns ErrNotFound under
	// the same conditions as GetByID, and ErrDuplicateProduct if changing
	// h.Product would collide with another harvest already recorded for
	// the same hive.
	Update(ctx context.Context, h *Harvest) error
	// Delete removes the harvest identified by harvestID under hiveID.
	// Returns ErrNotFound under the same conditions as GetByID.
	Delete(ctx context.Context, hiveID, harvestID uuid.UUID) error
}
