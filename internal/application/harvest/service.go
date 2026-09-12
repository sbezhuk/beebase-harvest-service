// Package harvest implements the harvest use cases: create, get, list,
// update, and delete, all scoped to one hive. It depends only on the
// domain/harvest port and the HiveVerifier port declared in this
// package, never on HTTP or PostgreSQL directly.
package harvest

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/sbezhuk/beebase-common/pagination"
	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
)

// Service implements the harvest use cases. Every method takes the
// hiveID the harvest belongs to and the caller's own access token, and
// starts by confirming with HiveVerifier that accessToken's owner owns
// hiveID - on every call, not just once at creation time, since this
// service stores no hive ownership data of its own (see
// HiveVerifier's doc comment).
type Service struct {
	harvests harvest.Repository
	hives    HiveVerifier
}

// NewService constructs a Service.
func NewService(harvests harvest.Repository, hives HiveVerifier) *Service {
	return &Service{harvests: harvests, hives: hives}
}

// Create creates a new harvest record under hiveID, after confirming
// hiveID belongs to whoever presented accessToken. A hive may have any
// number of harvest records for the same product - each call creates a
// new, independent record.
func (s *Service) Create(ctx context.Context, accessToken string, hiveID uuid.UUID, in CreateInput) (*harvest.Harvest, error) {
	if err := s.hives.Verify(ctx, accessToken, hiveID); err != nil {
		return nil, err
	}

	h := harvest.New(hiveID, in.Product, in.Amount, in.Unit, in.HarvestedAt)
	if err := s.harvests.Create(ctx, h); err != nil {
		return nil, fmt.Errorf("harvest: create: %w", err)
	}

	return h, nil
}

// Get returns the harvest identified by harvestID under hiveID, after
// confirming hiveID belongs to whoever presented accessToken.
func (s *Service) Get(ctx context.Context, accessToken string, hiveID, harvestID uuid.UUID) (*harvest.Harvest, error) {
	if err := s.hives.Verify(ctx, accessToken, hiveID); err != nil {
		return nil, err
	}

	return s.harvests.GetByID(ctx, hiveID, harvestID)
}

// List returns the page of harvest records described by p for hiveID,
// after confirming hiveID belongs to whoever presented accessToken,
// along with the total number of matching records. search, product,
// amountOperator, and amount are optional filters - see
// harvest.Repository.ListByHive for how they combine.
func (s *Service) List(ctx context.Context, accessToken string, hiveID uuid.UUID, p pagination.Params, search *string, product *harvest.Product, amountOperator *harvest.AmountOperator, amount *float64) ([]*harvest.Harvest, int, error) {
	if err := s.hives.Verify(ctx, accessToken, hiveID); err != nil {
		return nil, 0, err
	}

	return s.harvests.ListByHive(ctx, hiveID, p, search, product, amountOperator, amount)
}

// Update replaces the editable fields of the harvest identified by
// harvestID under hiveID, after confirming hiveID belongs to whoever
// presented accessToken and that harvestID itself belongs to hiveID.
func (s *Service) Update(ctx context.Context, accessToken string, hiveID, harvestID uuid.UUID, in UpdateInput) (*harvest.Harvest, error) {
	if err := s.hives.Verify(ctx, accessToken, hiveID); err != nil {
		return nil, err
	}

	h, err := s.harvests.GetByID(ctx, hiveID, harvestID)
	if err != nil {
		return nil, err
	}

	h.Product = in.Product
	h.Amount = in.Amount
	h.Unit = in.Unit
	h.HarvestedAt = in.HarvestedAt
	h.UpdatedAt = time.Now().UTC()

	if err := s.harvests.Update(ctx, h); err != nil {
		return nil, fmt.Errorf("harvest: update: %w", err)
	}

	return h, nil
}

// Delete deletes the harvest identified by harvestID under hiveID, after
// confirming hiveID belongs to whoever presented accessToken.
func (s *Service) Delete(ctx context.Context, accessToken string, hiveID, harvestID uuid.UUID) error {
	if err := s.hives.Verify(ctx, accessToken, hiveID); err != nil {
		return err
	}

	return s.harvests.Delete(ctx, hiveID, harvestID)
}
