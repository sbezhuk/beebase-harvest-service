//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sbezhuk/beebase-common/pagination"
	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
	repopostgres "github.com/sbezhuk/beebase-harvest-service/internal/repository/postgres"
)

var testHarvestedAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func TestHarvestRepository_CreateAndGet(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()

	h := harvest.New(hiveID, harvest.ProductHoney, 12.5, harvest.UnitKilogram, testHarvestedAt)
	if err := repo.Create(ctx, h); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, hiveID, h.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Product != harvest.ProductHoney || got.Amount != 12.5 || got.Unit != harvest.UnitKilogram {
		t.Errorf("got = %+v, want HONEY 12.5 kg", got)
	}
	if !got.HarvestedAt.Equal(testHarvestedAt) {
		t.Errorf("HarvestedAt = %v, want %v", got.HarvestedAt, testHarvestedAt)
	}
}

func TestHarvestRepository_GetByID_NotFound(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)

	_, err = repo.GetByID(ctx, uuid.New(), uuid.New())
	if !errors.Is(err, harvest.ErrNotFound) {
		t.Fatalf("GetByID for unknown harvest: got %v, want ErrNotFound", err)
	}
}

// TestHarvestRepository_GetByID_WrongHive_NotFound proves a harvest must
// belong to the specific hive asked for.
func TestHarvestRepository_GetByID_WrongHive_NotFound(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveA := uuid.New()
	hiveB := uuid.New()

	h := harvest.New(hiveA, harvest.ProductHoney, 10, harvest.UnitKilogram, testHarvestedAt)
	if err := repo.Create(ctx, h); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = repo.GetByID(ctx, hiveB, h.ID)
	if !errors.Is(err, harvest.ErrNotFound) {
		t.Fatalf("GetByID via wrong hive: got %v, want ErrNotFound", err)
	}
}

// TestHarvestRepository_Create_MultipleRecordsForSameProduct proves a
// hive can carry several harvest records for the same product - the
// UNIQUE (hive_id, product) constraint no longer exists.
func TestHarvestRepository_Create_MultipleRecordsForSameProduct(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()

	first := harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram, testHarvestedAt)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	second := harvest.New(hiveID, harvest.ProductHoney, 5, harvest.UnitKilogram, testHarvestedAt.AddDate(0, 0, 17))
	if err := repo.Create(ctx, second); err != nil {
		t.Fatalf("second Create for same product: %v", err)
	}

	list, total, err := repo.ListByHive(ctx, hiveID, pagination.Params{Page: 1, Limit: 20}, nil, nil, nil)
	if err != nil {
		t.Fatalf("ListByHive: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("ListByHive/total = %d/%d, want 2/2", len(list), total)
	}
}

func TestHarvestRepository_ListByHive(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()
	otherHive := uuid.New()

	if err := repo.Create(ctx, harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram, testHarvestedAt)); err != nil {
		t.Fatalf("create honey: %v", err)
	}
	if err := repo.Create(ctx, harvest.New(hiveID, harvest.ProductPollen, 500, harvest.UnitGram, testHarvestedAt)); err != nil {
		t.Fatalf("create pollen: %v", err)
	}
	if err := repo.Create(ctx, harvest.New(otherHive, harvest.ProductWax, 200, harvest.UnitGram, testHarvestedAt)); err != nil {
		t.Fatalf("create wax in other hive: %v", err)
	}

	list, total, err := repo.ListByHive(ctx, hiveID, pagination.Params{Page: 1, Limit: 20}, nil, nil, nil)
	if err != nil {
		t.Fatalf("ListByHive: %v", err)
	}
	if len(list) != 2 || total != 2 {
		t.Fatalf("ListByHive/total = %d/%d, want 2/2", len(list), total)
	}
	for _, h := range list {
		if h.HiveID != hiveID {
			t.Errorf("ListByHive leaked harvest %s from hive %s", h.ID, h.HiveID)
		}
	}
}

