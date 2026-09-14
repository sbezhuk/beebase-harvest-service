// Package hiveclient implements application/harvest.HiveVerifier against
// the real hive-service over HTTP.
package hiveclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	appharvest "github.com/sbezhuk/beebase-harvest-service/internal/application/harvest"
)

const requestTimeout = 5 * time.Second

// Client verifies hive ownership by forwarding the caller's own access
// token to hive-service's GET /api/v1/hives/{id}, and trusting
// hive-service's own ownership check: a 200 means whoever holds that
// token owns that hive (and, transitively, its apiary - hive-service's
// own check is itself transitive against apiary-service), a 404 means
// they don't. This service never queries hive or apiary ownership itself.
type Client struct {
	baseURL string
	http    *http.Client
}

type hivePage struct {
	Items []struct {
		ID uuid.UUID `json:"id"`
	} `json:"items"`
	Pagination struct {
		TotalPages int `json:"total_pages"`
	} `json:"pagination"`
}

// New returns a Client that calls hive-service at baseURL (e.g.
// "http://hive-service:8080").
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

// Verify implements application/harvest.HiveVerifier.
func (c *Client) Verify(ctx context.Context, accessToken string, hiveID uuid.UUID) error {
	url := fmt.Sprintf("%s/api/v1/hives/%s", c.baseURL, hiveID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("hiveclient: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("hiveclient: call hive-service: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return appharvest.ErrHiveNotFound
	default:
		// Anything else (401, 5xx, ...) is unexpected for a token this
		// service already verified itself: fail closed with a distinct,
		// observable error rather than silently treating it as "not
		// found", which would mask a real problem (e.g. hive-service
		// misconfigured or unreachable) as a client-facing 404.
		return fmt.Errorf("hiveclient: unexpected status %d from hive-service", resp.StatusCode)
	}
}

// ListOwned implements application/harvest.OwnedHiveLister by paging through
// hive-service's user-scoped global hive list.
func (c *Client) ListOwned(ctx context.Context, accessToken string) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	for page := 1; ; page++ {
		u := fmt.Sprintf("%s/api/v1/hives?page=%d&limit=100", c.baseURL, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, fmt.Errorf("hiveclient: build list request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("hiveclient: list hives: %w", err)
		}
		var body hivePage
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("hiveclient: unexpected status %d from hive-service", resp.StatusCode)
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("hiveclient: decode hive list: %w", decodeErr)
		}
		for _, item := range body.Items {
			ids = append(ids, item.ID)
		}
		if page >= body.Pagination.TotalPages || len(body.Items) == 0 {
			return ids, nil
		}
	}
}
