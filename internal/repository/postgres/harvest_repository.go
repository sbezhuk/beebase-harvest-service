package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/sbezhuk/beebase-harvest-service/internal/domain/harvest"
)

// uniqueViolationCode is PostgreSQL's SQLSTATE for a unique constraint
// violation, used to detect a duplicate (hive_id, product) pair.
const uniqueViolationCode = "23505"

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
		INSERT INTO harvests (id, hive_id, product, amount, unit, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := r.db.Exec(ctx, q, h.ID, h.HiveID, h.Product, h.Amount, h.Unit, h.CreatedAt, h.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return harvest.ErrDuplicateProduct
		}
		return fmt.Errorf("postgres: create harvest: %w", err)
	}

	return nil
}

func (r *HarvestRepository) GetByID(ctx context.Context, hiveID, harvestID uuid.UUID) (*harvest.Harvest, error) {
	const q = `
		SELECT id, hive_id, product, amount, unit, created_at, updated_at
		FROM harvests
		WHERE id = $1 AND hive_id = $2
	`

	var h harvest.Harvest
	err := r.db.QueryRow(ctx, q, harvestID, hiveID).Scan(
		&h.ID, &h.HiveID, &h.Product, &h.Amount, &h.Unit, &h.CreatedAt, &h.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, harvest.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get harvest: %w", err)
	}

	return &h, nil
}

func (r *HarvestRepository) ListByHive(ctx context.Context, hiveID uuid.UUID) ([]*harvest.Harvest, error) {
	const q = `
		SELECT id, hive_id, product, amount, unit, created_at, updated_at
		FROM harvests
		WHERE hive_id = $1
		ORDER BY created_at ASC, id ASC
	`

	rows, err := r.db.Query(ctx, q, hiveID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list harvests: %w", err)
	}
	defer rows.Close()

	harvests := []*harvest.Harvest{}
	for rows.Next() {
		var h harvest.Harvest
		if err := rows.Scan(&h.ID, &h.HiveID, &h.Product, &h.Amount, &h.Unit, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan harvest: %w", err)
		}
		harvests = append(harvests, &h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list harvests: %w", err)
	}

	return harvests, nil
}

func (r *HarvestRepository) Update(ctx context.Context, h *harvest.Harvest) error {
	const q = `
		UPDATE harvests
		SET product = $1, amount = $2, unit = $3, updated_at = $4
		WHERE id = $5 AND hive_id = $6
	`

	tag, err := r.db.Exec(ctx, q, h.Product, h.Amount, h.Unit, h.UpdatedAt, h.ID, h.HiveID)
	if err != nil {
		if isUniqueViolation(err) {
			return harvest.ErrDuplicateProduct
		}
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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode
}
