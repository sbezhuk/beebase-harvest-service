package hiveclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	appharvest "github.com/sbezhuk/beebase-harvest-service/internal/application/harvest"
	"github.com/sbezhuk/beebase-harvest-service/internal/platform/hiveclient"
)

func TestClient_Verify_OwnedAndWritable(t *testing.T) {
	hiveID := uuid.New()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-token" {
			t.Errorf("Authorization header = %q, want forwarded bearer token", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/api/v1/hives/"+hiveID.String() {
			t.Errorf("path = %q, want to include the hive id", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"writable": true})
	}))
	defer srv.Close()

	client := hiveclient.New(srv.URL)
	writable, err := client.Verify(context.Background(), "good-token", hiveID)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !writable {
		t.Error("writable = false, want true")
	}
}

func TestClient_Verify_OwnedButReadOnly(t *testing.T) {
	hiveID := uuid.New()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"writable": false})
	}))
	defer srv.Close()

	client := hiveclient.New(srv.URL)
	writable, err := client.Verify(context.Background(), "good-token", hiveID)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if writable {
		t.Error("writable = true, want false")
	}
}

func TestClient_Verify_NotOwned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := hiveclient.New(srv.URL)
	_, err := client.Verify(context.Background(), "some-token", uuid.New())
	if !errors.Is(err, appharvest.ErrHiveNotFound) {
		t.Fatalf("Verify against a 404: got %v, want ErrHiveNotFound", err)
	}
}

func TestClient_Verify_UnexpectedStatusFailsClosed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := hiveclient.New(srv.URL)
	_, err := client.Verify(context.Background(), "some-token", uuid.New())
	if err == nil {
		t.Fatal("Verify against a 500: got nil error, want a failure")
	}
	if errors.Is(err, appharvest.ErrHiveNotFound) {
		t.Fatal("Verify against a 500 should not be reported as ErrHiveNotFound: that would mask hive-service being broken as a plain 404")
	}
}

func TestClient_Verify_UnreachableServer(t *testing.T) {
	client := hiveclient.New("http://127.0.0.1:1") // nothing listens here
	_, err := client.Verify(context.Background(), "some-token", uuid.New())
	if err == nil {
		t.Fatal("Verify against an unreachable server: got nil error, want a failure")
	}
}
