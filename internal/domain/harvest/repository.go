package harvest

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/sbezhuk/beebase-common/pagination"
)

// Repository is the port through which the application persists and
// retrieves harvest records. Unlike every other repository port in
// BeeBase, methods here are scoped only by hiveID, never by a userID:
// harvest-service never stores hive ownership data of its own (see the
// package doc comment) - the application layer confirms hive ownership
// against hive-service, on every call, before a Repository method is
// ever invoked.
type Repository interface {
	// Create persists h.
	Create(ctx context.Context, h *Harvest) error
	// GetByID returns the harvest identified by harvestID under hiveID.
	// ErrNotFound covers both an unknown id and a harvestID that belongs
	// to a different hive.
	GetByID(ctx context.Context, hiveID, harvestID uuid.UUID) (*Harvest, error)
	// ListByHive is retained as the narrow repository port used by existing
	// hive-scoped callers. Implementations should route it through List.
	ListByHive(ctx context.Context, hiveID uuid.UUID, p pagination.Params, product *Product, amountOperator *AmountOperator, amount *float64, dateFrom, dateTo *time.Time, sortOrder *string) (harvests []*Harvest, total int, err error)
	// Update persists h.Product, h.Amount, h.Unit, h.HarvestedAt, and
	// h.UpdatedAt for the harvest identified by h.ID under h.HiveID.
	// Returns ErrNotFound under the same conditions as GetByID.
	Update(ctx context.Context, h *Harvest) error
	// Delete removes the harvest identified by harvestID under hiveID.
	// Returns ErrNotFound under the same conditions as GetByID.
	Delete(ctx context.Context, hiveID, harvestID uuid.UUID) error
}

// ScopedRepository is the optional extension used by the global list. The
// legacy Repository port remains hive-scoped for compatibility with callers
// and test doubles; the production PostgreSQL repository implements both.
type ScopedRepository interface {
	List(ctx context.Context, hiveIDs []uuid.UUID, p pagination.Params, product *Product, amountOperator *AmountOperator, amount *float64, dateFrom, dateTo *time.Time, sortOrder *string) (harvests []*Harvest, total int, err error)
}
