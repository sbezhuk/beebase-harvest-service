// Package harvest holds the HTTP handlers for harvest records nested
// under a hive. Handlers stay thin: they decode/validate the request,
// pull the hiveID (and, for mutating endpoints, harvestID) and the
// caller's raw access token off the request, call into the application
// service, and map the result (or error) to a response. No business
// logic or repository access happens here.
package harvest

import (
	"errors"
	"log/slog"
	"net/http"
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
	CodeHiveNotFound     = "hive_not_found"
	CodeInvalidHiveID    = "invalid_hive_id"
	CodeHarvestNotFound  = "harvest_not_found"
	CodeInvalidHarvestID = "invalid_harvest_id"
)

// Handler exposes the harvest HTTP endpoints. Every method requires the
// request to have already passed through httpmw.RequireAuth.
type Handler struct {
	service *appharvest.Service
	log     *slog.Logger
}

// NewHandler returns a Handler backed by service.
func NewHandler(service *appharvest.Service, log *slog.Logger) *Handler {
	return &Handler{service: service, log: log}
}

// Create handles POST /hives/{hiveID}/harvest.
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

// Get handles GET /hives/{hiveID}/harvest/{harvestID}.
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

// List handles GET /hives/{hiveID}/harvest.
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
	if len(fields) > 0 {
		httpx.WriteValidationError(w, fields)
		return
	}

	harvests, total, err := h.service.List(r.Context(), token, hiveID, p)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, pagination.NewResponse(newListResponse(harvests), p, total))
}

// Update handles PUT /hives/{hiveID}/harvest/{harvestID}.
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

// Delete handles DELETE /hives/{hiveID}/harvest/{harvestID}.
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
