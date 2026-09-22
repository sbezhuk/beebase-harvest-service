package harvest

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/sbezhuk/beebase-common/httpx"
)

const (
	CodeReportFromRequired = "from_required"
	CodeReportFromInvalid  = "from_invalid"
	CodeReportToRequired   = "to_required"
	CodeReportToInvalid    = "to_invalid"
	CodeReportFromAfterTo  = "from_after_to"
	CodeReportRangeTooLong = "report_range_too_long"
	maxReportMonths        = 12
)

type ReportTotalResponse struct {
	Product string  `json:"product"`
	Unit    string  `json:"unit"`
	Amount  float64 `json:"amount"`
}

type ReportDataResponse struct {
	From     string                `json:"from"`
	To       string                `json:"to"`
	Harvests []Response            `json:"harvests"`
	Count    int                   `json:"count"`
	Totals   []ReportTotalResponse `json:"totals"`
}

// InternalReportData handles GET /internal/api/v1/hives/{hiveId}/report-data.
// Authentication is applied by the root router's internal-auth middleware.
func (h *Handler) InternalReportData(w http.ResponseWriter, r *http.Request) {
	hiveID, err := uuid.Parse(chi.URLParam(r, "hiveId"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, CodeInvalidHiveID, "hive id must be a valid UUID")
		return
	}
	from, to, fields := parseReportRange(r)
	if len(fields) > 0 {
		httpx.WriteValidationError(w, fields)
		return
	}

	data, err := h.service.GetInternalReportData(r.Context(), hiveID, from, to)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	items := make([]Response, len(data.Harvests))
	for i, item := range data.Harvests {
		items[i] = newResponse(item)
	}
	totals := make([]ReportTotalResponse, len(data.Totals))
	for i, total := range data.Totals {
		totals[i] = ReportTotalResponse{Product: string(total.Product), Unit: string(total.Unit), Amount: total.Amount}
	}
	httpx.WriteJSON(w, http.StatusOK, ReportDataResponse{
		From: from.Format(dateFilterLayout), To: to.Format(dateFilterLayout),
		Harvests: items, Count: len(items), Totals: totals,
	})
}

func parseReportRange(r *http.Request) (from, to time.Time, fields map[string]string) {
	fields = map[string]string{}
	rawFrom, rawTo := r.URL.Query().Get("from"), r.URL.Query().Get("to")
	if rawFrom == "" {
		fields["from"] = CodeReportFromRequired
	} else if parsed, err := time.Parse(dateFilterLayout, rawFrom); err != nil {
		fields["from"] = CodeReportFromInvalid
	} else {
		from = parsed
	}
	if rawTo == "" {
		fields["to"] = CodeReportToRequired
	} else if parsed, err := time.Parse(dateFilterLayout, rawTo); err != nil {
		fields["to"] = CodeReportToInvalid
	} else {
		to = parsed
	}
	if len(fields) > 0 {
		return time.Time{}, time.Time{}, fields
	}
	if from.After(to) {
		fields["to"] = CodeReportFromAfterTo
		return time.Time{}, time.Time{}, fields
	}
	if to.After(from.AddDate(0, maxReportMonths, 0)) {
		fields["to"] = CodeReportRangeTooLong
		return time.Time{}, time.Time{}, fields
	}
	return from, to, nil
}
