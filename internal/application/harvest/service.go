// Package harvest implements the harvest use cases: create, get, list,
// update, and delete, all scoped to one hive. It depends only on the
// domain/harvest port and the HiveVerifier port declared in this
// package, never on HTTP or PostgreSQL directly.
package harvest

import (
	"context"
	"fmt"
	"sort"
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
	harvests  harvest.Repository
	hives     HiveVerifier
	reminders interface {
		Cleanup(context.Context, string, uuid.UUID) error
	}
}

type ReportData struct {
	Harvests []*harvest.Harvest
	Totals   []ReportTotal
}

type ReportTotal struct {
	Product harvest.Product
	Unit    harvest.Unit
	Amount  float64
}

// NewService constructs a Service.
func NewService(harvests harvest.Repository, hives HiveVerifier, reminders ...interface {
	Cleanup(context.Context, string, uuid.UUID) error
}) *Service {
	s := &Service{harvests: harvests, hives: hives}
	if len(reminders) > 0 {
		s.reminders = reminders[0]
	}
	return s
}

// Create creates a new harvest record under hiveID, after confirming
// hiveID belongs to whoever presented accessToken. A hive may have any
// number of harvest records for the same product - each call creates a
// new, independent record.
func (s *Service) Create(ctx context.Context, accessToken string, hiveID uuid.UUID, in CreateInput) (*harvest.Harvest, error) {
	return s.create(ctx, accessToken, hiveID, nil, in)
}

func (s *Service) CreateForUser(ctx context.Context, userID uuid.UUID, accessToken string, hiveID uuid.UUID, in CreateInput) (*harvest.Harvest, error) {
	return s.create(ctx, accessToken, hiveID, &userID, in)
}

func (s *Service) create(ctx context.Context, accessToken string, hiveID uuid.UUID, userID *uuid.UUID, in CreateInput) (*harvest.Harvest, error) {
	writable, err := s.hives.Verify(ctx, accessToken, hiveID)
	if err != nil {
		return nil, err
	}
	if !writable {
		return nil, ErrHiveReadOnly
	}

	h := harvest.New(hiveID, in.Product, in.Amount, in.Unit, in.HarvestedAt)
	h.UserID = userID
	if err := s.harvests.Create(ctx, h); err != nil {
		return nil, fmt.Errorf("harvest: create: %w", err)
	}

	return h, nil
}

func (s *Service) DeleteAllByUser(ctx context.Context, userID uuid.UUID) error {
	r, ok := s.harvests.(interface {
		DeleteAllByUser(context.Context, uuid.UUID) error
	})
	if !ok {
		return fmt.Errorf("harvest: repository does not support account cleanup")
	}
	return r.DeleteAllByUser(ctx, userID)
}

// DeleteByHive hard-deletes every harvest under an owned hive. It is the
// cascade primitive called by hive-service.
func (s *Service) DeleteByHive(ctx context.Context, accessToken string, hiveID uuid.UUID) ([]uuid.UUID, error) {
	if _, err := s.hives.Verify(ctx, accessToken, hiveID); err != nil {
		return nil, err
	}
	ids, err := s.harvests.ListIDsByHive(ctx, hiveID)
	if err != nil {
		return nil, err
	}
	if s.reminders != nil {
		for _, id := range ids {
			if err := s.reminders.Cleanup(ctx, "harvest", id); err != nil {
				return nil, err
			}
		}
	}
	return s.harvests.DeleteByHive(ctx, hiveID)
}

// Get returns the harvest identified by harvestID under hiveID, after
// confirming hiveID belongs to whoever presented accessToken.
func (s *Service) Get(ctx context.Context, accessToken string, hiveID, harvestID uuid.UUID) (*harvest.Harvest, error) {
	if _, err := s.hives.Verify(ctx, accessToken, hiveID); err != nil {
		return nil, err
	}

	return s.harvests.GetByID(ctx, hiveID, harvestID)
}

