package harvest

import (
	"time"

	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
)

// CreateInput is the input to Service.Create.
type CreateInput struct {
	Product     harvest.Product
	Amount      float64
	Unit        harvest.Unit
	HarvestedAt time.Time
}

// UpdateInput is the input to Service.Update. Update replaces every
// editable field (PUT semantics), not a partial patch.
type UpdateInput struct {
	Product     harvest.Product
	Amount      float64
	Unit        harvest.Unit
	HarvestedAt time.Time
}
