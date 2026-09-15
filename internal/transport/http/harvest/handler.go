// Package harvest holds the HTTP handlers for harvest records. Handlers stay
// thin: they decode/validate the request,
// pull the hiveID (and, for mutating endpoints, harvestID) and the
// caller's raw access token off the request, call into the application
// service, and map the result (or error) to a response. No business
// logic or repository access happens here.
package harvest

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	httpmw "github.com/sbezhuk/beebase-common/authmw"
	"github.com/sbezhuk/beebase-common/httpx"
	"github.com/sbezhuk/beebase-common/pagination"
	appharvest "github.com/sbezhuk/beebase-harvest-service/internal/application/harvest"
	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
)

// Error codes for harvest failures, returned as the top-level
// "error.code". Each is a stable key a client can map to a localized
// message. CodeHiveNotFound intentionally reuses hive-service's own code
// string, since it's the same meaning from the client's point of view
// regardless of which service returned it.
const (
	CodeHiveNotFound          = "hive_not_found"
	CodeInvalidHiveID         = "invalid_hive_id"
	CodeHarvestNotFound       = "harvest_not_found"
	CodeInvalidHarvestID      = "invalid_harvest_id"
	CodeInvalidProduct        = "invalid_product"
	CodeInvalidAmountOperator = "invalid_amount_operator"
	CodeInvalidAmount         = "invalid_amount"
	CodeInvalidDateFrom       = "invalid_date_from"
	CodeInvalidDateTo         = "invalid_date_to"
	CodeInvalidDateRange      = "invalid_date_range"
	CodeInvalidSortOrder      = "invalid_sort_order"
)

// dateFilterLayout is the ISO 8601 calendar-date format the date_from/
// date_to query parameters must use - a date only, no time-of-day or
// offset (unlike harvested_at in the request body, which is a full RFC
// 3339 timestamp).
const dateFilterLayout = "2006-01-02"

// Handler exposes the harvest HTTP endpoints. Every method requires the
// request to have already passed through httpmw.RequireAuth.
type Handler struct {
	service   *appharvest.Service
	log       *slog.Logger
	reminders interface {
		Cleanup(context.Context, string, string, uuid.UUID) error
	}
}

// NewHandler returns a Handler backed by service.
func NewHandler(service *appharvest.Service, log *slog.Logger, reminders ...interface {
	Cleanup(context.Context, string, string, uuid.UUID) error
}) *Handler {
	h := &Handler{service: service, log: log}
	if len(reminders) > 0 {
		h.reminders = reminders[0]
	}
	return h
}

// Create handles POST /hives/{hiveID}/harvests.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	token, ok := h.requireAuth(w, r)
	if !ok {
		return
	}

	hiveID, ok := h.pathHiveID(w, r)
	if !ok {
		return
	}

	var req CreateRequest
	if !decodeAndValidate(w, r, &req) {
		return
	}
	// Already validated as well-formed by CreateRequest.Validate.
	harvestedAt, _ := time.Parse(time.RFC3339, req.HarvestedAt)

	created, err := h.service.Create(r.Context(), token, hiveID, appharvest.CreateInput{
		Product:     harvest.Product(req.Product),
		Amount:      *req.Amount,
		Unit:        harvest.Unit(req.Unit),
		HarvestedAt: harvestedAt,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, newResponse(created))
}

// Get handles GET /hives/{hiveID}/harvests/{harvestID}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	token, ok := h.requireAuth(w, r)
	if !ok {
		return
	}

	hiveID, ok := h.pathHiveID(w, r)
	if !ok {
		return
	}

	harvestID, ok := h.pathHarvestID(w, r)
	if !ok {
		return
	}

	got, err := h.service.Get(r.Context(), token, hiveID, harvestID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, newResponse(got))
}

// List handles GET /hives/{hiveID}/harvests.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	token, ok := h.requireAuth(w, r)
	if !ok {
		return
	}

	hiveID, ok := h.pathHiveID(w, r)
	if !ok {
		return
	}

	p, fields := pagination.ParseParams(r)
	product, fields := parseProductFilter(r, fields)
	amountOperator, amount, fields := parseAmountFilter(r, fields)
	dateFrom, dateTo, fields := parseDateFilter(r, fields)
	sortOrder, fields := parseSortOrder(r, fields)
	if len(fields) > 0 {
		httpx.WriteValidationError(w, fields)
		return
	}

	harvests, total, err := h.service.List(r.Context(), token, hiveID, p, product, amountOperator, amount, dateFrom, dateTo, sortOrder)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, pagination.NewResponse(newListResponse(harvests), p, total))
}

