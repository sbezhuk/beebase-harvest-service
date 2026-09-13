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
	// ListByHive returns the page of harvest records described by p for
	// hiveID, ordered by harvested_at DESC with id DESC as a stable
	// secondary sort, along with the total number of matching records
	// (independent of p, for computing pagination metadata). product,
	// amountOperator/amount, and dateFrom/dateTo are optional filters,
	// combined with AND when several are given: product restricts to an
	// exact match; amountOperator/amount restrict amount by the given
	// comparison (both must be given together, or neither); dateFrom/dateTo
	// restrict harvested_at, independently of one another - dateFrom is an
	// inclusive lower bound, dateTo is an exclusive upper bound that the
	// caller has already advanced to the start of the day after the
	// requested end date, so together they cover the requested date_to's
	// whole calendar day. When sortOrder is non-nil ("asc" or "desc") the
	// page is ordered by creation date in that direction instead of the
	// default order; a nil sortOrder keeps the default order.
	ListByHive(ctx context.Context, hiveID uuid.UUID, p pagination.Params, product *Product, amountOperator *AmountOperator, amount *float64, dateFrom, dateTo *time.Time, sortOrder *string) (harvests []*Harvest, total int, err error)
	// Update persists h.Product, h.Amount, h.Unit, h.HarvestedAt, and
	// h.UpdatedAt for the harvest identified by h.ID under h.HiveID.
	// Returns ErrNotFound under the same conditions as GetByID.
	Update(ctx context.Context, h *Harvest) error
	// Delete removes the harvest identified by harvestID under hiveID.
	// Returns ErrNotFound under the same conditions as GetByID.
	Delete(ctx context.Context, hiveID, harvestID uuid.UUID) error
}
