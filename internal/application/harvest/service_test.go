package harvest_test

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/sbezhuk/beebase-common/pagination"
	appharvest "github.com/sbezhuk/beebase-harvest-service/internal/application/harvest"
	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
)

// --- in-memory fake harvest repository ---

type fakeHarvestRepo struct {
	mu   sync.Mutex
	byID map[uuid.UUID]*harvest.Harvest
}

func newFakeHarvestRepo() *fakeHarvestRepo {
	return &fakeHarvestRepo{byID: map[uuid.UUID]*harvest.Harvest{}}
}

func (f *fakeHarvestRepo) Create(_ context.Context, h *harvest.Harvest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *h
	f.byID[h.ID] = &cp
	return nil
}

func (f *fakeHarvestRepo) GetByID(_ context.Context, hiveID, harvestID uuid.UUID) (*harvest.Harvest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.byID[harvestID]
	if !ok || h.HiveID != hiveID {
		return nil, harvest.ErrNotFound
	}
	cp := *h
	return &cp, nil
}

func (f *fakeHarvestRepo) ListByHive(_ context.Context, hiveID uuid.UUID, p pagination.Params, search *string, product *harvest.Product, amountOperator *harvest.AmountOperator, amount *float64) ([]*harvest.Harvest, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []*harvest.Harvest
	for _, h := range f.byID {
		if h.HiveID != hiveID {
			continue
		}
		if product != nil && h.Product != *product {
			continue
		}
		if amountOperator != nil && amount != nil {
			switch *amountOperator {
			case harvest.AmountOperatorGT:
				if !(h.Amount > *amount) {
					continue
				}
			case harvest.AmountOperatorLT:
				if !(h.Amount < *amount) {
					continue
				}
			case harvest.AmountOperatorEQ:
				if h.Amount != *amount {
					continue
				}
			}
		}
		if search != nil {
			needle := strings.ToLower(*search)
			if !strings.Contains(strings.ToLower(string(h.Product)), needle) {
				continue
			}
		}
		cp := *h
		all = append(all, &cp)
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].HarvestedAt.Equal(all[j].HarvestedAt) {
			return all[i].HarvestedAt.After(all[j].HarvestedAt)
		}
		return all[i].ID.String() > all[j].ID.String()
	})

	total := len(all)
	start := p.Offset()
	if start > total {
		start = total
	}
	end := start + p.Limit
	if end > total {
		end = total
	}
	return all[start:end], total, nil
}

func (f *fakeHarvestRepo) Update(_ context.Context, h *harvest.Harvest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.byID[h.ID]
	if !ok || existing.HiveID != h.HiveID {
		return harvest.ErrNotFound
	}
	cp := *h
	f.byID[h.ID] = &cp
	return nil
}

func (f *fakeHarvestRepo) Delete(_ context.Context, hiveID, harvestID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.byID[harvestID]
	if !ok || h.HiveID != hiveID {
		return harvest.ErrNotFound
	}
	delete(f.byID, harvestID)
	return nil
}

// --- fake hive verifier ---

// fakeHiveVerifier simulates hive-service: a set of (token, hiveID) pairs
// are "owned", everything else is rejected exactly like a 404 from the
// real service would be.
type fakeHiveVerifier struct {
	owned map[string]uuid.UUID // token -> the one hive it owns
}

func newFakeHiveVerifier() *fakeHiveVerifier {
	return &fakeHiveVerifier{owned: map[string]uuid.UUID{}}
}

func (f *fakeHiveVerifier) allow(token string, hiveID uuid.UUID) {
	f.owned[token] = hiveID
}

func (f *fakeHiveVerifier) Verify(_ context.Context, accessToken string, hiveID uuid.UUID) error {
	if owned, ok := f.owned[accessToken]; ok && owned == hiveID {
		return nil
	}
	return appharvest.ErrHiveNotFound
}

// --- tests ---

var testHarvestedAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func TestCreate_Success(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "owner-token"
	verifier.allow(token, hiveID)

	h, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 12.5, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if h.HiveID != hiveID {
		t.Errorf("HiveID = %s, want %s", h.HiveID, hiveID)
	}
	if h.Product != harvest.ProductHoney || h.Amount != 12.5 || h.Unit != harvest.UnitKilogram {
		t.Errorf("h = %+v, want HONEY 12.5 kg", h)
	}
	if !h.HarvestedAt.Equal(testHarvestedAt) {
		t.Errorf("HarvestedAt = %v, want %v", h.HarvestedAt, testHarvestedAt)
	}
}

