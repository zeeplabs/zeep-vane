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
	// Name and Plan are the owning tenant's name/plan (tenants.name,
	// tenants.plan), joined in by ListForUser (new-layout-migration,
	// SHELL-20/21) so callers building meMembership/loginResponse's
	// membership list don't need a second query per row. ListForTenant
	// (below) does not join these - its callers don't need them yet.
	Name string
	Plan string
}

// ErrDuplicateMembership is returned when creating a membership that
// already exists for the (user_id, tenant_id) pair (the table's composite
// primary key).
var ErrDuplicateMembership = errors.New("db: membership already exists for this user and tenant")

// TenantMember is one member of a tenant together with the email a
// notification is addressed to (users.email) - the recipient shape the
// notification fan-out needs (notification-preferences NOTIFPREF-04/07).
// Role filtering (owner/operator) is the caller's policy, not this query's.
type TenantMember struct {
	UserID string
	Email  string
	Role   string
}

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
		"SELECT tm.user_id, tm.tenant_id, tm.role, tm.created_at, t.name, t.plan FROM tenant_memberships tm JOIN tenants t ON t.id = tm.tenant_id WHERE tm.user_id = $1 ORDER BY tm.created_at ASC",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list tenant memberships for user: %w", err)
	}
	defer rows.Close()

	var memberships []TenantMembership
	for rows.Next() {
		var m TenantMembership
		if err := rows.Scan(&m.UserID, &m.TenantID, &m.Role, &m.CreatedAt, &m.Name, &m.Plan); err != nil {
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

// ListMembersWithEmail returns every member of tenantID with their email
// (joined from users), for the notification fan-out. Requires app.tenant_id
// set to tenantID. Role filtering is left to the caller.
func (r *TenantMembershipRepository) ListMembersWithEmail(ctx context.Context, tenantID string) ([]TenantMember, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT tm.user_id, u.email, tm.role
		   FROM tenant_memberships tm
		   JOIN users u ON u.id = tm.user_id
		  WHERE tm.tenant_id = $1
		  ORDER BY tm.created_at ASC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list tenant members with email: %w", err)
	}
	defer rows.Close()

	var members []TenantMember
	for rows.Next() {
		var m TenantMember
		if err := rows.Scan(&m.UserID, &m.Email, &m.Role); err != nil {
			return nil, fmt.Errorf("db: failed to scan tenant member: %w", err)
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: error iterating tenant members: %w", err)
	}
	return members, nil
}

// GetRole returns the role userID holds in tenantID, or ErrNotFound if
// they hold no membership there. This is what resolves a request's
// effective role: since multi-tenancy-core a role is per tenant
// (tenant_memberships.role), never a property of the user.
func (r *TenantMembershipRepository) GetRole(ctx context.Context, userID, tenantID string) (string, error) {
	var role string
	row := r.pool.QueryRow(ctx,
		"SELECT role FROM tenant_memberships WHERE user_id = $1 AND tenant_id = $2",
		userID, tenantID,
	)
	if err := row.Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("db: failed to get tenant membership role: %w", err)
	}
	return role, nil
}

// UpdateRole sets a new role for the (userID, tenantID) membership,
// returning ErrNotFound if no such membership exists, or ErrLastOwner - no
// row changed - if the change would demote the tenant's only owner. Same
// lockout protection, and the same FOR UPDATE race-safety shape, as
// Delete.
func (r *TenantMembershipRepository) UpdateRole(ctx context.Context, userID, tenantID, role string) error {
	if role != RoleOwner {
		currentRole, err := r.GetRole(ctx, userID, tenantID)
		if err != nil {
			return err
		}
		if currentRole == RoleOwner {
			lastOwner, err := r.isLastOwner(ctx, tenantID)
			if err != nil {
				return err
			}
			if lastOwner {
				return ErrLastOwner
			}
		}
	}

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
// until this call's transaction ends - see isLastOwner.
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
		lastOwner, err := r.isLastOwner(ctx, tenantID)
		if err != nil {
			return err
		}
		if lastOwner {
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

// isLastOwner reports whether tenantID currently has at most one owner. It
// runs SELECT ... FOR UPDATE against that tenant's owner rows, so a
// concurrent Delete/UpdateRole affecting the same tenant's owners blocks
// until this call's transaction ends - the count a lockout decision is
// based on can't go stale between the check and the write.
func (r *TenantMembershipRepository) isLastOwner(ctx context.Context, tenantID string) (bool, error) {
	var ownerCount int
	row := r.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM (SELECT user_id FROM tenant_memberships WHERE tenant_id = $1 AND role = $2 FOR UPDATE) locked_owners",
		tenantID, RoleOwner,
	)
	if err := row.Scan(&ownerCount); err != nil {
		return false, fmt.Errorf("db: failed to count tenant owners: %w", err)
	}
	return ownerCount <= 1, nil
}

// CountForUser returns how many tenants userID is still a member of. The
// admin-removal path uses it to decide whether removing a membership left
// the user with no way into the product at all (in which case the account
// row itself goes too, preserving the pre-multi-tenancy behaviour of
// "removing an admin removes the account"), or whether they remain a
// member somewhere else and must be left alone.
func (r *TenantMembershipRepository) CountForUser(ctx context.Context, userID string) (int, error) {
	var count int
	row := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM tenant_memberships WHERE user_id = $1", userID)
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("db: failed to count tenant memberships for user: %w", err)
	}
	return count, nil
}
