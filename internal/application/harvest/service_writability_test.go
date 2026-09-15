package harvest_test

// This file covers transitive parent-hive writability enforcement: a
// harvest can only be created or updated when hive-service currently
// reports its hive as writable. See application/harvest.Service.Create
// and Service.Update, and HiveVerifier.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	appharvest "github.com/sbezhuk/beebase-harvest-service/internal/application/harvest"
	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
)

// TestCreate_ReadOnlyHive_Rejected proves creating a harvest under a hive
// hive-service reports as read-only is rejected with ErrHiveReadOnly,
// distinct from ErrHiveNotFound.
func TestCreate_ReadOnlyHive_Rejected(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)
	verifier.lock(hiveID)

	_, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 1, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if !errors.Is(err, appharvest.ErrHiveReadOnly) {
		t.Fatalf("Create under a read-only hive: got %v, want ErrHiveReadOnly", err)
	}
}

// TestUpdate_ReadOnlyHive_Rejected proves Update re-verifies the hive on
// every call (this service always has, per its own architecture - see
// HiveVerifier's doc comment) and now rejects a hive that has since
// become read-only, leaving the harvest's stored fields untouched.
func TestUpdate_ReadOnlyHive_Rejected(t *testing.T) {
	verifier := newFakeHiveVerifier()
	repo := newFakeHarvestRepo()
	svc := appharvest.NewService(repo, verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	created, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 5, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	verifier.lock(hiveID)

	_, err = svc.Update(context.Background(), token, hiveID, created.ID, appharvest.UpdateInput{
		Product: harvest.ProductHoney, Amount: 99, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if !errors.Is(err, appharvest.ErrHiveReadOnly) {
		t.Fatalf("Update under a now-read-only hive: got %v, want ErrHiveReadOnly", err)
	}

	got, err := svc.Get(context.Background(), token, hiveID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Amount != 5 {
		t.Errorf("Amount = %v after rejected update, want unchanged 5", got.Amount)
	}
}

// TestUpdate_HiveBecomesWritableAgain_UpdateSucceeds proves a promoted or
// re-upgraded hive immediately unblocks harvest updates again.
func TestUpdate_HiveBecomesWritableAgain_UpdateSucceeds(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	created, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 5, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	verifier.lock(hiveID)

	_, err = svc.Update(context.Background(), token, hiveID, created.ID, appharvest.UpdateInput{
		Product: harvest.ProductHoney, Amount: 6, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if !errors.Is(err, appharvest.ErrHiveReadOnly) {
		t.Fatalf("Update while locked: got %v, want ErrHiveReadOnly", err)
	}

	verifier.readOnly[hiveID] = false // re-upgrade / promotion

	if _, err := svc.Update(context.Background(), token, hiveID, created.ID, appharvest.UpdateInput{
		Product: harvest.ProductHoney, Amount: 7, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	}); err != nil {
		t.Fatalf("Update after hive becomes writable again: %v", err)
	}
}

// TestDelete_ReadOnlyHive_StillAllowed proves delete is never gated by
// hive writability.
func TestDelete_ReadOnlyHive_StillAllowed(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	created, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 5, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	verifier.lock(hiveID)

	if err := svc.Delete(context.Background(), token, hiveID, created.ID); err != nil {
		t.Fatalf("Delete under a read-only hive should still be allowed: %v", err)
	}
}

// TestGet_ReadOnlyHive_StillReadable and TestList_ReadOnlyHive_StillReadable
// prove reads are never gated by writability - only creates/updates are.
func TestGet_ReadOnlyHive_StillReadable(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	created, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 5, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	verifier.lock(hiveID)

	if _, err := svc.Get(context.Background(), token, hiveID, created.ID); err != nil {
		t.Fatalf("Get under a read-only hive should still succeed: %v", err)
	}
}