// ListAll handles GET /api/v1/harvests.
func (h *Handler) ListAll(w http.ResponseWriter, r *http.Request) {
	token, ok := h.requireAuth(w, r)
	if !ok {
		return
	}

	p, fields := pagination.ParseParams(r)
	product, fields := parseProductFilter(r, fields)
	amountOperator, amount, fields := parseAmountFilter(r, fields)
	dateFrom, dateTo, fields := parseDateFilter(r, fields)
	sortOrder, fields := parseSortOrder(r, fields)
	if len(fields) > 0 {
		httpx.WriteValidationError(w, fields)
		return
	}

	harvests, total, err := h.service.ListAll(r.Context(), token, p, product, amountOperator, amount, dateFrom, dateTo, sortOrder)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, pagination.NewResponse(newListResponse(harvests), p, total))
}

// parseProductFilter reads the optional "product" query parameter: an
// exact match against one of harvest's known products.
func parseProductFilter(r *http.Request, fields map[string]string) (*harvest.Product, map[string]string) {
	v := r.URL.Query().Get("product")
	if v == "" {
		return nil, fields
	}
	product := harvest.Product(v)
	if !product.Valid() {
		if fields == nil {
			fields = map[string]string{}
		}
		fields["product"] = CodeInvalidProduct
		return nil, fields
	}
	return &product, fields
}

// parseAmountFilter reads the optional "amount_operator"/"amount" query
// parameter pair. Both are optional, but only together: given alone,
// the missing one is rejected the same as an invalid value for it. The
// amount value follows the same non-negative rule as the request body's
// own amount field.
func parseAmountFilter(r *http.Request, fields map[string]string) (*harvest.AmountOperator, *float64, map[string]string) {
	rawOperator := r.URL.Query().Get("amount_operator")
	rawAmount := r.URL.Query().Get("amount")

	if rawOperator == "" && rawAmount == "" {
		return nil, nil, fields
	}

	var operator *harvest.AmountOperator
	op := harvest.AmountOperator(rawOperator)
	if rawOperator == "" || !op.Valid() {
		if fields == nil {
			fields = map[string]string{}
		}
		fields["amount_operator"] = CodeInvalidAmountOperator
	} else {
		operator = &op
	}

	var amount *float64
	v, err := strconv.ParseFloat(rawAmount, 64)
	if rawAmount == "" || err != nil || v < 0 {
		if fields == nil {
			fields = map[string]string{}
		}
		fields["amount"] = CodeInvalidAmount
	} else {
		amount = &v
	}

	return operator, amount, fields
}

// parseDateFilter reads the optional "date_from"/"date_to" query
// parameters: each is an ISO 8601 calendar date (YYYY-MM-DD), independently
// optional, restricting harvested_at. date_from is returned as that day's
// start (00:00:00 UTC), an inclusive lower bound. date_to is returned as
// the start of the following day (00:00:00 UTC), an exclusive upper bound
// - since the filter must match the requested day in full, "harvested_at
// < date_to" (the day after) does the same job as an inclusive same-day
// upper bound would, without depending on the column's time resolution.
// When both are given, date_from must not fall after date_to (as calendar
// dates, not as the adjusted bounds returned here); given alone, each
// applies independently, and neither requires the other.
func parseDateFilter(r *http.Request, fields map[string]string) (dateFrom, dateTo *time.Time, _ map[string]string) {
	rawFrom := r.URL.Query().Get("date_from")
	rawTo := r.URL.Query().Get("date_to")

	var fromDay, toDay *time.Time

	if rawFrom != "" {
		parsed, err := time.Parse(dateFilterLayout, rawFrom)
		if err != nil {
			if fields == nil {
				fields = map[string]string{}
			}
			fields["date_from"] = CodeInvalidDateFrom
		} else {
			fromDay = &parsed
			dateFrom = &parsed
		}
	}

	if rawTo != "" {
		parsed, err := time.Parse(dateFilterLayout, rawTo)
		if err != nil {
			if fields == nil {
				fields = map[string]string{}
			}
			fields["date_to"] = CodeInvalidDateTo
		} else {
			toDay = &parsed
			exclusive := parsed.AddDate(0, 0, 1)
			dateTo = &exclusive
		}
	}

	if fromDay != nil && toDay != nil && fromDay.After(*toDay) {
		if fields == nil {
			fields = map[string]string{}
		}
		fields["date_to"] = CodeInvalidDateRange
	}

	return dateFrom, dateTo, fields
}

