package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrIntervalRaceLost is returned by OpenOrExtend when this call's INSERT of
// a new open interval loses a race against a concurrent writer that opened
// (and committed) an interval for the same service first - surfaced by the
// unique partial index on (service_id) WHERE ends_at IS NULL (SHU-05).
var ErrIntervalRaceLost = errors.New("db: lost race to open a status interval for this service")

// StatusInterval is a single status-interval row: the service held Status
// from StartsAt until EndsAt (nil while it is the service's current, open
// interval). LastSeenAt is bumped on every poll that confirms Status is
// still current, including the poll that opened the interval.
type StatusInterval struct {
	ID                   string
	ServiceID            string
	Status               string
	ErrorBudgetRemaining float64
	StartsAt             time.Time
	LastSeenAt           time.Time
	EndsAt               *time.Time
}

// StatusIntervalRepository accesses the status_intervals table.
type StatusIntervalRepository struct {
	pool *Pool
}

// NewStatusIntervalRepository builds a StatusIntervalRepository backed by
// pool.
func NewStatusIntervalRepository(pool *Pool) *StatusIntervalRepository {
	return &StatusIntervalRepository{pool: pool}
}

// OpenOrExtend records serviceID's observed status as of at (SHU-01/02/03).
// It locks the service's currently open interval, if any, with
// SELECT ... FOR UPDATE inside a transaction (same shape as
// StatusPageRepository.AttachDomain) and branches:
//   - no open interval -> INSERT a new open interval
//   - open interval has the same status -> UPDATE its error_budget_remaining
//     and last_seen_at in place, no new row
//   - open interval has a different status -> UPDATE the old row's ends_at
//     to at, then INSERT a new open interval starting at at
//
// If a concurrent writer's INSERT commits first for the same service, this
// call's own INSERT fails the unique partial index and OpenOrExtend returns
// ErrIntervalRaceLost (SHU-05) - the losing writer's error is surfaced to
// the caller, never silently swallowed, and no duplicate open interval is
// ever left behind.
func (r *StatusIntervalRepository) OpenOrExtend(ctx context.Context, serviceID, status string, errorBudgetRemaining float64, at time.Time) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: failed to begin open-or-extend transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var open StatusInterval
	hasOpen := true
	row := tx.QueryRow(ctx,
		"SELECT id, status FROM status_intervals WHERE service_id = $1 AND ends_at IS NULL FOR UPDATE",
		serviceID,
	)
	if err := row.Scan(&open.ID, &open.Status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			hasOpen = false
		} else {
			return fmt.Errorf("db: failed to lock open status interval: %w", err)
		}
	}

	switch {
	case !hasOpen:
		if err := insertOpenInterval(ctx, tx, serviceID, status, errorBudgetRemaining, at); err != nil {
			return err
		}
	case open.Status == status:
		if _, err := tx.Exec(ctx,
			"UPDATE status_intervals SET error_budget_remaining = $1, last_seen_at = $2 WHERE id = $3",
			errorBudgetRemaining, at, open.ID,
		); err != nil {
			return fmt.Errorf("db: failed to extend open status interval: %w", err)
		}
	default:
		if _, err := tx.Exec(ctx,
			"UPDATE status_intervals SET ends_at = $1 WHERE id = $2",
			at, open.ID,
		); err != nil {
			return fmt.Errorf("db: failed to close open status interval: %w", err)
		}
		if err := insertOpenInterval(ctx, tx, serviceID, status, errorBudgetRemaining, at); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: failed to commit open-or-extend transaction: %w", err)
	}

	return nil
}

// insertOpenInterval inserts a new open interval (starts_at == last_seen_at
// == at, ends_at NULL) for serviceID within tx, translating a unique
// partial index violation into ErrIntervalRaceLost.
func insertOpenInterval(ctx context.Context, tx pgx.Tx, serviceID, status string, errorBudgetRemaining float64, at time.Time) error {
	if _, err := tx.Exec(ctx,
		"INSERT INTO status_intervals (service_id, status, error_budget_remaining, starts_at, last_seen_at) VALUES ($1, $2, $3, $4, $4)",
		serviceID, status, errorBudgetRemaining, at,
	); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return fmt.Errorf("db: failed to open status interval: %w", ErrIntervalRaceLost)
		}
		return fmt.Errorf("db: failed to insert open status interval: %w", err)
	}
	return nil
}
