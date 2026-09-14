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
		INSERT INTO harvests (id, hive_id, product, amount, unit, harvested_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := r.db.Exec(ctx, q, h.ID, h.HiveID, h.Product, h.Amount, h.Unit, h.HarvestedAt, h.CreatedAt, h.UpdatedAt)
	if err != nil {
		return fmt.Errorf("postgres: create harvest: %w", err)
	}

	return nil
}

func (r *HarvestRepository) GetByID(ctx context.Context, hiveID, harvestID uuid.UUID) (*harvest.Harvest, error) {
	const q = `
		SELECT id, hive_id, product, amount, unit, harvested_at, created_at, updated_at
		FROM harvests
		WHERE id = $1 AND hive_id = $2
	`

	var h harvest.Harvest
	err := r.db.QueryRow(ctx, q, harvestID, hiveID).Scan(
		&h.ID, &h.HiveID, &h.Product, &h.Amount, &h.Unit, &h.HarvestedAt, &h.CreatedAt, &h.UpdatedAt,
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
		SELECT id, hive_id, product, amount, unit, harvested_at, created_at, updated_at
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
		countArgs = append(countArgs, *dateFrom)
		argIdx++
	}

	if dateTo != nil {
		cond := fmt.Sprintf(" AND harvested_at < $%d", argIdx)
		countQ += cond
		q += cond
		countArgs = append(countArgs, *dateTo)
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
		if err := rows.Scan(&h.ID, &h.HiveID, &h.Product, &h.Amount, &h.Unit, &h.HarvestedAt, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("postgres: scan harvest: %w", err)
		}
		harvests = append(harvests, &h)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("postgres: list harvests: %w", err)
	}

	return harvests, total, nil
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
