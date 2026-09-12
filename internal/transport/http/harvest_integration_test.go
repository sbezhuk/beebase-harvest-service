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
	"github.com/sbezhuk/beebase-common/pagination"
)

const testKID = "test-kid"

const testHarvestedAt = "2026-09-01T00:00:00Z"

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
		"product": "HONEY", "amount": 12.5, "unit": "kg", "harvested_at": testHarvestedAt,
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
	if !created.HarvestedAt.Equal(mustParseTime(t, testHarvestedAt)) {
		t.Fatalf("create: harvested_at = %s, want %s", created.HarvestedAt, testHarvestedAt)
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
	var list pagination.Response[harvesthttp.Response]
	decodeJSON(t, resp, &list)
	if len(list.Items) != 1 {
		t.Fatalf("list: got %d harvests, want 1", len(list.Items))
	}
	if list.Pagination.Total != 1 || list.Pagination.Page != 1 || list.Pagination.Limit != pagination.DefaultLimit {
		t.Fatalf("list: pagination = %+v, want total=1 page=1 limit=%d", list.Pagination, pagination.DefaultLimit)
	}

	// Update
	newHarvestedAt := "2026-09-05T00:00:00Z"
	resp = stack.request(t, http.MethodPut, "/api/v1/hives/"+hiveID.String()+"/harvest/"+created.ID.String(), token, map[string]any{
		"product": "HONEY", "amount": 15, "unit": "l", "harvested_at": newHarvestedAt,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var updated harvesthttp.Response
	decodeJSON(t, resp, &updated)
	if updated.Amount != 15 || updated.Unit != "l" {
		t.Fatalf("update: got %+v, want amount=15 unit=l", updated)
	}
	if !updated.HarvestedAt.Equal(mustParseTime(t, newHarvestedAt)) {
		t.Fatalf("update: harvested_at = %s, want %s", updated.HarvestedAt, newHarvestedAt)
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

// TestHarvestFlow_EmptyListForHiveWithNoHarvests proves GET returns an
// empty "items" array, not "null" or an error, for a hive that simply
// has no harvest records yet.
func TestHarvestFlow_EmptyListForHiveWithNoHarvests(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	resp := stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var list pagination.Response[harvesthttp.Response]
	decodeJSON(t, resp, &list)
	if list.Items == nil || len(list.Items) != 0 {
		t.Fatalf("list.items = %v, want a non-nil empty array", list.Items)
	}
	if list.Pagination.Total != 0 {
		t.Fatalf("list.pagination.total = %d, want 0", list.Pagination.Total)
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
		"product": "WAX", "amount": 800, "unit": "g", "harvested_at": testHarvestedAt,
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
		{"product": "HONEY", "amount": 10, "unit": "kg", "harvested_at": testHarvestedAt},
		{"product": "POLLEN", "amount": 500, "unit": "g", "harvested_at": testHarvestedAt},
		{"product": "PROPOLIS", "amount": 150, "unit": "g", "harvested_at": testHarvestedAt},
		{"product": "WAX", "amount": 800, "unit": "g", "harvested_at": testHarvestedAt},
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
	var list pagination.Response[harvesthttp.Response]
	decodeJSON(t, resp, &list)
	if len(list.Items) != len(products) {
		t.Fatalf("list returned %d harvests, want %d", len(list.Items), len(products))
	}
}

// TestHarvestFlow_MultipleRecordsForSameProductAllowed proves a hive can
// carry several harvest records for the same product - each a separate
// harvest event - now that the UNIQUE (hive_id, product) constraint is
// gone.
func TestHarvestFlow_MultipleRecordsForSameProductAllowed(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
		"product": "HONEY", "amount": 10, "unit": "kg", "harvested_at": "2026-08-15T00:00:00Z",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create first honey: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	resp = stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
		"product": "HONEY", "amount": 7, "unit": "kg", "harvested_at": "2026-09-01T00:00:00Z",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create second honey: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	resp = stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var list pagination.Response[harvesthttp.Response]
	decodeJSON(t, resp, &list)
	if len(list.Items) != 2 || list.Pagination.Total != 2 {
		t.Fatalf("list/total = %d/%d, want 2/2", len(list.Items), list.Pagination.Total)
	}
}

// TestHarvestFlow_UpdateToExistingProductSucceeds proves changing a
// harvest's product to one already recorded on the same hive is
// allowed - there is no uniqueness constraint between hive and product.
func TestHarvestFlow_UpdateToExistingProductSucceeds(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
		"product": "HONEY", "amount": 10, "unit": "kg", "harvested_at": testHarvestedAt,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create honey: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var honey harvesthttp.Response
	decodeJSON(t, resp, &honey)

	resp = stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
		"product": "POLLEN", "amount": 500, "unit": "g", "harvested_at": testHarvestedAt,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create pollen: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	resp = stack.request(t, http.MethodPut, "/api/v1/hives/"+hiveID.String()+"/harvest/"+honey.ID.String(), token, map[string]any{
		"product": "POLLEN", "amount": 100, "unit": "g", "harvested_at": testHarvestedAt,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update to existing product: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// TestHarvestFlow_ValidationErrors covers rejected product/unit/amount/
// harvested_at combinations, mirroring the spec's validation matrix.
func TestHarvestFlow_ValidationErrors(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	cases := []map[string]any{
		{"product": "SWARM", "amount": 1, "unit": "kg", "harvested_at": testHarvestedAt},    // invalid product
		{"amount": 1, "unit": "kg", "harvested_at": testHarvestedAt},                        // missing product
		{"product": "HONEY", "amount": -1, "unit": "kg", "harvested_at": testHarvestedAt},   // negative amount
		{"product": "HONEY", "unit": "kg", "harvested_at": testHarvestedAt},                 // missing amount
		{"product": "HONEY", "amount": 1, "unit": "ml", "harvested_at": testHarvestedAt},    // invalid unit
		{"product": "HONEY", "amount": 1, "harvested_at": testHarvestedAt},                  // missing unit
		{"product": "HONEY", "amount": 1, "unit": "g", "harvested_at": testHarvestedAt},     // invalid combination
		{"product": "POLLEN", "amount": 1, "unit": "l", "harvested_at": testHarvestedAt},    // invalid combination
		{"product": "PROPOLIS", "amount": 1, "unit": "kg", "harvested_at": testHarvestedAt}, // invalid combination
		{"product": "WAX", "amount": 1, "unit": "kg", "harvested_at": testHarvestedAt},      // invalid combination
		{"product": "HONEY", "amount": 1, "unit": "kg"},                                     // missing harvested_at
		{"product": "HONEY", "amount": 1, "unit": "kg", "harvested_at": "2026-09-01"},       // invalid harvested_at format
	}
	for _, body := range cases {
		resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("create %v: status = %d, want %d", body, resp.StatusCode, http.StatusBadRequest)
		}
	}

	// A zero amount is explicitly allowed.
	resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
		"product": "HONEY", "amount": 0, "unit": "kg", "harvested_at": testHarvestedAt,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create with zero amount: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
}

// TestHarvestFlow_ListPagination proves page/limit are honored and the
// response's pagination metadata reflects the full result set.
func TestHarvestFlow_ListPagination(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	dates := []string{"2026-08-01T00:00:00Z", "2026-08-15T00:00:00Z", "2026-09-01T00:00:00Z"}
	for _, d := range dates {
		resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
			"product": "HONEY", "amount": 1, "unit": "kg", "harvested_at": d,
		})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create %s: status = %d, want %d", d, resp.StatusCode, http.StatusCreated)
		}
	}

	resp := stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest?page=1&limit=2", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list page 1: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var page1 pagination.Response[harvesthttp.Response]
	decodeJSON(t, resp, &page1)
	if len(page1.Items) != 2 {
		t.Fatalf("list page 1: got %d items, want 2", len(page1.Items))
	}
	if page1.Pagination.Total != 3 || page1.Pagination.TotalPages != 2 || !page1.Pagination.HasNext || page1.Pagination.HasPrevious {
		t.Fatalf("list page 1: pagination = %+v, want total=3 total_pages=2 has_next=true has_previous=false", page1.Pagination)
	}
	// Ordered by harvested_at DESC: the most recent two come first.
	if !page1.Items[0].HarvestedAt.Equal(mustParseTime(t, dates[2])) || !page1.Items[1].HarvestedAt.Equal(mustParseTime(t, dates[1])) {
		t.Fatalf("list page 1: items not ordered by harvested_at DESC: %+v", page1.Items)
	}

	resp = stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest?page=2&limit=2", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list page 2: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var page2 pagination.Response[harvesthttp.Response]
	decodeJSON(t, resp, &page2)
	if len(page2.Items) != 1 || page2.Pagination.HasNext || !page2.Pagination.HasPrevious {
		t.Fatalf("list page 2: pagination = %+v, items = %d, want 1 item has_next=false has_previous=true", page2.Pagination, len(page2.Items))
	}
	if !page2.Items[0].HarvestedAt.Equal(mustParseTime(t, dates[0])) {
		t.Fatalf("list page 2: expected the oldest harvest, got %+v", page2.Items[0])
	}
}

// mustParseTime parses an RFC 3339 timestamp, failing the test on error.
func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return parsed
}

// TestHarvestFlow_ListDefaultsPageAndLimit proves omitting page/limit
// falls back to page=1, limit=pagination.DefaultLimit.
func TestHarvestFlow_ListDefaultsPageAndLimit(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	resp := stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var list pagination.Response[harvesthttp.Response]
	decodeJSON(t, resp, &list)
	if list.Pagination.Page != pagination.DefaultPage || list.Pagination.Limit != pagination.DefaultLimit {
		t.Fatalf("list: pagination = %+v, want page=%d limit=%d", list.Pagination, pagination.DefaultPage, pagination.DefaultLimit)
	}
}

// TestHarvestFlow_ListInvalidPageAndLimit covers rejected page/limit
// query parameters, including limit above pagination.MaxLimit.
func TestHarvestFlow_ListInvalidPageAndLimit(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())

	cases := []string{
		"/api/v1/hives/" + hiveID.String() + "/harvest?page=0",
		"/api/v1/hives/" + hiveID.String() + "/harvest?page=-1",
		"/api/v1/hives/" + hiveID.String() + "/harvest?page=abc",
		"/api/v1/hives/" + hiveID.String() + "/harvest?limit=0",
		"/api/v1/hives/" + hiveID.String() + "/harvest?limit=101",
		"/api/v1/hives/" + hiveID.String() + "/harvest?limit=abc",
	}
	for _, path := range cases {
		resp := stack.request(t, http.MethodGet, path, token, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("GET %s: status = %d, want %d", path, resp.StatusCode, http.StatusBadRequest)
		}
	}
}

// seedFilterFixture creates a small, varied set of harvest records used
// by the filter tests below: two honey records (5kg, 15kg) and one wax
// record (15g), all under the same hive.
func seedFilterFixture(t *testing.T, stack *testStack, token string, hiveID uuid.UUID) {
	t.Helper()

	records := []map[string]any{
		{"product": "HONEY", "amount": 5, "unit": "kg", "harvested_at": testHarvestedAt},
		{"product": "HONEY", "amount": 15, "unit": "kg", "harvested_at": testHarvestedAt},
		{"product": "WAX", "amount": 15, "unit": "g", "harvested_at": testHarvestedAt},
	}
	for _, body := range records {
		resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, body)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("seed %v: status = %d, want %d", body, resp.StatusCode, http.StatusCreated)
		}
	}
}

