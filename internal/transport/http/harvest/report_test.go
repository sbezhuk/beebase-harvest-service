package harvest

import (
	"net/http/httptest"
	"testing"
)

func TestParseReportRange(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr string
	}{
		{name: "valid", query: "from=2026-01-01&to=2027-01-01"},
		{name: "missing from", query: "to=2026-01-01", wantErr: CodeReportFromRequired},
		{name: "missing to", query: "from=2026-01-01", wantErr: CodeReportToRequired},
		{name: "malformed from", query: "from=2026-1-01&to=2026-01-01", wantErr: CodeReportFromInvalid},
		{name: "malformed to", query: "from=2026-01-01&to=nope", wantErr: CodeReportToInvalid},
		{name: "reversed", query: "from=2026-02-01&to=2026-01-01", wantErr: CodeReportFromAfterTo},
		{name: "too long", query: "from=2026-01-01&to=2027-01-02", wantErr: CodeReportRangeTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to, fields := parseReportRange(httptest.NewRequest("GET", "/?"+tt.query, nil))
			if tt.wantErr != "" {
				if fields["from"] != tt.wantErr && fields["to"] != tt.wantErr {
					t.Fatalf("fields = %v, want %q", fields, tt.wantErr)
				}
				if !from.IsZero() || !to.IsZero() {
					t.Fatalf("invalid range returned %v..%v", from, to)
				}
				return
			}
			if len(fields) != 0 || from.IsZero() || to.IsZero() {
				t.Fatalf("range = %v..%v, fields = %v", from, to, fields)
			}
		})
	}
}
