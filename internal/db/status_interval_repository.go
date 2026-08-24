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

// OpenIntervalsByService returns each service's currently-open interval
// (ends_at IS NULL), keyed by service_id. A service with no currently-open
// interval (never polled, or - impossible in practice today - somehow left
// with only closed rows) simply has no entry in the returned map.
func (r *StatusIntervalRepository) OpenIntervalsByService(ctx context.Context) (map[string]StatusInterval, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id, service_id, status, error_budget_remaining, starts_at, last_seen_at, ends_at FROM status_intervals WHERE ends_at IS NULL",
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to query open status intervals: %w", err)
	}
	defer rows.Close()

	open := map[string]StatusInterval{}
	for rows.Next() {
		var si StatusInterval
		if err := rows.Scan(&si.ID, &si.ServiceID, &si.Status, &si.ErrorBudgetRemaining, &si.StartsAt, &si.LastSeenAt, &si.EndsAt); err != nil {
			return nil, fmt.Errorf("db: failed to scan open status interval: %w", err)
		}
		open[si.ServiceID] = si
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate open status intervals: %w", err)
	}

	return open, nil
}

// ListOverlapping returns every status_intervals row for any of serviceIDs
// that overlaps [windowStart, now] - i.e. starts_at < now AND (ends_at IS
// NULL OR ends_at > windowStart) - ordered by service_id then starts_at
// ascending. An empty serviceIDs returns an empty slice immediately, no
// query executed, never an error.
func (r *StatusIntervalRepository) ListOverlapping(ctx context.Context, serviceIDs []string, windowStart, now time.Time) ([]StatusInterval, error) {
	if len(serviceIDs) == 0 {
		return []StatusInterval{}, nil
	}

	rows, err := r.pool.Query(ctx,
		"SELECT id, service_id, status, error_budget_remaining, starts_at, last_seen_at, ends_at FROM status_intervals "+
			"WHERE service_id = ANY($1) AND starts_at < $2 AND (ends_at IS NULL OR ends_at > $3) "+
			"ORDER BY service_id, starts_at ASC",
		serviceIDs, now, windowStart,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to query overlapping status intervals: %w", err)
	}
	defer rows.Close()

	intervals := []StatusInterval{}
	for rows.Next() {
		var si StatusInterval
		if err := rows.Scan(&si.ID, &si.ServiceID, &si.Status, &si.ErrorBudgetRemaining, &si.StartsAt, &si.LastSeenAt, &si.EndsAt); err != nil {
			return nil, fmt.Errorf("db: failed to scan overlapping status interval: %w", err)
		}
		intervals = append(intervals, si)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate overlapping status intervals: %w", err)
	}

	return intervals, nil
}

// DeleteClosedBefore deletes every status_intervals row with a non-null
// ends_at older than cutoff, returning the number of rows deleted. Open
// intervals (ends_at IS NULL) are never touched, regardless of how old
// their starts_at is.
func (r *StatusIntervalRepository) DeleteClosedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		"DELETE FROM status_intervals WHERE ends_at IS NOT NULL AND ends_at < $1",
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("db: failed to delete closed status intervals before cutoff: %w", err)
	}

	return tag.RowsAffected(), nil
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