// TestHarvestFlow_ProductFilter covers filtering by each supported
// product, plus rejection of an unsupported value.
func TestHarvestFlow_ProductFilter(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)
	seedFilterFixture(t, stack, token, hiveID)

	for _, tc := range []struct {
		product string
		want    int
	}{
		{"HONEY", 2},
		{"WAX", 1},
		{"POLLEN", 0},
		{"PROPOLIS", 0},
	} {
		resp := stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest?product="+tc.product, token, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("product=%s: status = %d, want %d", tc.product, resp.StatusCode, http.StatusOK)
		}
		var list pagination.Response[harvesthttp.Response]
		decodeJSON(t, resp, &list)
		if list.Pagination.Total != tc.want {
			t.Errorf("product=%s: total = %d, want %d", tc.product, list.Pagination.Total, tc.want)
		}
		for _, item := range list.Items {
			if string(item.Product) != tc.product {
				t.Errorf("product=%s: got item with product %s", tc.product, item.Product)
			}
		}
	}

	resp := stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest?product=SWARM", token, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("product=SWARM: status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// TestHarvestFlow_AmountFilter covers the amount_operator/amount pair
// for each supported operator, and rejection of invalid values.
func TestHarvestFlow_AmountFilter(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)
	seedFilterFixture(t, stack, token, hiveID)

	for _, tc := range []struct {
		name  string
		query string
		want  int
	}{
		{"gt", "amount_operator=gt&amount=10", 2}, // 15kg honey, 15g wax
		{"lt", "amount_operator=lt&amount=10", 1}, // 5kg honey
		{"eq", "amount_operator=eq&amount=15", 2}, // 15kg honey, 15g wax
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest?"+tc.query, token, nil)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			var list pagination.Response[harvesthttp.Response]
			decodeJSON(t, resp, &list)
			if list.Pagination.Total != tc.want {
				t.Fatalf("total = %d, want %d", list.Pagination.Total, tc.want)
			}
		})
	}

	invalidCases := []string{
		"amount_operator=gte&amount=10", // invalid operator
		"amount_operator=gt&amount=abc", // invalid amount
		"amount_operator=gt&amount=-1",  // negative amount
		"amount_operator=gt",            // operator without amount
		"amount=10",                     // amount without operator
	}
	for _, query := range invalidCases {
		resp := stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest?"+query, token, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d", query, resp.StatusCode, http.StatusBadRequest)
		}
	}
}

