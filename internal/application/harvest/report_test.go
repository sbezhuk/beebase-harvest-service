package harvest_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	appharvest "github.com/sbezhuk/beebase-harvest-service/internal/application/harvest"
	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
)

func TestGetInternalReportDataUsesInclusiveDateSemanticsAndCanonicalTotals(t *testing.T) {
	repo := newFakeHarvestRepo()
	service := appharvest.NewService(repo, newFakeHiveVerifier())
	hiveID := uuid.New()
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	for _, item := range []*harvest.Harvest{
		harvest.New(hiveID, harvest.ProductHoney, 2, harvest.UnitKilogram, from.AddDate(0, 0, -1)),
		harvest.New(hiveID, harvest.ProductHoney, 3, harvest.UnitKilogram, from),
		harvest.New(hiveID, harvest.ProductHoney, 4, harvest.UnitKilogram, to),
		harvest.New(hiveID, harvest.ProductHoney, 5, harvest.UnitKilogram, to.AddDate(0, 0, 1)),
	} {
		if err := repo.Create(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}

	data, err := service.GetInternalReportData(context.Background(), hiveID, from, to)
	if err != nil {
		t.Fatalf("GetInternalReportData: %v", err)
	}
	if len(data.Harvests) != 2 {
		t.Fatalf("harvests = %d, want 2", len(data.Harvests))
	}
	if len(data.Totals) != 1 || data.Totals[0].Amount != 7 {
		t.Fatalf("totals = %+v, want one 7 kg total", data.Totals)
	}
}
