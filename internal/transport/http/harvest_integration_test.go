//go:build integration

package http_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	appharvest "github.com/sbezhuk/beebase-harvest-service/internal/application/harvest"
	"github.com/sbezhuk/beebase-harvest-service/internal/platform/hiveclient"
	repopostgres "github.com/sbezhuk/beebase-harvest-service/internal/repository/postgres"
	transporthttp "github.com/sbezhuk/beebase-harvest-service/internal/transport/http"
	harvesthttp "github.com/sbezhuk/beebase-harvest-service/internal/transport/http/harvest"

	"github.com/sbezhuk/beebase-common/authmw"
	"github.com/sbezhuk/beebase-common/jwks"
	"github.com/sbezhuk/beebase-common/logger"
)

const testKID = "test-kid"

// alwaysActiveSessionChecker is a stand-in for *sessionstore.Store: these
// integration tests mint tokens directly (see tokenFor) rather than going
// through auth-service's real session-issuing flow, so there's no actual
// session to track here.
type alwaysActiveSessionChecker struct{}

func (alwaysActiveSessionChecker) IsActive(_ context.Context, _, _ uuid.UUID) (bool, error) {
	return true, nil
}

// fakeHiveService stands in for the real hive-service: it owns exactly
// one hive per bearer token registered via allow, and answers
// GET /api/v1/hives/{id} exactly like the real service would - 200 if the
// presented token's owner owns that hive, 404 otherwise - so this test
// exercises harvest-service's real cross-service HTTP call without
// needing a second full service running.
type fakeHiveService struct {
	mu    sync.Mutex
	owned map[string]uuid.UUID // "Bearer <token>" -> the one hive it owns
}

func newFakeHiveService() *fakeHiveService {
	return &fakeHiveService{owned: map[string]uuid.UUID{}}
}

func (f *fakeHiveService) allow(token string, hiveID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owned["Bearer "+token] = hiveID
}

