package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/sbezhuk/beebase-common/pagination"
	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
)

// minSearchLength is the minimum number of characters required for the
// search term to be applied. Shorter terms produce noisy results and put
// unnecessary load on the database.
const minSearchLength = 3

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

func (r *HarvestRepository) ListByHive(ctx context.Context, hiveID uuid.UUID, p pagination.Params, search *string, product *harvest.Product, amountOperator *harvest.AmountOperator, amount *float64) ([]*harvest.Harvest, int, error) {
	countQ := `SELECT count(*) FROM harvests WHERE hive_id = $1`
	q := `
		SELECT id, hive_id, product, amount, unit, harvested_at, created_at, updated_at
		FROM harvests
		WHERE hive_id = $1
	`
	countArgs := []any{hiveID}
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

	listArgs := make([]any, len(countArgs))
	copy(listArgs, countArgs)

	if search != nil && len(*search) >= minSearchLength {
		pattern := "%" + *search + "%"
		cond := fmt.Sprintf(" AND product ILIKE $%d", argIdx)
		countQ += cond
		q += cond
		countArgs = append(countArgs, pattern)
		listArgs = append(listArgs, pattern)
		argIdx++
	}

	q += fmt.Sprintf(`
		ORDER BY harvested_at DESC, id DESC
		LIMIT $%d OFFSET $%d`, argIdx, argIdx+1)
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
