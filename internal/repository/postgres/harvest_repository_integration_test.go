//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
	repopostgres "github.com/sbezhuk/beebase-harvest-service/internal/repository/postgres"
)

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

	h := harvest.New(hiveID, harvest.ProductHoney, 12.5, harvest.UnitKilogram)
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

	h := harvest.New(hiveA, harvest.ProductHoney, 10, harvest.UnitKilogram)
	if err := repo.Create(ctx, h); err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = repo.GetByID(ctx, hiveB, h.ID)
	if !errors.Is(err, harvest.ErrNotFound) {
		t.Fatalf("GetByID via wrong hive: got %v, want ErrNotFound", err)
	}
}

func TestHarvestRepository_Create_DuplicateProduct(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()

	first := harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	second := harvest.New(hiveID, harvest.ProductHoney, 5, harvest.UnitKilogram)
	err = repo.Create(ctx, second)
	if !errors.Is(err, harvest.ErrDuplicateProduct) {
		t.Fatalf("Create with duplicate product: got %v, want ErrDuplicateProduct", err)
	}
}

// TestHarvestRepository_DuplicateProduct_AllowedAcrossDifferentHives
// proves the UNIQUE constraint is scoped per hive, not global.
func TestHarvestRepository_DuplicateProduct_AllowedAcrossDifferentHives(t *testing.T) {
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

	if err := repo.Create(ctx, harvest.New(hiveA, harvest.ProductHoney, 10, harvest.UnitKilogram)); err != nil {
		t.Fatalf("create in hiveA: %v", err)
	}
	if err := repo.Create(ctx, harvest.New(hiveB, harvest.ProductHoney, 8, harvest.UnitKilogram)); err != nil {
		t.Fatalf("create same product in hiveB: %v", err)
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

	if err := repo.Create(ctx, harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram)); err != nil {
		t.Fatalf("create honey: %v", err)
	}
	if err := repo.Create(ctx, harvest.New(hiveID, harvest.ProductPollen, 500, harvest.UnitGram)); err != nil {
		t.Fatalf("create pollen: %v", err)
	}
	if err := repo.Create(ctx, harvest.New(otherHive, harvest.ProductWax, 200, harvest.UnitGram)); err != nil {
		t.Fatalf("create wax in other hive: %v", err)
	}

	list, err := repo.ListByHive(ctx, hiveID)
	if err != nil {
		t.Fatalf("ListByHive: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListByHive returned %d harvests, want 2", len(list))
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

	list, err := repo.ListByHive(ctx, uuid.New())
	if err != nil {
		t.Fatalf("ListByHive: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("ListByHive = %v, want empty", list)
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

	h := harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram)
	if err := repo.Create(ctx, h); err != nil {
		t.Fatalf("Create: %v", err)
	}

	h.Amount = 15
	h.Unit = harvest.UnitLiter
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
}

func TestHarvestRepository_Update_ChangingProductToExistingOneFails(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	repo := repopostgres.NewHarvestRepository(tx)
	hiveID := uuid.New()

	honey := harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram)
	if err := repo.Create(ctx, honey); err != nil {
		t.Fatalf("create honey: %v", err)
	}
	pollen := harvest.New(hiveID, harvest.ProductPollen, 500, harvest.UnitGram)
	if err := repo.Create(ctx, pollen); err != nil {
		t.Fatalf("create pollen: %v", err)
	}

	honey.Product = harvest.ProductPollen
	honey.Unit = harvest.UnitGram
	err = repo.Update(ctx, honey)
	if !errors.Is(err, harvest.ErrDuplicateProduct) {
		t.Fatalf("Update colliding with existing product: got %v, want ErrDuplicateProduct", err)
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

	h := harvest.New(hiveA, harvest.ProductHoney, 10, harvest.UnitKilogram)
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

	h := harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram)
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

	honey := harvest.New(hiveID, harvest.ProductHoney, 10, harvest.UnitKilogram)
	wax := harvest.New(hiveID, harvest.ProductWax, 800, harvest.UnitGram)
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

	h := harvest.New(hiveA, harvest.ProductHoney, 10, harvest.UnitKilogram)
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