// parseSortOrder reads the optional "sortOrder" query parameter, which
// requests the list be ordered by creation date instead of the endpoint's
// default order (which today is the harvest's own business date,
// HarvestedAt, descending). A missing value means "use the default order"
// (nil); an invalid value ("asc"/"desc" are the only accepted ones) is
// reported as a validation error the same way parseProductFilter reports
// one.
func parseSortOrder(r *http.Request, fields map[string]string) (*string, map[string]string) {
	s := r.URL.Query().Get("sortOrder")
	if s == "" {
		return nil, fields
	}
	if s != "asc" && s != "desc" {
		if fields == nil {
			fields = map[string]string{}
		}
		fields["sortOrder"] = CodeInvalidSortOrder
		return nil, fields
	}
	return &s, fields
}

// Update handles PUT /hives/{hiveID}/harvests/{harvestID}.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	token, ok := h.requireAuth(w, r)
	if !ok {
		return
	}

	hiveID, ok := h.pathHiveID(w, r)
	if !ok {
		return
	}

	harvestID, ok := h.pathHarvestID(w, r)
	if !ok {
		return
	}

	var req UpdateRequest
	if !decodeAndValidate(w, r, &req) {
		return
	}
	// Already validated as well-formed by UpdateRequest.Validate.
	harvestedAt, _ := time.Parse(time.RFC3339, req.HarvestedAt)

	updated, err := h.service.Update(r.Context(), token, hiveID, harvestID, appharvest.UpdateInput{
		Product:     harvest.Product(req.Product),
		Amount:      *req.Amount,
		Unit:        harvest.Unit(req.Unit),
		HarvestedAt: harvestedAt,
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, newResponse(updated))
}

// Delete handles DELETE /hives/{hiveID}/harvests/{harvestID}.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	token, ok := h.requireAuth(w, r)
	if !ok {
		return
	}

	hiveID, ok := h.pathHiveID(w, r)
	if !ok {
		return
	}

	harvestID, ok := h.pathHarvestID(w, r)
	if !ok {
		return
	}

	if err := h.service.Delete(r.Context(), token, hiveID, harvestID); err != nil {
		h.writeServiceError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	if h.reminders != nil {
		if err := h.reminders.Cleanup(r.Context(), token, "harvest", harvestID); err != nil {
			h.log.Warn("reminder cleanup failed", "entity_type", "harvest", "entity_id", harvestID, "error", err)
		}
	}
}

// requireAuth returns the caller's raw access token (read off the
// request's own Authorization header, which httpmw.RequireAuth already
// validated) so it can be forwarded to hive-service. Unlike most BeeBase
// handlers, the authenticated userID itself is never used here: this
// service verifies ownership by asking hive-service, not by scoping a
// local user_id column (see application/harvest.HiveVerifier).
func (h *Handler) requireAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	if _, ok := httpmw.UserIDFromContext(r.Context()); !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpmw.CodeMissingAuthorization, "missing authentication")
		return "", false
	}

	const prefix = "Bearer "
	token := strings.TrimPrefix(r.Header.Get("Authorization"), prefix)

	return token, true
}

func (h *Handler) pathHiveID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "hiveID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, CodeInvalidHiveID, "hive id must be a valid UUID")
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) pathHarvestID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "harvestID"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, CodeInvalidHarvestID, "harvest id must be a valid UUID")
		return uuid.Nil, false
	}
	return id, true
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appharvest.ErrHiveNotFound):
		httpx.WriteError(w, http.StatusNotFound, CodeHiveNotFound, "hive not found")
	case errors.Is(err, harvest.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, CodeHarvestNotFound, "harvest not found")
	default:
		httpx.WriteInternalError(w, h.log, err)
	}
}
