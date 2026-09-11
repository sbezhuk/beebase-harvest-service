package harvest

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	appharvest "github.com/sbezhuk/beebase-harvest-service/internal/application/harvest"
	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
)

func TestWriteServiceError(t *testing.T) {
	h := NewHandler(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name:       "hive not found",
			err:        appharvest.ErrHiveNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   CodeHiveNotFound,
		},
		{
			name:       "harvest not found",
			err:        harvest.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantCode:   CodeHarvestNotFound,
		},
		{
			name:       "duplicate product",
			err:        harvest.ErrDuplicateProduct,
			wantStatus: http.StatusConflict,
			wantCode:   CodeDuplicateProduct,
		},
		{
			name:       "internal error",
			err:        errors.New("db explosion"),
			wantStatus: http.StatusInternalServerError,
			wantCode:   "internal_error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.writeServiceError(rec, tt.err)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			var body struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Error.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q", body.Error.Code, tt.wantCode)
			}
		})
	}
}
