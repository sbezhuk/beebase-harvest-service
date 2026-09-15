package harvest

import "testing"

func amountPtr(f float64) *float64 { return &f }

const testHarvestedAt = "2026-09-01"

func TestCreateRequest_Validate(t *testing.T) {
	tests := []struct {
		name string
		req  CreateRequest
		want map[string]string
	}{
		{
			name: "valid honey kg",
			req:  CreateRequest{Product: "HONEY", Amount: amountPtr(12.5), Unit: "kg", HarvestedAt: testHarvestedAt},
			want: map[string]string{},
		},
		{
			name: "valid honey l",
			req:  CreateRequest{Product: "HONEY", Amount: amountPtr(10), Unit: "l", HarvestedAt: testHarvestedAt},
			want: map[string]string{},
		},
		{
			name: "valid pollen g",
			req:  CreateRequest{Product: "POLLEN", Amount: amountPtr(500), Unit: "g", HarvestedAt: testHarvestedAt},
			want: map[string]string{},
		},
		{
			name: "valid pollen kg",
			req:  CreateRequest{Product: "POLLEN", Amount: amountPtr(1), Unit: "kg", HarvestedAt: testHarvestedAt},
			want: map[string]string{},
		},
		{
			name: "valid propolis g",
			req:  CreateRequest{Product: "PROPOLIS", Amount: amountPtr(150), Unit: "g", HarvestedAt: testHarvestedAt},
			want: map[string]string{},
		},
		{
			name: "valid wax g",
			req:  CreateRequest{Product: "WAX", Amount: amountPtr(800), Unit: "g", HarvestedAt: testHarvestedAt},
			want: map[string]string{},
		},
		{
			name: "zero amount is valid",
			req:  CreateRequest{Product: "HONEY", Amount: amountPtr(0), Unit: "kg", HarvestedAt: testHarvestedAt},
			want: map[string]string{},
		},
		{
			name: "missing product",
			req:  CreateRequest{Product: "", Amount: amountPtr(1), Unit: "kg", HarvestedAt: testHarvestedAt},
			want: map[string]string{"product": CodeProductRequired},
		},
		{
			name: "invalid product",
			req:  CreateRequest{Product: "SWARM", Amount: amountPtr(1), Unit: "kg", HarvestedAt: testHarvestedAt},
			want: map[string]string{"product": CodeProductInvalid},
		},
		{
			name: "missing amount",
			req:  CreateRequest{Product: "HONEY", Amount: nil, Unit: "kg", HarvestedAt: testHarvestedAt},
			want: map[string]string{"amount": CodeAmountRequired},
		},
		{
			name: "negative amount",
			req:  CreateRequest{Product: "HONEY", Amount: amountPtr(-1), Unit: "kg", HarvestedAt: testHarvestedAt},
			want: map[string]string{"amount": CodeAmountNegative},
		},
		{
			name: "missing unit",
			req:  CreateRequest{Product: "HONEY", Amount: amountPtr(1), Unit: "", HarvestedAt: testHarvestedAt},
			want: map[string]string{"unit": CodeUnitRequired},
		},
		{
			name: "invalid unit",
			req:  CreateRequest{Product: "HONEY", Amount: amountPtr(1), Unit: "ml", HarvestedAt: testHarvestedAt},
			want: map[string]string{"unit": CodeUnitInvalid},
		},
		{
			name: "honey + g is an invalid combination",
			req:  CreateRequest{Product: "HONEY", Amount: amountPtr(1), Unit: "g", HarvestedAt: testHarvestedAt},
			want: map[string]string{"unit": CodeUnitCombination},
		},
		{
			name: "pollen + l is an invalid combination",
			req:  CreateRequest{Product: "POLLEN", Amount: amountPtr(1), Unit: "l", HarvestedAt: testHarvestedAt},
			want: map[string]string{"unit": CodeUnitCombination},
		},
		{
			name: "propolis + kg is an invalid combination",
			req:  CreateRequest{Product: "PROPOLIS", Amount: amountPtr(1), Unit: "kg", HarvestedAt: testHarvestedAt},
			want: map[string]string{"unit": CodeUnitCombination},
		},
		{
			name: "wax + kg is an invalid combination",
			req:  CreateRequest{Product: "WAX", Amount: amountPtr(1), Unit: "kg", HarvestedAt: testHarvestedAt},
			want: map[string]string{"unit": CodeUnitCombination},
		},
		{
			name: "missing harvested_at",
			req:  CreateRequest{Product: "HONEY", Amount: amountPtr(1), Unit: "kg", HarvestedAt: ""},
			want: map[string]string{"harvestedAt": CodeHarvestedAtRequired},
		},
		{
			name: "invalid harvested_at format",
			req:  CreateRequest{Product: "HONEY", Amount: amountPtr(1), Unit: "kg", HarvestedAt: "2026-09-01T00:00:00Z"},
			want: map[string]string{"harvestedAt": CodeHarvestedAtInvalid},
		},
		{
			name: "everything wrong at once",
			req:  CreateRequest{Product: "", Amount: nil, Unit: "", HarvestedAt: ""},
			want: map[string]string{
				"product":     CodeProductRequired,
				"amount":      CodeAmountRequired,
				"unit":        CodeUnitRequired,
				"harvestedAt": CodeHarvestedAtRequired,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.req.Validate()
			if len(got) != len(tt.want) {
				t.Fatalf("Validate() = %v, want %v", got, tt.want)
			}
			for field, wantCode := range tt.want {
				if gotCode, ok := got[field]; !ok || gotCode != wantCode {
					t.Errorf("field %q: got code %q, want %q", field, gotCode, wantCode)
				}
			}
		})
	}
}

func TestUpdateRequest_Validate(t *testing.T) {
	if fields := (&UpdateRequest{Product: "HONEY", Amount: amountPtr(1), Unit: "kg", HarvestedAt: testHarvestedAt}).Validate(); len(fields) != 0 {
		t.Errorf("valid update: expected no errors, got %v", fields)
	}

	fields := (&UpdateRequest{Product: "HONEY", Amount: amountPtr(1), Unit: "g", HarvestedAt: testHarvestedAt}).Validate()
	if code := fields["unit"]; code != CodeUnitCombination {
		t.Errorf("unit code = %q, want %q", code, CodeUnitCombination)
	}

	fields = (&UpdateRequest{Product: "", Amount: amountPtr(1), Unit: "kg", HarvestedAt: testHarvestedAt}).Validate()
	if code := fields["product"]; code != CodeProductRequired {
		t.Errorf("product code = %q, want %q", code, CodeProductRequired)
	}

	fields = (&UpdateRequest{Product: "HONEY", Amount: amountPtr(1), Unit: "kg", HarvestedAt: ""}).Validate()
	if code := fields["harvestedAt"]; code != CodeHarvestedAtRequired {
		t.Errorf("harvested_at code = %q, want %q", code, CodeHarvestedAtRequired)
	}
}
