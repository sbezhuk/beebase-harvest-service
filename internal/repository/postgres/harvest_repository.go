package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/sbezhuk/beebase-common/pagination"
	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
)

// HarvestRepository implements domain/harvest.Repository against
// PostgreSQL. Unlike most BeeBase repositories, no query here is scoped
// by a user_id column: this table has none (see domain/harvest's package
// doc comment) - ownership is entirely the application layer's
// responsibility, checked against hive-service before any of these
// methods is called.
type HarvestRepository struct {
	db Querier
}

type LegacyHarvest struct{ ID, HiveID uuid.UUID }

func (r *HarvestRepository) ListLegacy(ctx context.Context, after uuid.UUID, limit int) ([]LegacyHarvest, error) {
	rows, err := r.db.Query(ctx, `SELECT id,hive_id FROM harvests WHERE user_id IS NULL AND id > $1 ORDER BY id LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LegacyHarvest
	for rows.Next() {
		var v LegacyHarvest
		if err := rows.Scan(&v.ID, &v.HiveID); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (r *HarvestRepository) SetUserID(ctx context.Context, harvestID, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE harvests SET user_id=$2,updated_at=now() WHERE id=$1 AND user_id IS NULL`, harvestID, userID)
	return err
}
func (r *HarvestRepository) RecordBackfillFailure(ctx context.Context, harvestID, hiveID uuid.UUID, reason string) error {
	_, err := r.db.Exec(ctx, `INSERT INTO harvest_backfill_failures(harvest_id,hive_id,reason) VALUES($1,$2,$3) ON CONFLICT(harvest_id) DO UPDATE SET attempts=harvest_backfill_failures.attempts+1,reason=EXCLUDED.reason,last_attempt_at=now(),resolved_at=NULL`, harvestID, hiveID, reason)
	return err
}

// NewHarvestRepository returns a HarvestRepository backed by db.
func NewHarvestRepository(db Querier) *HarvestRepository {
	return &HarvestRepository{db: db}
}

// createdAtOrderClause returns the ORDER BY clause for a list query. When
// sortOrder is nil, defaultClause (the query's normal, pre-existing order)
// is used unchanged; otherwise the list is ordered by creation date in the
// requested direction, with id tied to the same direction as a stable
// tiebreaker (matching the convention every other ORDER BY in this
// repository already follows).
func createdAtOrderClause(sortOrder *string, defaultClause string) string {
	if sortOrder == nil {
		return defaultClause
	}
	dir := "ASC"
	if *sortOrder == "desc" {
		dir = "DESC"
	}
	return fmt.Sprintf("created_at %s, id %s", dir, dir)
}

