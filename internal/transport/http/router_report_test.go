package http

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	httpmw "github.com/sbezhuk/beebase-common/authmw"
	harvesthttp "github.com/sbezhuk/beebase-harvest-service/internal/transport/http/harvest"
)

func TestInternalReportRouteAuthentication(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := harvesthttp.NewHandler(nil, log)
	router := NewRouter(log, nil, handler, httpmw.AccessTokenParser(nil), "internal-secret")
	path := "/internal/api/v1/hives/not-a-uuid/report-data?from=2026-01-01&to=2026-01-02"

	tests := []struct {
		name          string
		authorization string
		wantStatus    int
	}{
		{name: "valid token reaches handler", authorization: "Bearer internal-secret", wantStatus: http.StatusBadRequest},
		{name: "missing token rejected", wantStatus: http.StatusUnauthorized},
		{name: "invalid token rejected", authorization: "Bearer wrong", wantStatus: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}