// TestCreate_HiveNotOwnedByCaller is the core security guarantee: a
// harvest can't be created under a hive the caller doesn't own, even if
// they know its ID.
func TestCreate_HiveNotOwnedByCaller(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	someoneElsesHive := uuid.New()
	// Deliberately not calling verifier.allow for this token/hive pair.

	_, err := svc.Create(context.Background(), "attacker-token", someoneElsesHive, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 1, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if !errors.Is(err, appharvest.ErrHiveNotFound) {
		t.Fatalf("Create under unowned hive: got %v, want ErrHiveNotFound", err)
	}
}

// TestCreate_MultipleRecordsForSameProduct proves a hive can carry
// several harvest records for the same product, each a separate harvest
// event distinguished by HarvestedAt.
func TestCreate_MultipleRecordsForSameProduct(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	first, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 10, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	second, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 7, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt.AddDate(0, 0, 17),
	})
	if err != nil {
		t.Fatalf("second Create for same product: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("second Create returned the same ID as the first")
	}

	list, total, err := svc.List(context.Background(), token, hiveID, pagination.Params{Page: 1, Limit: 20}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("List/total = %d/%d, want 2/2", len(list), total)
	}
}

func TestGet_Success(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	created, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 10, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := svc.Get(context.Background(), token, hiveID, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("Get returned %s, want %s", got.ID, created.ID)
	}
}

func TestGet_HiveNotOwnedByCaller(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	owner := "owner-token"
	other := "other-token"
	hiveID := uuid.New()
	verifier.allow(owner, hiveID)

	created, err := svc.Create(context.Background(), owner, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 10, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = svc.Get(context.Background(), other, hiveID, created.ID)
	if !errors.Is(err, appharvest.ErrHiveNotFound) {
		t.Fatalf("Get by non-owner: got %v, want ErrHiveNotFound", err)
	}
}

func TestGet_HarvestNotFound(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	_, err := svc.Get(context.Background(), token, hiveID, uuid.New())
	if !errors.Is(err, harvest.ErrNotFound) {
		t.Fatalf("Get with unknown id: got %v, want ErrNotFound", err)
	}
}

func TestList_ReturnsEveryHarvestForTheHive(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	if _, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 10, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	}); err != nil {
		t.Fatalf("create honey: %v", err)
	}
	if _, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductPollen, Amount: 500, Unit: harvest.UnitGram, HarvestedAt: testHarvestedAt,
	}); err != nil {
		t.Fatalf("create pollen: %v", err)
	}

	list, total, err := svc.List(context.Background(), token, hiveID, pagination.Params{Page: 1, Limit: 20}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 || total != 2 {
		t.Fatalf("List/total = %d/%d, want 2/2", len(list), total)
	}
}

func TestList_NoHarvests_ReturnsEmpty(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	list, total, err := svc.List(context.Background(), token, hiveID, pagination.Params{Page: 1, Limit: 20}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 || total != 0 {
		t.Fatalf("List/total = %d/%d, want empty", len(list), total)
	}
}

func TestList_HiveNotOwnedByCaller(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)

	_, _, err := svc.List(context.Background(), "some-token", uuid.New(), pagination.Params{Page: 1, Limit: 20}, nil, nil, nil, nil)
	if !errors.Is(err, appharvest.ErrHiveNotFound) {
		t.Fatalf("List for unowned hive: got %v, want ErrHiveNotFound", err)
	}
}