func (f *fakeHiveService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	owned, ok := f.owned[r.Header.Get("Authorization")]
	f.mu.Unlock()

	hiveID, err := uuid.Parse(strings.TrimPrefix(r.URL.Path, "/api/v1/hives/"))
	if err != nil || !ok || owned != hiveID {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

type testStack struct {
	server *httptest.Server
	hive   *fakeHiveService
	priv   ed25519.PrivateKey
}

// newTestStack wires a full router against a real PostgreSQL database
// (every write scoped to a transaction rolled back at the end of the
// test), a real JWKS server, and a fake hive-service - exactly mirroring
// how harvest-service verifies tokens and hive ownership in production,
// just with throwaway stand-ins instead of the real services.
func newTestStack(t *testing.T) *testStack {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping HTTP harvest integration test")
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	jwksHandler, err := jwks.NewHandler(pub, testKID)
	if err != nil {
		t.Fatalf("jwks.NewHandler: %v", err)
	}
	jwksServer := httptest.NewServer(jwksHandler)
	t.Cleanup(jwksServer.Close)

	verifier, err := authmw.NewVerifierFromJWKSURL(context.Background(), jwksServer.URL, alwaysActiveSessionChecker{})
	if err != nil {
		t.Fatalf("NewVerifierFromJWKSURL: %v", err)
	}

	hive := newFakeHiveService()
	hiveServer := httptest.NewServer(hive)
	t.Cleanup(hiveServer.Close)

	harvestRepo := repopostgres.NewHarvestRepository(tx)
	hiveVerifier := hiveclient.New(hiveServer.URL)
	harvestService := appharvest.NewService(harvestRepo, hiveVerifier)
	log := logger.New("development", "error")
	handler := harvesthttp.NewHandler(harvestService, log)

	router := transporthttp.NewRouter(log, pool, handler, verifier)

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	return &testStack{server: srv, hive: hive, priv: priv}
}

func (s *testStack) tokenFor(t *testing.T, userID uuid.UUID) string {
	t.Helper()

	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = testKID

	signed, err := token.SignedString(s.priv)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func (s *testStack) request(t *testing.T, method, path, token string, body any) *http.Response {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(buf)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, s.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()

	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func TestHarvestFlow_CreateListGetUpdateDelete(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	// Create
	resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
		"product": "HONEY", "amount": 12.5, "unit": "kg",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var created harvesthttp.Response
	decodeJSON(t, resp, &created)
	if created.Product != "HONEY" || created.Amount != 12.5 || created.Unit != "kg" {
		t.Fatalf("create: got %+v, want HONEY 12.5 kg", created)
	}
	if created.HiveID != hiveID {
		t.Fatalf("create: hive_id = %s, want %s", created.HiveID, hiveID)
	}

	// Get
	resp = stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest/"+created.ID.String(), token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var fetched harvesthttp.Response
	decodeJSON(t, resp, &fetched)
	if fetched.ID != created.ID {
		t.Fatalf("get: id = %s, want %s", fetched.ID, created.ID)
	}

	// List
	resp = stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var list []harvesthttp.Response
	decodeJSON(t, resp, &list)
	if len(list) != 1 {
		t.Fatalf("list: got %d harvests, want 1", len(list))
	}

	// Update
	resp = stack.request(t, http.MethodPut, "/api/v1/hives/"+hiveID.String()+"/harvest/"+created.ID.String(), token, map[string]any{
		"product": "HONEY", "amount": 15, "unit": "l",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var updated harvesthttp.Response
	decodeJSON(t, resp, &updated)
	if updated.Amount != 15 || updated.Unit != "l" {
		t.Fatalf("update: got %+v, want amount=15 unit=l", updated)
	}

	// Delete
	resp = stack.request(t, http.MethodDelete, "/api/v1/hives/"+hiveID.String()+"/harvest/"+created.ID.String(), token, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	resp = stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest/"+created.ID.String(), token, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete: status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// TestHarvestFlow_EmptyListForHiveWithNoHarvests proves GET returns "[]",
// not "null" or an error, for a hive that simply has no harvest records
// yet.
func TestHarvestFlow_EmptyListForHiveWithNoHarvests(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	resp := stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var list []harvesthttp.Response
	decodeJSON(t, resp, &list)
	if list == nil || len(list) != 0 {
		t.Fatalf("list = %v, want a non-nil empty array", list)
	}
}

// TestHarvestFlow_HarvestWithoutAnyInspection proves harvest works
// against a hive with no dependency on inspection-service whatsoever -
// harvest is now an independent domain (User -> Apiary -> Hive ->
// Harvest), not nested under an inspection.
func TestHarvestFlow_HarvestWithoutAnyInspection(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
		"product": "WAX", "amount": 800, "unit": "g",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
}

// TestHarvestFlow_MultipleProductsOnOneHive proves a hive can carry
// several harvest records, one per product, at once.
func TestHarvestFlow_MultipleProductsOnOneHive(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	products := []map[string]any{
		{"product": "HONEY", "amount": 10, "unit": "kg"},
		{"product": "POLLEN", "amount": 500, "unit": "g"},
		{"product": "PROPOLIS", "amount": 150, "unit": "g"},
		{"product": "WAX", "amount": 800, "unit": "g"},
	}
	for _, body := range products {
		resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create %v: status = %d, want %d", body, resp.StatusCode, http.StatusCreated)
		}
	}

	resp := stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var list []harvesthttp.Response
	decodeJSON(t, resp, &list)
	if len(list) != len(products) {
		t.Fatalf("list returned %d harvests, want %d", len(list), len(products))
	}
}

// TestHarvestFlow_CreateDuplicateProductRejected proves a second harvest
// for a product already recorded on the same hive is rejected. It
// deliberately stops right after the conflict: a real unique-constraint
// violation aborts the underlying Postgres transaction this test stack
// shares across every request (see newTestStack), so any further query in
// the same test would itself fail regardless of the application logic
// being exercised.
func TestHarvestFlow_CreateDuplicateProductRejected(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
		"product": "HONEY", "amount": 10, "unit": "kg",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create honey: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	resp = stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
		"product": "HONEY", "amount": 5, "unit": "kg",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("create duplicate honey: status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
}

// TestHarvestFlow_UpdateToExistingProductRejected proves changing a
// harvest's product to one already recorded on the same hive is rejected
// too - the UNIQUE constraint applies to updates, not just inserts. Uses
// its own stack for the same reason described on
// TestHarvestFlow_CreateDuplicateProductRejected.
func TestHarvestFlow_UpdateToExistingProductRejected(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
		"product": "HONEY", "amount": 10, "unit": "kg",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create honey: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var honey harvesthttp.Response
	decodeJSON(t, resp, &honey)

	resp = stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
		"product": "POLLEN", "amount": 500, "unit": "g",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create pollen: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	resp = stack.request(t, http.MethodPut, "/api/v1/hives/"+hiveID.String()+"/harvest/"+honey.ID.String(), token, map[string]any{
		"product": "POLLEN", "amount": 100, "unit": "g",
	})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("update to colliding product: status = %d, want %d", resp.StatusCode, http.StatusConflict)
	}
}

// TestHarvestFlow_ValidationErrors covers rejected product/unit/amount
// combinations, mirroring the spec's validation matrix.
func TestHarvestFlow_ValidationErrors(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	cases := []map[string]any{
		{"product": "SWARM", "amount": 1, "unit": "kg"},    // invalid product
		{"amount": 1, "unit": "kg"},                        // missing product
		{"product": "HONEY", "amount": -1, "unit": "kg"},   // negative amount
		{"product": "HONEY", "unit": "kg"},                 // missing amount
		{"product": "HONEY", "amount": 1, "unit": "ml"},    // invalid unit
		{"product": "HONEY", "amount": 1},                  // missing unit
		{"product": "HONEY", "amount": 1, "unit": "g"},     // invalid combination
		{"product": "POLLEN", "amount": 1, "unit": "l"},    // invalid combination
		{"product": "PROPOLIS", "amount": 1, "unit": "kg"}, // invalid combination
		{"product": "WAX", "amount": 1, "unit": "kg"},      // invalid combination
	}
	for _, body := range cases {
		resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("create %v: status = %d, want %d", body, resp.StatusCode, http.StatusBadRequest)
		}
	}

	// A zero amount is explicitly allowed.
	resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
		"product": "HONEY", "amount": 0, "unit": "kg",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create with zero amount: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
}

// TestHarvestFlow_CannotAccessAnotherUsersHarvest is the end-to-end proof
// that harvest reuses hive ownership: a different user gets 404 on every
// operation, and the owner's data is untouched afterward.
func TestHarvestFlow_CannotAccessAnotherUsersHarvest(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	ownerToken := stack.tokenFor(t, uuid.New())
	otherToken := stack.tokenFor(t, uuid.New())
	stack.hive.allow(ownerToken, hiveID)

	resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", ownerToken, map[string]any{
		"product": "HONEY", "amount": 10, "unit": "kg",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var created harvesthttp.Response
	decodeJSON(t, resp, &created)

	// Cannot create.
	resp = stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", otherToken, map[string]any{
		"product": "POLLEN", "amount": 500, "unit": "g",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("create as different user: status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	// Cannot get.
	resp = stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest/"+created.ID.String(), otherToken, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("get as different user: status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	// Cannot list.
	resp = stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest", otherToken, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("list as different user: status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	// Cannot update.
	resp = stack.request(t, http.MethodPut, "/api/v1/hives/"+hiveID.String()+"/harvest/"+created.ID.String(), otherToken, map[string]any{
		"product": "HONEY", "amount": 999, "unit": "kg",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("update as different user: status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	// Cannot delete.
	resp = stack.request(t, http.MethodDelete, "/api/v1/hives/"+hiveID.String()+"/harvest/"+created.ID.String(), otherToken, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("delete as different user: status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	// The owner's harvest survived every attempt untouched.
	resp = stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest", ownerToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("owner list after attacks: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var list []harvesthttp.Response
	decodeJSON(t, resp, &list)
	if len(list) != 1 || list[0].Amount != 10 {
		t.Fatalf("owner's harvest changed after other user's attempts: %+v", list)
	}
}

// TestHarvestFlow_UpdateVerifiesHarvestBelongsToHive proves the endpoint
// checks that harvestId actually belongs to the hiveId in the path, not
// just that the caller owns some hive with that harvest.
func TestHarvestFlow_UpdateVerifiesHarvestBelongsToHive(t *testing.T) {
	stack := newTestStack(t)
	hiveA := uuid.New()
	hiveB := uuid.New()
	tokenA := stack.tokenFor(t, uuid.New())
	tokenB := stack.tokenFor(t, uuid.New())
	stack.hive.allow(tokenA, hiveA)
	stack.hive.allow(tokenB, hiveB)

	resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveA.String()+"/harvest", tokenA, map[string]any{
		"product": "HONEY", "amount": 10, "unit": "kg",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create in hive A: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var created harvesthttp.Response
	decodeJSON(t, resp, &created)

	// tokenB owns hiveB, not hiveA, so this must fail with hive_not_found
	// (checked first) rather than leaking anything about hiveA's harvest.
	resp = stack.request(t, http.MethodPut, "/api/v1/hives/"+hiveB.String()+"/harvest/"+created.ID.String(), tokenB, map[string]any{
		"product": "HONEY", "amount": 999, "unit": "kg",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("update via wrong hive (different owner): status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	// Same owner, but the wrong hive in the path: the hive check passes
	// (tokenA does own hiveB too, once allowed) yet the harvest doesn't
	// belong to hiveB, so it's harvest_not_found.
	stack.hive.allow(tokenA, hiveB)
	resp = stack.request(t, http.MethodPut, "/api/v1/hives/"+hiveB.String()+"/harvest/"+created.ID.String(), tokenA, map[string]any{
		"product": "HONEY", "amount": 999, "unit": "kg",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("update via wrong hive (same owner): status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// TestHarvestFlow_InvalidIDs covers malformed hiveId/harvestId path
// segments.
func TestHarvestFlow_InvalidIDs(t *testing.T) {
	stack := newTestStack(t)
	token := stack.tokenFor(t, uuid.New())

	resp := stack.request(t, http.MethodGet, "/api/v1/hives/not-a-uuid/harvest", token, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("list with malformed hive id: status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	resp = stack.request(t, http.MethodPut, "/api/v1/hives/"+uuid.New().String()+"/harvest/not-a-uuid", token, map[string]any{
		"product": "HONEY", "amount": 1, "unit": "kg",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("update with malformed harvest id: status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHarvestFlow_WithoutTokenIsUnauthorized(t *testing.T) {
	stack := newTestStack(t)

	resp := stack.request(t, http.MethodGet, "/api/v1/hives/"+uuid.New().String()+"/harvest", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("list without token: status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}