func (r *HarvestRepository) Create(ctx context.Context, h *harvest.Harvest) error {
	const q = `
		INSERT INTO harvests (id, hive_id, user_id, product, amount, unit, harvested_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := r.db.Exec(ctx, q, h.ID, h.HiveID, h.UserID, h.Product, h.Amount, h.Unit, h.HarvestedAt, h.CreatedAt, h.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres: create harvest: %w", err)
	}

	return nil
}

func (r *HarvestRepository) GetByID(ctx context.Context, hiveID, harvestID uuid.UUID) (*harvest.Harvest, error) {
	const q = `
		SELECT id, hive_id, user_id, product, amount, unit, harvested_at, created_at, updated_at
		FROM harvests
		WHERE id = $1 AND hive_id = $2
	`

	var h harvest.Harvest
	err := r.db.QueryRow(ctx, q, harvestID, hiveID).Scan(
		&h.ID, &h.HiveID, &h.UserID, &h.Product, &h.Amount, &h.Unit, &h.HarvestedAt, &h.CreatedAt, &h.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, harvest.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get harvest: %w", err)
	}

	return &h, nil
}

func (r *HarvestRepository) List(ctx context.Context, hiveIDs []uuid.UUID, p pagination.Params, product *harvest.Product, amountOperator *harvest.AmountOperator, amount *float64, dateFrom, dateTo *time.Time, sortOrder *string) ([]*harvest.Harvest, int, error) {
	countQ := `SELECT count(*) FROM harvests WHERE hive_id = ANY($1)`
	q := `
		SELECT id, hive_id, user_id, product, amount, unit, harvested_at, created_at, updated_at
		FROM harvests
		WHERE hive_id = ANY($1)
	`
	countArgs := []any{hiveIDs}
	argIdx := 2

	if product != nil {
		cond := fmt.Sprintf(" AND product = $%d", argIdx)
		countQ += cond
		q += cond
		countArgs = append(countArgs, *product)
		argIdx++
	}

	if amountOperator != nil && amount != nil {
		cond := fmt.Sprintf(" AND amount %s $%d", amountOperator.SQL(), argIdx)
		countQ += cond
		q += cond
		countArgs = append(countArgs, *amount)
		argIdx++
	}

	if dateFrom != nil {
		cond := fmt.Sprintf(" AND harvested_at >= $%d", argIdx)
		countQ += cond
		q += cond
		countArgs = append(countArgs, dateFrom.Format("2006-01-02"))
		argIdx++
	}

	if dateTo != nil {
		cond := fmt.Sprintf(" AND harvested_at < $%d", argIdx)
		countQ += cond
		q += cond
		countArgs = append(countArgs, dateTo.Format("2006-01-02"))
		argIdx++
	}

	listArgs := make([]any, len(countArgs))
	copy(listArgs, countArgs)

	q += fmt.Sprintf(`
		ORDER BY %s
		LIMIT $%d OFFSET $%d`, createdAtOrderClause(sortOrder, "harvested_at DESC, id DESC"), argIdx, argIdx+1)
	listArgs = append(listArgs, p.Limit, p.Offset())

	var total int
	if err := r.db.QueryRow(ctx, countQ, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("postgres: count harvests: %w", err)
	}

	rows, err := r.db.Query(ctx, q, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres: list harvests: %w", err)
	}
	defer rows.Close()

	harvests := []*harvest.Harvest{}
	for rows.Next() {
		var h harvest.Harvest
		if err := rows.Scan(&h.ID, &h.HiveID, &h.UserID, &h.Product, &h.Amount, &h.Unit, &h.HarvestedAt, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("postgres: scan harvest: %w", err)
		}
		harvests = append(harvests, &h)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("postgres: list harvests: %w", err)
	}

	return harvests, total, nil
}

func (r *HarvestRepository) DeleteAllByUser(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM harvests WHERE user_id=$1`, userID)
	return err
}

func (r *HarvestRepository) ListByHive(ctx context.Context, hiveID uuid.UUID, p pagination.Params, product *harvest.Product, amountOperator *harvest.AmountOperator, amount *float64, dateFrom, dateTo *time.Time, sortOrder *string) ([]*harvest.Harvest, int, error) {
	return r.List(ctx, []uuid.UUID{hiveID}, p, product, amountOperator, amount, dateFrom, dateTo, sortOrder)
}

func (r *HarvestRepository) Update(ctx context.Context, h *harvest.Harvest) error {
	const q = `
		UPDATE harvests
		SET product = $1, amount = $2, unit = $3, harvested_at = $4, updated_at = $5
		WHERE id = $6 AND hive_id = $7
	`

	tag, err := r.db.Exec(ctx, q, h.Product, h.Amount, h.Unit, h.HarvestedAt, h.UpdatedAt, h.ID, h.HiveID)
	if err != nil {
		return fmt.Errorf("postgres: update harvest: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return harvest.ErrNotFound
	}

	return nil
}

func (r *HarvestRepository) Delete(ctx context.Context, hiveID, harvestID uuid.UUID) error {
	const q = `DELETE FROM harvests WHERE id = $1 AND hive_id = $2`

	tag, err := r.db.Exec(ctx, q, harvestID, hiveID)
	if err != nil {
		return fmt.Errorf("postgres: delete harvest: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return harvest.ErrNotFound
	}

	return nil
}

func (r *HarvestRepository) DeleteByHive(ctx context.Context, hiveID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `DELETE FROM harvests WHERE hive_id=$1 RETURNING id`, hiveID)
	if err != nil {
		return nil, fmt.Errorf("postgres: delete harvests by hive: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("postgres: scan deleted harvest id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *HarvestRepository) ListIDsByHive(ctx context.Context, hiveID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx, `SELECT id FROM harvests WHERE hive_id=$1`, hiveID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