// TestList_Pagination proves paging is forwarded to the repository and
// the total reflects every matching record, not just the page returned.
func TestList_Pagination(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	for i := 0; i < 3; i++ {
		if _, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
			Product: harvest.ProductHoney, Amount: float64(i), Unit: harvest.UnitKilogram,
			HarvestedAt: testHarvestedAt.AddDate(0, 0, i),
		}); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}

	page, total, err := svc.List(context.Background(), token, hiveID, pagination.Params{Page: 1, Limit: 2}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if len(page) != 2 || total != 3 {
		t.Fatalf("List page 1/total = %d/%d, want 2/3", len(page), total)
	}

	page, total, err = svc.List(context.Background(), token, hiveID, pagination.Params{Page: 2, Limit: 2}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if len(page) != 1 || total != 3 {
		t.Fatalf("List page 2/total = %d/%d, want 1/3", len(page), total)
	}
}

func TestUpdate_Success(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	created, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 10, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newHarvestedAt := testHarvestedAt.AddDate(0, 0, 1)
	updated, err := svc.Update(context.Background(), token, hiveID, created.ID, appharvest.UpdateInput{
		Product: harvest.ProductHoney, Amount: 15, Unit: harvest.UnitKilogram, HarvestedAt: newHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Amount != 15 {
		t.Errorf("Amount = %v, want 15", updated.Amount)
	}
	if !updated.HarvestedAt.Equal(newHarvestedAt) {
		t.Errorf("HarvestedAt = %v, want %v", updated.HarvestedAt, newHarvestedAt)
	}
	if updated.UpdatedAt.Before(created.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want >= %v", updated.UpdatedAt, created.UpdatedAt)
	}
}

// TestUpdate_ToExistingProductSucceeds proves changing a harvest's
// product to one already recorded on the same hive is allowed - there is
// no uniqueness constraint between hive and product.
func TestUpdate_ToExistingProductSucceeds(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	honey, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 10, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("create honey: %v", err)
	}
	if _, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductPollen, Amount: 500, Unit: harvest.UnitGram, HarvestedAt: testHarvestedAt,
	}); err != nil {
		t.Fatalf("create pollen: %v", err)
	}

	updated, err := svc.Update(context.Background(), token, hiveID, honey.ID, appharvest.UpdateInput{
		Product: harvest.ProductPollen, Amount: 10, Unit: harvest.UnitGram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Update to existing product: %v", err)
	}
	if updated.Product != harvest.ProductPollen {
		t.Errorf("Product = %v, want POLLEN", updated.Product)
	}
}

func TestUpdate_WrongHive_ReturnsNotFound(t *testing.T) {
	verifier := newFakeHiveVerifier()
	repo := newFakeHarvestRepo()
	svc := appharvest.NewService(repo, verifier)
	hiveA := uuid.New()
	hiveB := uuid.New()
	tokenA := "token-a"
	tokenB := "token-b"
	verifier.allow(tokenA, hiveA)
	verifier.allow(tokenB, hiveB)

	created, err := svc.Create(context.Background(), tokenA, hiveA, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 10, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = svc.Update(context.Background(), tokenB, hiveB, created.ID, appharvest.UpdateInput{
		Product: harvest.ProductHoney, Amount: 5, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if !errors.Is(err, harvest.ErrNotFound) {
		t.Fatalf("Update via wrong hive: got %v, want ErrNotFound", err)
	}
}

func TestUpdate_HiveNotOwnedByCaller(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	owner := "owner-token"
	other := "other-token"
	hiveID := uuid.New()
	verifier.allow(owner, hiveID)

	created, err := svc.Create(context.Background(), owner, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 10, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = svc.Update(context.Background(), other, hiveID, created.ID, appharvest.UpdateInput{
		Product: harvest.ProductHoney, Amount: 999, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if !errors.Is(err, appharvest.ErrHiveNotFound) {
		t.Fatalf("Update by non-owner: got %v, want ErrHiveNotFound", err)
	}

	got, err := svc.Get(context.Background(), owner, hiveID, created.ID)
	if err != nil {
		t.Fatalf("Get after failed hijack attempt: %v", err)
	}
	if got.Amount != 10 {
		t.Errorf("Amount = %v after failed hijack attempt, want unchanged 10", got.Amount)
	}
}

func TestDelete_Success(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	created, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 10, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(context.Background(), token, hiveID, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := svc.Get(context.Background(), token, hiveID, created.ID); !errors.Is(err, harvest.ErrNotFound) {
		t.Fatalf("Get after Delete: got %v, want ErrNotFound", err)
	}
}

// TestDelete_PreservesOtherHarvestsOnSameHive proves deleting one harvest
// doesn't affect siblings under the same hive.
func TestDelete_PreservesOtherHarvestsOnSameHive(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	hiveID := uuid.New()
	token := "token"
	verifier.allow(token, hiveID)

	honey, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 10, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("create honey: %v", err)
	}
	if _, err := svc.Create(context.Background(), token, hiveID, appharvest.CreateInput{
		Product: harvest.ProductWax, Amount: 800, Unit: harvest.UnitGram, HarvestedAt: testHarvestedAt,
	}); err != nil {
		t.Fatalf("create wax: %v", err)
	}

	if err := svc.Delete(context.Background(), token, hiveID, honey.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	list, _, err := svc.List(context.Background(), token, hiveID, pagination.Params{Page: 1, Limit: 20}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Product != harvest.ProductWax {
		t.Fatalf("List after deleting honey = %+v, want only wax", list)
	}
}

func TestDelete_HiveNotOwnedByCaller(t *testing.T) {
	verifier := newFakeHiveVerifier()
	svc := appharvest.NewService(newFakeHarvestRepo(), verifier)
	owner := "owner-token"
	other := "other-token"
	hiveID := uuid.New()
	verifier.allow(owner, hiveID)

	created, err := svc.Create(context.Background(), owner, hiveID, appharvest.CreateInput{
		Product: harvest.ProductHoney, Amount: 10, Unit: harvest.UnitKilogram, HarvestedAt: testHarvestedAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(context.Background(), other, hiveID, created.ID); !errors.Is(err, appharvest.ErrHiveNotFound) {
		t.Fatalf("Delete by non-owner: got %v, want ErrHiveNotFound", err)
	}

	list, _, err := svc.List(context.Background(), owner, hiveID, pagination.Params{Page: 1, Limit: 20}, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("harvest deleted by non-owner attempt: %v", list)
	}
}