// TestHarvestFlow_CombinedFilters covers product+amount and filters
// combined with pagination, all applied together with AND semantics.
func TestHarvestFlow_CombinedFilters(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)
	seedFilterFixture(t, stack, token, hiveID)

	// product + amount: only the 15kg honey record matches both.
	resp := stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest?product=HONEY&amount_operator=gt&amount=10", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("product+amount: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var list pagination.Response[harvesthttp.Response]
	decodeJSON(t, resp, &list)
	if list.Pagination.Total != 1 || list.Items[0].Amount != 15 || list.Items[0].Product != "HONEY" {
		t.Fatalf("product+amount: got %+v, want only the 15kg honey record", list)
	}

	// Filters combined with pagination: product=HONEY matches 2 records;
	// limit=1 should still report the correct total across both pages.
	resp = stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest?product=HONEY&page=1&limit=1", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("product+pagination page 1: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	decodeJSON(t, resp, &list)
	if len(list.Items) != 1 || list.Pagination.Total != 2 || !list.Pagination.HasNext {
		t.Fatalf("product+pagination page 1: got %+v, want 1 item, total=2, has_next=true", list)
	}

	resp = stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest?product=HONEY&page=2&limit=1", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("product+pagination page 2: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	decodeJSON(t, resp, &list)
	if len(list.Items) != 1 || list.Pagination.Total != 2 || list.Pagination.HasNext {
		t.Fatalf("product+pagination page 2: got %+v, want 1 item, total=2, has_next=false", list)
	}
}

