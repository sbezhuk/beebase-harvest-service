package harvest

import (
	"time"

	"github.com/google/uuid"

	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
)

// Response is the public representation of a harvest record.
type Response struct {
	ID          uuid.UUID       `json:"id"`
	HiveID      uuid.UUID       `json:"hiveId"`
	Product     harvest.Product `json:"product"`
	Amount      float64         `json:"amount"`
	Unit        harvest.Unit    `json:"unit"`
	HarvestedAt time.Time       `json:"harvestedAt"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// newResponse builds a Response for h.
func newResponse(h *harvest.Harvest) Response {
	return Response{
		ID:          h.ID,
		HiveID:      h.HiveID,
		Product:     h.Product,
		Amount:      h.Amount,
		Unit:        h.Unit,
		HarvestedAt: h.HarvestedAt,
		CreatedAt:   h.CreatedAt,
		UpdatedAt:   h.UpdatedAt,
	}
}

// newListResponse builds the response for one page of GET results - never
// nil, so a page with no harvest records renders as "[]" rather than
// "null".
func newListResponse(harvests []*harvest.Harvest) []Response {
	out := make([]Response, len(harvests))
	for i, h := range harvests {
		out[i] = newResponse(h)
	}
	return out
}
