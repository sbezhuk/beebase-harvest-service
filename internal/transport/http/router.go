// Package http wires the HTTP transport: routing, middleware, and the
// handlers that don't yet belong to a specific domain (health, readiness).
package http

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	httpmw "github.com/sbezhuk/beebase-common/authmw"
	"github.com/sbezhuk/beebase-common/httpx"
	"github.com/sbezhuk/beebase-common/internalauth"
	harvesthttp "github.com/sbezhuk/beebase-harvest-service/internal/transport/http/harvest"
)

// NewRouter builds the root HTTP handler for the service.
func NewRouter(
	log *slog.Logger,
	db *pgxpool.Pool,
	harvestHandler *harvesthttp.Handler,
	tokenParser httpmw.AccessTokenParser,
	internalTokens ...string,
) http.Handler {
	internalToken := ""
	if len(internalTokens) > 0 {
		internalToken = internalTokens[0]
	}
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(requestLogger(log))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/health", HealthHandler)
	r.Get("/ready", ReadyHandler(db))
	r.With(internalauth.RequireAuth(internalToken)).Get("/internal/api/v1/harvests/{id}/exists", existsHandler(db, "harvests", false))
	r.With(internalauth.RequireAuth(internalToken)).Get("/internal/api/v1/hives/{hiveId}/report-data", harvestHandler.InternalReportData)
	r.With(internalauth.RequireAuth(internalToken)).Delete("/internal/api/v1/users/{userID}", func(w http.ResponseWriter, req *http.Request) {
		id, err := uuid.Parse(chi.URLParam(req, "userID"))
		if err != nil {
			httpx.WriteError(w, 400, "invalid_user_id", "invalid user id")
			return
		}
		if err := harvestHandler.DeleteUserData(req.Context(), id); err != nil {
			httpx.WriteError(w, 500, "cleanup_failed", "could not delete harvest data")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	r.Group(func(r chi.Router) {
		r.Use(httpmw.RequireAuth(tokenParser))
		r.Get("/api/v1/harvests", harvestHandler.ListAll)
		r.Get("/api/v1/harvests/", harvestHandler.ListAll)

		r.Route("/api/v1/hives/{hiveId}/harvests", func(r chi.Router) {
			r.Post("/", harvestHandler.Create)
			r.Get("/", harvestHandler.List)
			r.Get("/{harvestId}", harvestHandler.Get)
			r.Put("/{harvestId}", harvestHandler.Update)
			r.Delete("/{harvestId}", harvestHandler.Delete)
			r.Delete("/", harvestHandler.DeleteByHive)
		})
	})

	return r
}
func existsHandler(db *pgxpool.Pool, table string, soft bool) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, e := uuid.Parse(chi.URLParam(r, "id"))
		if e != nil {
			http.NotFound(w, r)
			return
		}
		q := "SELECT EXISTS(SELECT 1 FROM " + table + " WHERE id=$1"
		if soft {
			q += " AND deleted_at IS NULL"
		}
		q += ")"
		var ok bool
		if e = db.QueryRow(r.Context(), q, id).Scan(&ok); e != nil {
			http.Error(w, "", 500)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// requestLogger logs each request's method, path, status, and duration
// through slog instead of chi's default stdlib logger.
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			log.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}