// TestHarvestFlow_FilteredOrdering proves harvested_at DESC / id DESC
// ordering still holds once a filter narrows the result set.
func TestHarvestFlow_FilteredOrdering(t *testing.T) {
	stack := newTestStack(t)
	hiveID := uuid.New()
	token := stack.tokenFor(t, uuid.New())
	stack.hive.allow(token, hiveID)

	dates := []string{"2026-08-01T00:00:00Z", "2026-08-15T00:00:00Z", "2026-09-01T00:00:00Z"}
	for _, d := range dates {
		resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
			"product": "HONEY", "amount": 20, "unit": "kg", "harvested_at": d,
		})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create %s: status = %d, want %d", d, resp.StatusCode, http.StatusCreated)
		}
	}
	// Should be excluded by the product filter below.
	resp := stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", token, map[string]any{
		"product": "WAX", "amount": 20, "unit": "g", "harvested_at": "2026-09-05T00:00:00Z",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create wax: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	resp = stack.request(t, http.MethodGet, "/api/v1/hives/"+hiveID.String()+"/harvest?product=HONEY", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var list pagination.Response[harvesthttp.Response]
	decodeJSON(t, resp, &list)
	if len(list.Items) != 3 {
		t.Fatalf("got %d items, want 3", len(list.Items))
	}
	for i, wantDate := range []string{dates[2], dates[1], dates[0]} {
		if !list.Items[i].HarvestedAt.Equal(mustParseTime(t, wantDate)) {
			t.Fatalf("item %d: harvested_at = %v, want %s (DESC order)", i, list.Items[i].HarvestedAt, wantDate)
		}
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
		"product": "HONEY", "amount": 10, "unit": "kg", "harvested_at": testHarvestedAt,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var created harvesthttp.Response
	decodeJSON(t, resp, &created)

	// Cannot create.
	resp = stack.request(t, http.MethodPost, "/api/v1/hives/"+hiveID.String()+"/harvest", otherToken, map[string]any{
		"product": "POLLEN", "amount": 500, "unit": "g", "harvested_at": testHarvestedAt,
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
		"product": "HONEY", "amount": 999, "unit": "kg", "harvested_at": testHarvestedAt,
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
	var list pagination.Response[harvesthttp.Response]
	decodeJSON(t, resp, &list)
	if len(list.Items) != 1 || list.Items[0].Amount != 10 {
		t.Fatalf("owner's harvest changed after other user's attempts: %+v", list.Items)
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
		"product": "HONEY", "amount": 10, "unit": "kg", "harvested_at": testHarvestedAt,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create in hive A: status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var created harvesthttp.Response
	decodeJSON(t, resp, &created)

	// tokenB owns hiveB, not hiveA, so this must fail with hive_not_found
	// (checked first) rather than leaking anything about hiveA's harvest.
	resp = stack.request(t, http.MethodPut, "/api/v1/hives/"+hiveB.String()+"/harvest/"+created.ID.String(), tokenB, map[string]any{
		"product": "HONEY", "amount": 999, "unit": "kg", "harvested_at": testHarvestedAt,
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("update via wrong hive (different owner): status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}

	// Same owner, but the wrong hive in the path: the hive check passes
	// (tokenA does own hiveB too, once allowed) yet the harvest doesn't
	// belong to hiveB, so it's harvest_not_found.
	stack.hive.allow(tokenA, hiveB)
	resp = stack.request(t, http.MethodPut, "/api/v1/hives/"+hiveB.String()+"/harvest/"+created.ID.String(), tokenA, map[string]any{
		"product": "HONEY", "amount": 999, "unit": "kg", "harvested_at": testHarvestedAt,
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
		"product": "HONEY", "amount": 1, "unit": "kg", "harvested_at": testHarvestedAt,
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