// List returns the page of harvest records described by p for hiveID,
// after confirming hiveID belongs to whoever presented accessToken,
// along with the total number of matching records. product,
// amountOperator/amount, and dateFrom/dateTo are optional filters - see
// harvest.Repository.ListByHive for how they combine. When sortOrder is
// non-nil ("asc" or "desc") the page is ordered by creation date in that
// direction instead of the repository's default order (HarvestedAt DESC).
func (s *Service) List(ctx context.Context, accessToken string, hiveID uuid.UUID, p pagination.Params, product *harvest.Product, amountOperator *harvest.AmountOperator, amount *float64, dateFrom, dateTo *time.Time, sortOrder *string) ([]*harvest.Harvest, int, error) {
	if _, err := s.hives.Verify(ctx, accessToken, hiveID); err != nil {
		return nil, 0, err
	}

	if scoped, ok := s.harvests.(harvest.ScopedRepository); ok {
		return scoped.List(ctx, []uuid.UUID{hiveID}, p, product, amountOperator, amount, dateFrom, dateTo, sortOrder)
	}
	return s.harvests.ListByHive(ctx, hiveID, p, product, amountOperator, amount, dateFrom, dateTo, sortOrder)
}

// ListAll returns the page of harvest records across every hive owned by the
// caller. Hive-service supplies the owned hive IDs; the repository then
// applies the same filters, ordering, and pagination as the nested list.
func (s *Service) ListAll(ctx context.Context, accessToken string, p pagination.Params, product *harvest.Product, amountOperator *harvest.AmountOperator, amount *float64, dateFrom, dateTo *time.Time, sortOrder *string) ([]*harvest.Harvest, int, error) {
	lister, ok := s.hives.(OwnedHiveLister)
	if !ok {
		return nil, 0, fmt.Errorf("harvest: hive verifier does not support listing owned hives")
	}
	hiveIDs, err := lister.ListOwned(ctx, accessToken)
	if err != nil {
		return nil, 0, fmt.Errorf("harvest: list owned hives: %w", err)
	}
	scoped, ok := s.harvests.(harvest.ScopedRepository)
	if !ok {
		return nil, 0, fmt.Errorf("harvest: repository does not support cross-hive listing")
	}
	return scoped.List(ctx, hiveIDs, p, product, amountOperator, amount, dateFrom, dateTo, sortOrder)
}

// GetInternalReportData returns all harvest records in the inclusive date
// range for a trusted internal report consumer. It reuses the repository's
// existing date filtering and ordering semantics, fetching pages internally
// so the wire contract itself is not paginated.
func (s *Service) GetInternalReportData(ctx context.Context, hiveID uuid.UUID, from, to time.Time) (ReportData, error) {
	toExclusive := to.AddDate(0, 0, 1)
	all := make([]*harvest.Harvest, 0)
	for page := 1; ; page++ {
		items, total, err := s.harvests.ListByHive(ctx, hiveID, pagination.Params{Page: page, Limit: pagination.MaxLimit}, nil, nil, nil, &from, &toExclusive, nil)
		if err != nil {
			return ReportData{}, fmt.Errorf("harvest: list internal report data: %w", err)
		}
		all = append(all, items...)
		if len(all) >= total || len(items) == 0 {
			break
		}
	}

	totalByKey := make(map[[2]string]float64)
	for _, item := range all {
		key := [2]string{string(item.Product), string(item.Unit)}
		totalByKey[key] += item.Amount
	}
	totals := make([]ReportTotal, 0, len(totalByKey))
	for key, amount := range totalByKey {
		totals = append(totals, ReportTotal{Product: harvest.Product(key[0]), Unit: harvest.Unit(key[1]), Amount: amount})
	}
	sort.Slice(totals, func(i, j int) bool {
		if totals[i].Product != totals[j].Product {
			return totals[i].Product < totals[j].Product
		}
		return totals[i].Unit < totals[j].Unit
	})
	return ReportData{Harvests: all, Totals: totals}, nil
}

// Update replaces the editable fields of the harvest identified by
// harvestID under hiveID, after confirming hiveID belongs to whoever
// presented accessToken and that harvestID itself belongs to hiveID.
func (s *Service) Update(ctx context.Context, accessToken string, hiveID, harvestID uuid.UUID, in UpdateInput) (*harvest.Harvest, error) {
	writable, err := s.hives.Verify(ctx, accessToken, hiveID)
	if err != nil {
		return nil, err
	}
	if !writable {
		return nil, ErrHiveReadOnly
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
	// Deleting a harvest is never gated by writability - a Free user can
	// always delete historical records under a read-only hive, the same
	// as every other resource in this model.
	if _, err := s.hives.Verify(ctx, accessToken, hiveID); err != nil {
		return err
	}

	return s.harvests.Delete(ctx, hiveID, harvestID)
}
