package harvest

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestParseProductFilter(t *testing.T) {
	cases := []struct {
		name        string
		query       string
		wantProduct *harvest.Product
		wantCode    string
	}{
		{name: "omitted", query: ""},
		{name: "honey", query: "product=HONEY", wantProduct: productPtr(harvest.ProductHoney)},
		{name: "pollen", query: "product=POLLEN", wantProduct: productPtr(harvest.ProductPollen)},
		{name: "propolis", query: "product=PROPOLIS", wantProduct: productPtr(harvest.ProductPropolis)},
		{name: "wax", query: "product=WAX", wantProduct: productPtr(harvest.ProductWax)},
		{name: "invalid", query: "product=SWARM", wantCode: CodeInvalidProduct},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+tc.query, nil)
			product, fields := parseProductFilter(req, nil)
			if tc.wantCode != "" {
				if fields["product"] != tc.wantCode {
					t.Fatalf("fields[product] = %q, want %q", fields["product"], tc.wantCode)
				}
				if product != nil {
					t.Fatalf("product = %v, want nil", *product)
				}
				return
			}
			if len(fields) != 0 {
				t.Fatalf("unexpected fields: %v", fields)
			}
			if tc.wantProduct == nil {
				if product != nil {
					t.Fatalf("product = %v, want nil", *product)
				}
				return
			}
			if product == nil || *product != *tc.wantProduct {
				t.Fatalf("product = %v, want %v", product, *tc.wantProduct)
			}
		})
	}
}

func productPtr(p harvest.Product) *harvest.Product { return &p }

func TestParseAmountFilter(t *testing.T) {
	cases := []struct {
		name         string
		query        string
		wantOperator *harvest.AmountOperator
		wantAmount   *float64
		wantFields   map[string]string
	}{
		{name: "omitted", query: ""},
		{
			name:         "gt",
			query:        "amount_operator=gt&amount=10",
			wantOperator: amountOperatorPtr(harvest.AmountOperatorGT),
			wantAmount:   amountPtr(10),
		},
		{
			name:         "lt",
			query:        "amount_operator=lt&amount=10",
			wantOperator: amountOperatorPtr(harvest.AmountOperatorLT),
			wantAmount:   amountPtr(10),
		},
		{
			name:         "eq",
			query:        "amount_operator=eq&amount=10",
			wantOperator: amountOperatorPtr(harvest.AmountOperatorEQ),
			wantAmount:   amountPtr(10),
		},
		{
			name:       "invalid operator",
			query:      "amount_operator=gte&amount=10",
			wantFields: map[string]string{"amount_operator": CodeInvalidAmountOperator},
		},
		{
			name:       "invalid amount",
			query:      "amount_operator=gt&amount=notanumber",
			wantFields: map[string]string{"amount": CodeInvalidAmount},
		},
		{
			name:       "negative amount",
			query:      "amount_operator=gt&amount=-1",
			wantFields: map[string]string{"amount": CodeInvalidAmount},
		},
		{
			name:       "operator without amount",
			query:      "amount_operator=gt",
			wantFields: map[string]string{"amount": CodeInvalidAmount},
		},
		{
			name:       "amount without operator",
			query:      "amount=10",
			wantFields: map[string]string{"amount_operator": CodeInvalidAmountOperator},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+tc.query, nil)
			operator, amount, fields := parseAmountFilter(req, nil)
			if len(tc.wantFields) > 0 {
				for field, code := range tc.wantFields {
					if fields[field] != code {
						t.Fatalf("fields[%q] = %q, want %q", field, fields[field], code)
					}
				}
				return
			}
			if len(fields) != 0 {
				t.Fatalf("unexpected fields: %v", fields)
			}
			if tc.wantOperator == nil {
				if operator != nil {
					t.Fatalf("operator = %v, want nil", *operator)
				}
			} else if operator == nil || *operator != *tc.wantOperator {
				t.Fatalf("operator = %v, want %v", operator, *tc.wantOperator)
			}
			if tc.wantAmount == nil {
				if amount != nil {
					t.Fatalf("amount = %v, want nil", *amount)
				}
			} else if amount == nil || *amount != *tc.wantAmount {
				t.Fatalf("amount = %v, want %v", amount, *tc.wantAmount)
			}
		})
	}
}

func amountOperatorPtr(o harvest.AmountOperator) *harvest.AmountOperator { return &o }

