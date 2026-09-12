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

func TestParseSearch(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	cases := []struct {
		name       string
		query      string
		wantSearch *string
		wantCode   string
	}{
		{name: "omitted", query: "", wantSearch: nil},
		{name: "empty", query: "search=", wantSearch: nil},
		{name: "one char", query: "search=a", wantCode: CodeInvalidSearch},
		{name: "two chars", query: "search=ab", wantCode: CodeInvalidSearch},
		{name: "three chars", query: "search=hon", wantSearch: strPtr("hon")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+tc.query, nil)
			s, fields := parseSearch(req, nil)
			if tc.wantCode != "" {
				if fields["search"] != tc.wantCode {
					t.Fatalf("fields[search] = %q, want %q", fields["search"], tc.wantCode)
				}
				if s != nil {
					t.Fatalf("search = %v, want nil", s)
				}
				return
			}
			if len(fields) != 0 {
				t.Fatalf("unexpected fields: %v", fields)
			}
			if tc.wantSearch == nil {
				if s != nil {
					t.Fatalf("search = %v, want nil", *s)
				}
				return
			}
			if s == nil || *s != *tc.wantSearch {
				t.Fatalf("search = %v, want %v", s, *tc.wantSearch)
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
