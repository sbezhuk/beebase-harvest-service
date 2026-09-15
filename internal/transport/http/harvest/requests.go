package harvest

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sbezhuk/beebase-common/httpx"
	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
)

// Field validation error codes. Each is a stable key a client can map to
// a localized message; the field carrying no error is simply absent from
// the response's "fields" map.
const (
	CodeProductRequired     = "product_required"
	CodeProductInvalid      = "product_invalid"
	CodeAmountRequired      = "amount_required"
	CodeAmountNegative      = "amount_negative"
	CodeUnitRequired        = "unit_required"
	CodeUnitInvalid         = "unit_invalid"
	CodeUnitCombination     = "unit_invalid_for_product"
	CodeHarvestedAtRequired = "harvested_at_required"
	CodeHarvestedAtInvalid  = "harvested_at_invalid"
)

// validatable is implemented by every request DTO in this package.
// Validate returns a map of field name to error code, empty if valid.
type validatable interface {
	Validate() map[string]string
}

// decodeAndValidate decodes the request body into dst and validates it,
// writing an appropriate error response and returning false if either
// step fails.
func decodeAndValidate(w http.ResponseWriter, r *http.Request, dst validatable) bool {
	defer func() { _ = r.Body.Close() }()

	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidBody, "request body must be valid JSON")
		return false
	}

	if fields := dst.Validate(); len(fields) > 0 {
		httpx.WriteValidationError(w, fields)
		return false
	}

	return true
}

// Request is the shared body shape of POST and PUT: both replace every
// editable field (PUT semantics for updates too, per this resource's own
// nature - there is no partial patch).
type Request struct {
	Product string `json:"product"`
	// Amount uses a pointer so a request that omits it entirely - as
	// opposed to explicitly sending 0, which is a valid amount - can be
	// told apart and rejected as CodeAmountRequired rather than
	// CodeAmountNegative.
	Amount *float64 `json:"amount"`
	Unit   string   `json:"unit"`
	// HarvestedAt is the calendar date when the product was actually collected,
	// distinct from the record's own created_at/updated_at bookkeeping
	// timestamps.
	HarvestedAt string `json:"harvestedAt"`
}

// CreateRequest is the body of POST /hives/{hiveId}/harvests.
type CreateRequest Request

func (r *CreateRequest) Validate() map[string]string {
	return Request(*r).validate()
}

// UpdateRequest is the body of PUT /hives/{hiveId}/harvests/{harvestId}.
type UpdateRequest Request

func (r *UpdateRequest) Validate() map[string]string {
	return Request(*r).validate()
}

func (r Request) validate() map[string]string {
	fields := map[string]string{}

	product := harvest.Product(r.Product)
	switch {
	case r.Product == "":
		fields["product"] = CodeProductRequired
	case !product.Valid():
		fields["product"] = CodeProductInvalid
	}

	switch {
	case r.Amount == nil:
		fields["amount"] = CodeAmountRequired
	case *r.Amount < 0:
		fields["amount"] = CodeAmountNegative
	}

	unit := harvest.Unit(r.Unit)
	switch {
	case r.Unit == "":
		fields["unit"] = CodeUnitRequired
	case !unit.Valid():
		fields["unit"] = CodeUnitInvalid
	}

	switch {
	case r.HarvestedAt == "":
		fields["harvestedAt"] = CodeHarvestedAtRequired
	default:
		if _, err := time.Parse("2006-01-02", r.HarvestedAt); err != nil {
			fields["harvestedAt"] = CodeHarvestedAtInvalid
		}
	}

	// Only check the product/unit combination once both are independently
	// valid - otherwise an already-reported product or unit error would be
	// masked by a confusing combination error.
	if _, ok := fields["product"]; !ok {
		if _, ok := fields["unit"]; !ok {
			if !harvest.ValidCombination(product, unit) {
				fields["unit"] = CodeUnitCombination
			}
		}
	}

	return fields
}