func TestParseDateFilter(t *testing.T) {
	utcDate := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}

	cases := []struct {
		name         string
		query        string
		wantDateFrom *time.Time
		wantDateTo   *time.Time
		wantFields   map[string]string
	}{
		{name: "omitted"},
		{
			name:         "date_from only",
			query:        "date_from=2026-01-01",
			wantDateFrom: timePtr(utcDate(2026, 1, 1)),
		},
		{
			// date_to is returned as the exclusive start of the next day.
			name:       "date_to only",
			query:      "date_to=2026-09-13",
			wantDateTo: timePtr(utcDate(2026, 9, 14)),
		},
		{
			name:         "both",
			query:        "date_from=2026-01-01&date_to=2026-09-13",
			wantDateFrom: timePtr(utcDate(2026, 1, 1)),
			wantDateTo:   timePtr(utcDate(2026, 9, 14)),
		},
		{
			name:         "exact boundary: date_from equals date_to",
			query:        "date_from=2026-09-13&date_to=2026-09-13",
			wantDateFrom: timePtr(utcDate(2026, 9, 13)),
			wantDateTo:   timePtr(utcDate(2026, 9, 14)),
		},
		{
			name:       "invalid date_from format",
			query:      "date_from=2026/01/01",
			wantFields: map[string]string{"date_from": CodeInvalidDateFrom},
		},
		{
			name:       "invalid date_to format",
			query:      "date_to=13-09-2026",
			wantFields: map[string]string{"date_to": CodeInvalidDateTo},
		},
		{
			name:       "date_from is a full timestamp, not a date",
			query:      "date_from=2026-01-01T00:00:00Z",
			wantFields: map[string]string{"date_from": CodeInvalidDateFrom},
		},
		{
			name:       "date_from after date_to",
			query:      "date_from=2026-09-14&date_to=2026-09-13",
			wantFields: map[string]string{"date_to": CodeInvalidDateRange},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+tc.query, nil)
			dateFrom, dateTo, fields := parseDateFilter(req, nil)
			if len(tc.wantFields) > 0 {
				for field, code := range tc.wantFields {
					if fields[field] != code {
						t.Fatalf("fields[%q] = %q, want %q", field, fields[field], code)
					}
				}
				return
			}
			if len(fields) != 0 {
				t.Fatalf("unexpected fields: %v", fields)
			}
			if tc.wantDateFrom == nil {
				if dateFrom != nil {
					t.Fatalf("dateFrom = %v, want nil", *dateFrom)
				}
			} else if dateFrom == nil || !dateFrom.Equal(*tc.wantDateFrom) {
				t.Fatalf("dateFrom = %v, want %v", dateFrom, *tc.wantDateFrom)
			}
			if tc.wantDateTo == nil {
				if dateTo != nil {
					t.Fatalf("dateTo = %v, want nil", *dateTo)
				}
			} else if dateTo == nil || !dateTo.Equal(*tc.wantDateTo) {
				t.Fatalf("dateTo = %v, want %v", dateTo, *tc.wantDateTo)
			}
		})
	}
}

func timePtr(t time.Time) *time.Time { return &t }

func TestParseSortOrder(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	cases := []struct {
		name          string
		query         string
		wantSortOrder *string
		wantCode      string
	}{
		{
			name:          "omitted",
			query:         "",
			wantSortOrder: nil,
		},
		{
			name:          "asc",
			query:         "sortOrder=asc",
			wantSortOrder: strPtr("asc"),
		},
		{
			name:          "desc",
			query:         "sortOrder=desc",
			wantSortOrder: strPtr("desc"),
		},
		{
			name:     "invalid",
			query:    "sortOrder=newest",
			wantCode: CodeInvalidSortOrder,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+tc.query, nil)
			s, fields := parseSortOrder(req, nil)
			if tc.wantCode != "" {
				if fields["sortOrder"] != tc.wantCode {
					t.Fatalf("fields[sortOrder] = %q, want %q", fields["sortOrder"], tc.wantCode)
				}
				if s != nil {
					t.Fatalf("sortOrder = %v, want nil", s)
				}
				return
			}
			if len(fields) != 0 {
				t.Fatalf("unexpected fields: %v", fields)
			}
			if tc.wantSortOrder == nil && s != nil {
				t.Fatalf("sortOrder = %v, want nil", s)
			}
			if tc.wantSortOrder != nil {
				if s == nil || *s != *tc.wantSortOrder {
					t.Fatalf("sortOrder = %v, want %v", s, *tc.wantSortOrder)
				}
			}
		})
	}
}