func TestHarvestRepository_ListByHive_Empty(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)

	list, total, err := repo.ListByHive(ctx, uuid.New(), pagination.Params{Page: 1, Limit: 20}, nil, nil, nil)
	if err != nil {
		t.Fatalf("ListByHive: %v", err)
	}
	if len(list) != 0 || total != 0 {
		t.Fatalf("ListByHive/total = %d/%d, want empty", len(list), total)
	}
}

// TestHarvestRepository_ListByHive_OrderedByHarvestedAtDescWithIDTiebreak
// proves the ordering contract: harvested_at DESC, with id DESC as a
// stable secondary sort for equal harvested_at values.
func TestHarvestRepository_ListByHive_OrderedByHarvestedAtDescWithIDTiebreak(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()

	older := harvest.New(hiveID, harvest.ProductHoney, 1, harvest.UnitKilogram, testHarvestedAt)
	newer := harvest.New(hiveID, harvest.ProductHoney, 2, harvest.UnitKilogram, testHarvestedAt.AddDate(0, 0, 5))
	// Same harvested_at as newer, to exercise the id DESC tiebreak.
	sameDate := harvest.New(hiveID, harvest.ProductWax, 3, harvest.UnitGram, testHarvestedAt.AddDate(0, 0, 5))

	for _, h := range []*harvest.Harvest{older, newer, sameDate} {
		if err := repo.Create(ctx, h); err != nil {
			t.Fatalf("create %v: %v", h, err)
		}
	}

	list, _, err := repo.ListByHive(ctx, hiveID, pagination.Params{Page: 1, Limit: 20}, nil, nil, nil)
	if err != nil {
		t.Fatalf("ListByHive: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("ListByHive returned %d harvests, want 3", len(list))
	}

	// The two harvests sharing harvested_at come first (order between
	// them determined by id DESC), then the older one last.
	wantFirstTwo := map[uuid.UUID]bool{newer.ID: true, sameDate.ID: true}
	if !wantFirstTwo[list[0].ID] || !wantFirstTwo[list[1].ID] {
		t.Fatalf("ListByHive[0:2] = %v, want the two harvests sharing the latest harvested_at", list[0:2])
	}
	if list[0].ID.String() < list[1].ID.String() {
		t.Errorf("tie between equal harvested_at not broken by id DESC: %s before %s", list[0].ID, list[1].ID)
	}
	if list[2].ID != older.ID {
		t.Errorf("ListByHive[2] = %s, want the oldest harvest %s last", list[2].ID, older.ID)
	}
}

// TestHarvestRepository_ListByHive_Pagination proves LIMIT/OFFSET and the
// total count behave correctly across pages.
func TestHarvestRepository_ListByHive_Pagination(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()

	for i := 0; i < 5; i++ {
		h := harvest.New(hiveID, harvest.ProductHoney, float64(i), harvest.UnitKilogram, testHarvestedAt.AddDate(0, 0, i))
		if err := repo.Create(ctx, h); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	page1, total, err := repo.ListByHive(ctx, hiveID, pagination.Params{Page: 1, Limit: 2}, nil, nil, nil)
	if err != nil {
		t.Fatalf("ListByHive page 1: %v", err)
	}
	if len(page1) != 2 || total != 5 {
		t.Fatalf("page 1/total = %d/%d, want 2/5", len(page1), total)
	}

	page3, total, err := repo.ListByHive(ctx, hiveID, pagination.Params{Page: 3, Limit: 2}, nil, nil, nil)
	if err != nil {
		t.Fatalf("ListByHive page 3: %v", err)
	}
	if len(page3) != 1 || total != 5 {
		t.Fatalf("page 3/total = %d/%d, want 1/5", len(page3), total)
	}
}

// TestHarvestRepository_ListByHive_ProductFilter proves the product
// filter restricts to an exact match.
func TestHarvestRepository_ListByHive_ProductFilter(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()

	if err := repo.Create(ctx, harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram, testHarvestedAt)); err != nil {
		t.Fatalf("create honey: %v", err)
	}
	if err := repo.Create(ctx, harvest.New(hiveID, harvest.ProductPollen, 500, harvest.UnitGram, testHarvestedAt)); err != nil {
		t.Fatalf("create pollen: %v", err)
	}

	product := harvest.ProductPollen
	list, total, err := repo.ListByHive(ctx, hiveID, pagination.Params{Page: 1, Limit: 20}, &product, nil, nil)
	if err != nil {
		t.Fatalf("ListByHive: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Product != harvest.ProductPollen {
		t.Fatalf("ListByHive product=%q = %+v (total=%d), want only pollen", product, list, total)
	}
}

// TestHarvestRepository_ListByHive_AmountFilter proves each amount
// comparison operator behaves correctly.
func TestHarvestRepository_ListByHive_AmountFilter(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()

	if err := repo.Create(ctx, harvest.New(hiveID, harvest.ProductHoney, 5, harvest.UnitKilogram, testHarvestedAt)); err != nil {
		t.Fatalf("create 5kg: %v", err)
	}
	if err := repo.Create(ctx, harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram, testHarvestedAt)); err != nil {
		t.Fatalf("create 10kg: %v", err)
	}
	if err := repo.Create(ctx, harvest.New(hiveID, harvest.ProductHoney, 15, harvest.UnitKilogram, testHarvestedAt)); err != nil {
		t.Fatalf("create 15kg: %v", err)
	}

	gt := harvest.AmountOperatorGT
	ten := 10.0
	list, total, err := repo.ListByHive(ctx, hiveID, pagination.Params{Page: 1, Limit: 20}, nil, &gt, &ten)
	if err != nil {
		t.Fatalf("ListByHive gt: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Amount != 15 {
		t.Fatalf("ListByHive amount>10 = %+v (total=%d), want only 15", list, total)
	}

	lt := harvest.AmountOperatorLT
	list, total, err = repo.ListByHive(ctx, hiveID, pagination.Params{Page: 1, Limit: 20}, nil, &lt, &ten)
	if err != nil {
		t.Fatalf("ListByHive lt: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Amount != 5 {
		t.Fatalf("ListByHive amount<10 = %+v (total=%d), want only 5", list, total)
	}

	eq := harvest.AmountOperatorEQ
	list, total, err = repo.ListByHive(ctx, hiveID, pagination.Params{Page: 1, Limit: 20}, nil, &eq, &ten)
	if err != nil {
		t.Fatalf("ListByHive eq: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].Amount != 10 {
		t.Fatalf("ListByHive amount=10 = %+v (total=%d), want only 10", list, total)
	}
}

// TestHarvestRepository_ListByHive_CombinedFilters proves product,
// amount, and pagination all apply together with AND semantics.
func TestHarvestRepository_ListByHive_CombinedFilters(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()

	// Matches every filter below.
	match := harvest.New(hiveID, harvest.ProductHoney, 15, harvest.UnitKilogram, testHarvestedAt)
	// Right product, wrong amount.
	wrongAmount := harvest.New(hiveID, harvest.ProductHoney, 5, harvest.UnitKilogram, testHarvestedAt)
	// Right amount, wrong product.
	wrongProduct := harvest.New(hiveID, harvest.ProductWax, 15, harvest.UnitGram, testHarvestedAt)
	for _, h := range []*harvest.Harvest{match, wrongAmount, wrongProduct} {
		if err := repo.Create(ctx, h); err != nil {
			t.Fatalf("create %v: %v", h, err)
		}
	}

	product := harvest.ProductHoney
	gt := harvest.AmountOperatorGT
	ten := 10.0
	list, total, err := repo.ListByHive(ctx, hiveID, pagination.Params{Page: 1, Limit: 20}, &product, &gt, &ten)
	if err != nil {
		t.Fatalf("ListByHive combined: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != match.ID {
		t.Fatalf("ListByHive combined filters = %+v (total=%d), want only %s", list, total, match.ID)
	}
}

func TestHarvestRepository_Update(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()

	h := harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram, testHarvestedAt)
	if err := repo.Create(ctx, h); err != nil {
		t.Fatalf("Create: %v", err)
	}

	newHarvestedAt := testHarvestedAt.AddDate(0, 0, 1)
	h.Amount = 15
	h.Unit = harvest.UnitLiter
	h.HarvestedAt = newHarvestedAt
	h.UpdatedAt = time.Now().UTC()
	if err := repo.Update(ctx, h); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, err := repo.GetByID(ctx, hiveID, h.ID)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if got.Amount != 15 || got.Unit != harvest.UnitLiter {
		t.Errorf("got = %+v, want amount=15 unit=l", got)
	}
	if !got.HarvestedAt.Equal(newHarvestedAt) {
		t.Errorf("HarvestedAt = %v, want %v", got.HarvestedAt, newHarvestedAt)
	}
}

// TestHarvestRepository_Update_ToExistingProductSucceeds proves changing
// a harvest's product to one already recorded on the same hive is
// allowed now that there's no uniqueness constraint between hive and
// product.
func TestHarvestRepository_Update_ToExistingProductSucceeds(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()

	honey := harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram, testHarvestedAt)
	if err := repo.Create(ctx, honey); err != nil {
		t.Fatalf("create honey: %v", err)
	}
	pollen := harvest.New(hiveID, harvest.ProductPollen, 500, harvest.UnitGram, testHarvestedAt)
	if err := repo.Create(ctx, pollen); err != nil {
		t.Fatalf("create pollen: %v", err)
	}

	honey.Product = harvest.ProductPollen
	honey.Unit = harvest.UnitGram
	if err := repo.Update(ctx, honey); err != nil {
		t.Fatalf("Update to existing product: %v", err)
	}
}

func TestHarvestRepository_Update_WrongHive_NotFoundAndUnchanged(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveA := uuid.New()
	hiveB := uuid.New()

	h := harvest.New(hiveA, harvest.ProductHoney, 10, harvest.UnitKilogram, testHarvestedAt)
	if err := repo.Create(ctx, h); err != nil {
		t.Fatalf("Create: %v", err)
	}

	hijack := *h
	hijack.HiveID = hiveB
	hijack.Amount = 999
	if err := repo.Update(ctx, &hijack); !errors.Is(err, harvest.ErrNotFound) {
		t.Fatalf("Update via wrong hive: got %v, want ErrNotFound", err)
	}

	got, err := repo.GetByID(ctx, hiveA, h.ID)
	if err != nil {
		t.Fatalf("GetByID after failed hijack: %v", err)
	}
	if got.Amount != 10 {
		t.Errorf("Amount = %v after failed hijack, want unchanged 10", got.Amount)
	}
}

func TestHarvestRepository_Delete(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()

	h := harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram, testHarvestedAt)
	if err := repo.Create(ctx, h); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, hiveID, h.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := repo.GetByID(ctx, hiveID, h.ID); !errors.Is(err, harvest.ErrNotFound) {
		t.Fatalf("GetByID after delete: got %v, want ErrNotFound", err)
	}
}

// TestHarvestRepository_Delete_PreservesOtherHarvests proves deleting one
// harvest record doesn't affect siblings on the same hive.
func TestHarvestRepository_Delete_PreservesOtherHarvests(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()

	honey := harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram, testHarvestedAt)
	wax := harvest.New(hiveID, harvest.ProductWax, 800, harvest.UnitGram, testHarvestedAt)
	if err := repo.Create(ctx, honey); err != nil {
		t.Fatalf("create honey: %v", err)
	}
	if err := repo.Create(ctx, wax); err != nil {
		t.Fatalf("create wax: %v", err)
	}

	if err := repo.Delete(ctx, hiveID, honey.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := repo.GetByID(ctx, hiveID, wax.ID); err != nil {
		t.Fatalf("sibling harvest should survive: %v", err)
	}
}

func TestHarvestRepository_Delete_WrongHive_NotFoundAndNotDeleted(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveA := uuid.New()
	hiveB := uuid.New()

	h := harvest.New(hiveA, harvest.ProductHoney, 10, harvest.UnitKilogram, testHarvestedAt)
	if err := repo.Create(ctx, h); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, hiveB, h.ID); !errors.Is(err, harvest.ErrNotFound) {
		t.Fatalf("Delete via wrong hive: got %v, want ErrNotFound", err)
	}

	if _, err := repo.GetByID(ctx, hiveA, h.ID); err != nil {
		t.Fatalf("harvest should survive a failed delete attempt: %v", err)
	}
}
