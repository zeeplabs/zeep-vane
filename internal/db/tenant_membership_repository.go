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

// TenantMembership links a user to a tenant with a role (owner/operator/
// viewer, AD-003 - now scoped per tenant rather than global) -
// multi-tenancy-core, AD-022.
type TenantMembership struct {
	UserID    string
	TenantID  string
	Role      string
	CreatedAt time.Time
}

// ErrDuplicateMembership is returned when creating a membership that
// already exists for the (user_id, tenant_id) pair (the table's composite
// primary key).
var ErrDuplicateMembership = errors.New("db: membership already exists for this user and tenant")

// ErrLastOwner is returned by Delete when removing the given membership
// would leave its tenant with zero owners.
var ErrLastOwner = errors.New("db: cannot remove the last owner of a tenant")

// TenantMembershipRepository accesses the tenant_memberships table.
type TenantMembershipRepository struct {
	pool *Pool
}

// NewTenantMembershipRepository builds a TenantMembershipRepository backed
// by pool.
func NewTenantMembershipRepository(pool *Pool) *TenantMembershipRepository {
	return &TenantMembershipRepository{pool: pool}
}

// Create inserts m, filling in its CreatedAt. tenant_memberships' RLS
// policy accepts either app.user_id or app.tenant_id matching the row
// being written (0024) - a caller creating a membership as part of
// provisioning a new tenant (bootstrap/signup, T6/T9) already has
// app.tenant_id set to that tenant from the same transaction that created
// it, which alone satisfies WITH CHECK here; a caller accepting an invite
// for an existing tenant (T13/T14) is expected to have app.tenant_id set to
// that tenant's id via the tenant-context middleware.
func (r *TenantMembershipRepository) Create(ctx context.Context, m *TenantMembership) error {
	row := r.pool.QueryRow(ctx,
		"INSERT INTO tenant_memberships (user_id, tenant_id, role) VALUES ($1, $2, $3) RETURNING created_at",
		m.UserID, m.TenantID, m.Role,
	)
	if err := row.Scan(&m.CreatedAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return ErrDuplicateMembership
		}
		return fmt.Errorf("db: failed to create tenant membership: %w", err)
	}
	return nil
}

// ListForUser returns every tenant_membership row for userID, across every
// tenant that user belongs to - the query login (T7) resolves the active
// tenant from, and the tenant-selection screen (T17) lists. Requires
// app.user_id set to userID (or app.tenant_id set to one of the tenants
// being asked about) - RLS's user_id branch is what makes this callable
// before any tenant is chosen yet (the chicken-and-egg case: this query is
// how the caller finds out which tenant(s) exist for this user in the
// first place).
func (r *TenantMembershipRepository) ListForUser(ctx context.Context, userID string) ([]TenantMembership, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT user_id, tenant_id, role, created_at FROM tenant_memberships WHERE user_id = $1 ORDER BY created_at ASC",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list tenant memberships for user: %w", err)
	}
	defer rows.Close()

	var memberships []TenantMembership
	for rows.Next() {
		var m TenantMembership
		if err := rows.Scan(&m.UserID, &m.TenantID, &m.Role, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("db: failed to scan tenant membership: %w", err)
		}
		memberships = append(memberships, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to list tenant memberships for user: %w", err)
	}

	return memberships, nil
}

// ListForTenant returns every member of tenantID - the tenant-scoped
// member-management list (T13, not wired to any handler in this batch).
// Requires app.tenant_id set to tenantID.
func (r *TenantMembershipRepository) ListForTenant(ctx context.Context, tenantID string) ([]TenantMembership, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT user_id, tenant_id, role, created_at FROM tenant_memberships WHERE tenant_id = $1 ORDER BY created_at ASC",
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list tenant memberships for tenant: %w", err)
	}
	defer rows.Close()

	var memberships []TenantMembership
	for rows.Next() {
		var m TenantMembership
		if err := rows.Scan(&m.UserID, &m.TenantID, &m.Role, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("db: failed to scan tenant membership: %w", err)
		}
		memberships = append(memberships, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to list tenant memberships for tenant: %w", err)
	}

	return memberships, nil
}

// UpdateRole sets a new role for the (userID, tenantID) membership,
// returning ErrNotFound if no such membership exists.
func (r *TenantMembershipRepository) UpdateRole(ctx context.Context, userID, tenantID, role string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE tenant_memberships SET role = $3 WHERE user_id = $1 AND tenant_id = $2",
		userID, tenantID, role,
	)
	if err != nil {
		return fmt.Errorf("db: failed to update tenant membership role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes the (userID, tenantID) membership, returning ErrNotFound
// if no such membership exists, or ErrLastOwner - no row removed - if
// userID is that tenant's only owner. The owner-count check runs
// SELECT ... FOR UPDATE against the tenant's owner rows first, so a
// concurrent Delete/UpdateRole affecting the same tenant's owners blocks
// until this call's transaction ends, the same race-safety shape as
// AdminRepository.CountActiveOwners.
func (r *TenantMembershipRepository) Delete(ctx context.Context, userID, tenantID string) error {
	var role string
	row := r.pool.QueryRow(ctx,
		"SELECT role FROM tenant_memberships WHERE user_id = $1 AND tenant_id = $2",
		userID, tenantID,
	)
	if err := row.Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("db: failed to look up tenant membership for delete: %w", err)
	}

	if role == RoleOwner {
		var ownerCount int
		countRow := r.pool.QueryRow(ctx,
			"SELECT COUNT(*) FROM (SELECT user_id FROM tenant_memberships WHERE tenant_id = $1 AND role = $2 FOR UPDATE) locked_owners",
			tenantID, RoleOwner,
		)
		if err := countRow.Scan(&ownerCount); err != nil {
			return fmt.Errorf("db: failed to count tenant owners: %w", err)
		}
		if ownerCount <= 1 {
			return ErrLastOwner
		}
	}

	tag, err := r.pool.Exec(ctx,
		"DELETE FROM tenant_memberships WHERE user_id = $1 AND tenant_id = $2",
		userID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("db: failed to delete tenant membership: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}
