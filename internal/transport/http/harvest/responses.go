package harvest

import (
	"time"

	"github.com/google/uuid"

	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
)

// Response is the public representation of a harvest record.
type Response struct {
	ID        uuid.UUID       `json:"id"`
	HiveID    uuid.UUID       `json:"hive_id"`
	Product   harvest.Product `json:"product"`
	Amount    float64         `json:"amount"`
	Unit      harvest.Unit    `json:"unit"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// newResponse builds a Response for h.
func newResponse(h *harvest.Harvest) Response {
	return Response{
		ID:        h.ID,
		HiveID:    h.HiveID,
		Product:   h.Product,
		Amount:    h.Amount,
		Unit:      h.Unit,
		CreatedAt: h.CreatedAt,
		UpdatedAt: h.UpdatedAt,
	}
}

// newListResponse builds the response for GET, which is always a plain
// array (never wrapped in pagination - there can be at most four harvest
// records per hive, one per product) - never nil, so a hive with no
// harvest records renders as "[]" rather than "null".
func newListResponse(harvests []*harvest.Harvest) []Response {
	out := make([]Response, len(harvests))
	for i, h := range harvests {
		out[i] = newResponse(h)
	}
	return out
}
