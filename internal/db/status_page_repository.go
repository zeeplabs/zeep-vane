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

// ErrDomainAlreadyAttached is returned by AttachDomain when the target
// status page already has a non-null domain_id.
var ErrDomainAlreadyAttached = errors.New("db: status page already has a domain attached")

// ErrInvalidDomain is returned by AttachDomain when the given domain_id
// does not reference an existing Domain.
var ErrInvalidDomain = errors.New("db: domain_id does not reference an existing domain")

// ErrDuplicateDomainSubdomain is returned by AttachDomain when the given
// (domain_id, subdomain) pair is already used by another status page.
var ErrDuplicateDomainSubdomain = errors.New("db: this domain/subdomain pair is already in use")

// ErrNoDomainAttached is returned by PublicHostnameByID when the target
// status page has no domain_id/subdomain set yet.
var ErrNoDomainAttached = errors.New("db: status page has no domain attached")

// pendingTLSState is the StatusPage.State value AttachDomain moves a page
// to (was left at "draft" - a bug: tls.HostPolicy rejects ACME issuance
// for any hostname whose state is still "draft", but only MarkPublished/
// MarkTLSFailed - both reachable only via a HostPolicy-approved ACME
// attempt - ever moved it off "draft". AttachDomain never changed state,
// so every status page was permanently stuck: HostPolicy always found
// "draft" and always refused, so ACME was never attempted, so the state
// could never change. See migration 0020 for the one-time data fix on
// existing rows already stuck this way.
const pendingTLSState = "pending_tls"

// StatusPage is a public status page published at Subdomain under Domain,
// showing the linked services' current status. Subdomain and DomainID are
// both nullable: a status page can be created with no domain attached
// (SPD-01) and gets both set together, exactly once, by AttachDomain.
type StatusPage struct {
	ID string
	// TenantID is the tenant this status page belongs to (0024). Only
	// GetByHostname populates it: it is the one lookup that runs before any
	// tenant context exists (an anonymous visitor has no session), so the
	// row itself is what tells router.HostRouter which tenant to scope the
	// rest of the request to.
	TenantID     string
	Name         string
	Subdomain    *string
	DomainID     *string
	State        string
	TLSLastError *string
	// Environment is a purely visual, non-functional banner tag shown on
	// the public status page (status-page-environment-banner SPEB-01):
	// "" (default - no banner, production implied), "staging", or
	// "development". Enforced against those three values by 0048's CHECK
	// constraint, not by this type - same ""-means-absent convention as
	// Service.Environment/Service.MonitorMode.
	Environment string
	CreatedAt   time.Time
	ServiceIDs  []string
}

// StatusPageRepository accesses the status_pages and status_page_services
// tables.
type StatusPageRepository struct {
	pool *Pool
}

// NewStatusPageRepository builds a StatusPageRepository backed by pool.
func NewStatusPageRepository(pool *Pool) *StatusPageRepository {
	return &StatusPageRepository{pool: pool}
}

// Create inserts statusPage and links it to every service in serviceIDs,
// filling in statusPage's generated ID, State ("draft" - the DB default -
// SP-15), and CreatedAt. The insert and the service links are wrapped in a
// single transaction: a status page is never left without its intended
// service links because a later insert failed partway through.
//
// If ctx already carries a tenant transaction (db.WithTenantTx - e.g. a
// request inside api.TenantContext, or router.HostRouter's public path),
// Create runs on that transaction instead of opening its own, following
// TenantRepository.Create's convention. This is required, not just tidier:
// status_pages.tenant_id defaults from current_setting('app.tenant_id'),
// which is SET LOCAL on the caller's transaction (0024), so a second,
// independently pooled transaction would resolve that DEFAULT to NULL and
// the insert would fail the NOT NULL constraint.
func (r *StatusPageRepository) Create(ctx context.Context, statusPage *StatusPage, serviceIDs []string) error {
	if _, ok := TenantTxFromContext(ctx); ok {
		return r.insert(ctx, statusPage, serviceIDs)
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: failed to begin status page create transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := r.insert(WithTenantTx(ctx, tx), statusPage, serviceIDs); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: failed to commit status page create transaction: %w", err)
	}

	return nil
}

// insert performs Create's writes on whatever transaction ctx carries.
func (r *StatusPageRepository) insert(ctx context.Context, statusPage *StatusPage, serviceIDs []string) error {
	row := r.pool.QueryRow(ctx,
		"INSERT INTO status_pages (name, subdomain, domain_id, environment) VALUES ($1, $2, $3, $4) "+
			"RETURNING id, state, created_at",
		statusPage.Name, statusPage.Subdomain, statusPage.DomainID, statusPage.Environment,
	)
	if err := row.Scan(&statusPage.ID, &statusPage.State, &statusPage.CreatedAt); err != nil {
		return fmt.Errorf("db: failed to create status page: %w", err)
	}

	for _, serviceID := range serviceIDs {
		if _, err := r.pool.Exec(ctx,
			"INSERT INTO status_page_services (status_page_id, service_id) VALUES ($1, $2)",
			statusPage.ID, serviceID,
		); err != nil {
			return fmt.Errorf("db: failed to link service %s to status page: %w", serviceID, err)
		}
	}

	statusPage.ServiceIDs = serviceIDs
	return nil
}

// SetServices replaces the full set of services linked to the status page
// identified by statusPageID with placements, inside a single transaction
// (delete all existing links, then insert the given set) - the same
// replace-all semantics as Create's initial linking, so a partial update
// never leaves a mix of old and new links, and dropping a previously-set
// GroupID/ParentServiceID for a service clears it rather than preserving
// it (there is no merge). It returns ErrNotFound if no status page matches
// statusPageID.
//
// Before touching the DB, it loads this page's valid group ids (a
// lighter, tx-scoped id-only query rather than ListGroups - T4 only needs
// the id set for validateAndSanitizeServicePlacements, not the full
// StatusPageServiceGroup rows, and running it on the same tx as the
// existence check/delete/insert keeps the whole read-then-write under one
// connection instead of borrowing a second one from the pool) and calls
// validateAndSanitizeServicePlacements (SGD-08, SGD-10, SGD-11, SGD-12,
// SGD-13). On a
// validation error, the transaction is rolled back untouched (deferred
// Rollback) and that sentinel error is returned directly - no partial
// writes.
func (r *StatusPageRepository) SetServices(ctx context.Context, statusPageID string, placements []ServicePlacement) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: failed to begin set services transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var exists bool
	row := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM status_pages WHERE id = $1)", statusPageID)
	if err := row.Scan(&exists); err != nil {
		return fmt.Errorf("db: failed to check status page existence: %w", err)
	}
	if !exists {
		return ErrNotFound
	}

	validGroupIDs, err := r.validGroupIDsTx(ctx, tx, statusPageID)
	if err != nil {
		return err
	}

	existingServiceIDs, err := r.existingServiceIDsTx(ctx, tx, statusPageID)
	if err != nil {
		return err
	}

	sanitizedPlacements, err := validateAndSanitizeServicePlacements(placements, validGroupIDs, existingServiceIDs)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, "DELETE FROM status_page_services WHERE status_page_id = $1", statusPageID); err != nil {
		return fmt.Errorf("db: failed to clear existing service links: %w", err)
	}
	for _, placement := range sanitizedPlacements {
		if _, err := tx.Exec(ctx,
			"INSERT INTO status_page_services (status_page_id, service_id, group_id, parent_service_id, position) "+
				"VALUES ($1, $2, $3, $4, $5)",
			statusPageID, placement.ServiceID, placement.GroupID, placement.ParentServiceID, placement.Position,
		); err != nil {
			return fmt.Errorf("db: failed to link service %s to status page: %w", placement.ServiceID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: failed to commit set services transaction: %w", err)
	}

	return nil
}

// validGroupIDsTx loads the set of status_page_service_groups ids
// belonging to statusPageID, scoped to tx, as a map suitable for
// validateAndSanitizeServicePlacements. It exists instead of reusing
// ListGroups
// because SetServices only needs the id set, not the full
// StatusPageServiceGroup rows (name/position/created_at), and running it
// on tx - rather than ListGroups' r.pool - keeps the read on the same
// connection/transaction as the existence check and the delete+insert
// that follow.
func (r *StatusPageRepository) validGroupIDsTx(ctx context.Context, tx pgx.Tx, statusPageID string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, "SELECT id FROM status_page_service_groups WHERE status_page_id = $1", statusPageID)
	if err != nil {
		return nil, fmt.Errorf("db: failed to load valid group ids: %w", err)
	}
	defer rows.Close()

	validGroupIDs := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("db: failed to scan group id: %w", err)
		}
		validGroupIDs[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate valid group ids: %w", err)
	}

	return validGroupIDs, nil
}

// existingServiceIDsTx loads the set of service ids currently linked to
// statusPageID via status_page_services, scoped to tx, as it stands *before*
// SetServices' delete+insert runs. validateAndSanitizeServicePlacements uses
// this to tell apart two structurally identical cases for a dangling
// ParentServiceID (SGD-13 vs SGD-11): a parent this very call is removing
// from the page (it was linked before, isn't in the new placements) is not
// an error, while a parent that was never linked to this page at all still
// is.
func (r *StatusPageRepository) existingServiceIDsTx(ctx context.Context, tx pgx.Tx, statusPageID string) (map[string]bool, error) {
	rows, err := tx.Query(ctx, "SELECT service_id FROM status_page_services WHERE status_page_id = $1", statusPageID)
	if err != nil {
		return nil, fmt.Errorf("db: failed to load existing service ids: %w", err)
	}
	defer rows.Close()

	existingServiceIDs := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("db: failed to scan existing service id: %w", err)
		}
		existingServiceIDs[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate existing service ids: %w", err)
	}

	return existingServiceIDs, nil
}

// ErrGroupCrossPage is returned by validateAndSanitizeServicePlacements when a
// placement's GroupID references a group that does not belong to the
// status page being written to (SGD-11).
var ErrGroupCrossPage = errors.New("db: group does not belong to this status page")

// ErrParentNotInSet is returned by validateAndSanitizeServicePlacements when a
// placement's ParentServiceID does not match any ServiceID present in the
// same placements slice.
var ErrParentNotInSet = errors.New("db: parent service is not part of this status page's service set")

// ErrSelfReference is returned by validateAndSanitizeServicePlacements when a
// placement's ParentServiceID equals its own ServiceID (SGD-12). Checked
// ahead of the "parent not in set" rule: a self-reference technically also
// fails "ParentServiceID must match some OTHER placement's ServiceID", but
// spec.md's P2-AC6 calls for a distinct, more specific error for this case
// rather than the generic "not in set" one, so it gets its own dedicated
// check instead of falling through to ErrParentNotInSet.
var ErrSelfReference = errors.New("db: service cannot be its own parent")

// ErrMaxDepthExceeded is returned by validateAndSanitizeServicePlacements when a
// placement that is itself used as another placement's parent also has a
// non-nil ParentServiceID of its own - i.e. it would be both a parent and
// a child at once. Nesting is capped at depth 2 (parent -> child, never
// parent -> child -> grandchild), per spec.md's explicit Out-of-Scope item
// and P2-AC2.
var ErrMaxDepthExceeded = errors.New("db: nesting depth is capped at parent and child, no grandchildren")

// ServicePlacement describes where a single service sits within a status
// page's service list: which group (if any) it belongs to, which service
// (if any) is its parent, and its display position. It is the input to
// validateAndSanitizeServicePlacements and (T4) SetServices.
type ServicePlacement struct {
	ServiceID       string
	GroupID         *string
	ParentServiceID *string
	Position        int
}

// validateAndSanitizeServicePlacements enforces the placement rules for a
// status page's full set of service placements (SGD-08, SGD-10, SGD-11,
// SGD-12, SGD-13):
//
//  0. (SGD-13) first, any non-nil ParentServiceID that is absent from this
//     same placements slice but present in existingServiceIDs (i.e. it was
//     linked to this status page before this call, and this very call is
//     removing it) is cleared to nil instead of being treated as an error -
//     the parent is being removed from the page, so its children become
//     parentless, not dangling or blocking the save. Migration 0046's
//     parent_service_id ON DELETE SET NULL FK applies this same
//     never-block-on-a-vanished-parent intent when the service row itself
//     is deleted outright; this is that same intent for "removed from this
//     page" rather than "service deleted". A self-reference (ParentServiceID
//     == that placement's own ServiceID) is left untouched here - rule 3
//     below still rejects it, it must never silently become parentless.
//     This step operates on, and returns, a copy - placements itself is
//     never mutated.
//  1. every non-nil GroupID must reference a group that belongs to this
//     status page (validGroupIDs), else ErrGroupCrossPage;
//  2. every remaining non-nil ParentServiceID must equal some OTHER
//     placement's ServiceID in the same slice, else ErrParentNotInSet
//     (SGD-11: a reference that was never valid for this page at all, not
//     just-removed by this call) - except a self-reference (rule 3) is
//     checked first and wins over this one;
//  3. ParentServiceID must never equal that same placement's own
//     ServiceID, else ErrSelfReference;
//  4. no placement that is itself used as another placement's
//     ParentServiceID (i.e. it is somebody's parent) may also carry a
//     non-nil ParentServiceID of its own (i.e. be somebody else's child),
//     else ErrMaxDepthExceeded - this caps nesting at depth 2.
//
// Rules are checked in that order per placement, so the error returned for
// a placement that violates more than one rule is deterministic. This is a
// pure function with no DB access - callers (T4's SetServices) are
// responsible for loading validGroupIDs and existingServiceIDs first.
func validateAndSanitizeServicePlacements(placements []ServicePlacement, validGroupIDs, existingServiceIDs map[string]bool) ([]ServicePlacement, error) {
	submitted := make(map[string]bool, len(placements)) // ServiceID -> present in this call's own placements
	for _, p := range placements {
		submitted[p.ServiceID] = true
	}

	sanitized := make([]ServicePlacement, len(placements))
	copy(sanitized, placements)
	for i, p := range sanitized {
		if p.ParentServiceID == nil || *p.ParentServiceID == p.ServiceID {
			continue // nil, or a self-reference left for rule 3 below
		}
		if !submitted[*p.ParentServiceID] && existingServiceIDs[*p.ParentServiceID] {
			sanitized[i].ParentServiceID = nil // SGD-13: parent removed by this call
		}
	}

	parentOf := make(map[string]bool, len(sanitized)) // ServiceID -> is some other (sanitized) placement's parent
	for _, p := range sanitized {
		if p.ParentServiceID != nil {
			parentOf[*p.ParentServiceID] = true
		}
	}

	for _, p := range sanitized {
		if p.GroupID != nil && !validGroupIDs[*p.GroupID] {
			return nil, ErrGroupCrossPage
		}

		if p.ParentServiceID != nil {
			if *p.ParentServiceID == p.ServiceID {
				return nil, ErrSelfReference
			}
			if !submitted[*p.ParentServiceID] {
				return nil, ErrParentNotInSet
			}
		}

		if parentOf[p.ServiceID] && p.ParentServiceID != nil {
			return nil, ErrMaxDepthExceeded
		}
	}

	return sanitized, nil
}

// StatusPageServiceGroup is a named, ordered grouping of services shown
// together on a status page (SGD-01, migration 0046). Position determines
// display order among a status page's own groups.
type StatusPageServiceGroup struct {
	ID           string
	StatusPageID string
	Name         string
	Position     int
	CreatedAt    time.Time
}

// CreateGroup inserts a new StatusPageServiceGroup for statusPageID, named
// name, with Position set to one past the highest position already used on
// that page (COALESCE(MAX(position), -1) + 1 - the standard fix for this
// class of bug), so a freshly created group always appends after the
// existing ones (SGD-01). This previously used COUNT(*), which produces a
// duplicate position - and therefore an ambiguous display order between
// two groups - as soon as a group in the middle of the sequence is ever
// deleted: create G0/G1/G2 (positions 0/1/2), delete G0, create G3 ->
// COUNT(*) is 2, the same position G2 already has. MAX(position)+1 doesn't
// have this problem since it never reuses a position a surviving row still
// holds. The MAX and the INSERT run as one statement (a correlated
// subquery, not two round trips) to narrow, though not fully eliminate
// without a serializable transaction, the race between two concurrent
// CreateGroup calls on the same status page both computing the same next
// position before either commits.
func (r *StatusPageRepository) CreateGroup(ctx context.Context, statusPageID, name string) (*StatusPageServiceGroup, error) {
	group := &StatusPageServiceGroup{StatusPageID: statusPageID, Name: name}
	row := r.pool.QueryRow(ctx,
		"INSERT INTO status_page_service_groups (status_page_id, name, position) "+
			"VALUES ($1, $2, (SELECT COALESCE(MAX(position), -1) + 1 FROM status_page_service_groups WHERE status_page_id = $1)) "+
			"RETURNING id, position, created_at",
		statusPageID, name,
	)
	if err := row.Scan(&group.ID, &group.Position, &group.CreatedAt); err != nil {
		return nil, fmt.Errorf("db: failed to create status page service group: %w", err)
	}

	return group, nil
}

// UpdateGroup renames and/or repositions the status page service group
// identified by id, scoped to both tenantID and statusPageID. It returns
// ErrNotFound if no group matches id, if id matches a group that belongs
// to a different status page, or if id matches a group that belongs to a
// different tenant - the caller must not be able to tell any of these
// apart, since doing so would leak cross-tenant/cross-page existence
// information (security review finding: UpdateGroup previously filtered
// by "WHERE id = $1" alone, letting an authenticated operator of one
// tenant mutate another tenant's group by guessing/reading its id off
// that tenant's public status page).
//
// The explicit "AND tenant_id = $N" filter is deliberate, not merely
// redundant with RLS (migration 0049): it mirrors every other
// tenant-scoped repository method in this codebase (e.g.
// TenantInviteRepository.Refresh) in filtering by tenant_id explicitly
// rather than relying solely on the session-level RLS policy to narrow an
// otherwise-unscoped query. RLS is the hard, fail-closed backstop for a
// query that forgets this filter; this filter is what makes the isolation
// guarantee independent of which role the connection happens to run as -
// notably, a Postgres superuser bypasses FORCE ROW LEVEL SECURITY
// entirely, which is exactly the role this codebase's own disposable
// integration-test container connects as (see rls_test.go's rlsTestRole
// comment), so a query relying on RLS alone would silently pass bypassed
// in both environments without ever being exercised. The status_page_id
// filter is still separately required too: two status pages in the same
// tenant must not be able to cross-mutate each other's groups, which
// tenant_id scoping alone does not prevent.
func (r *StatusPageRepository) UpdateGroup(ctx context.Context, tenantID, statusPageID, id, name string, position int) (*StatusPageServiceGroup, error) {
	row := r.pool.QueryRow(ctx,
		"UPDATE status_page_service_groups SET name = $4, position = $5 "+
			"WHERE id = $1 AND status_page_id = $2 AND tenant_id = $3 "+
			"RETURNING id, status_page_id, name, position, created_at",
		id, statusPageID, tenantID, name, position,
	)

	var group StatusPageServiceGroup
	if err := row.Scan(&group.ID, &group.StatusPageID, &group.Name, &group.Position, &group.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to update status page service group: %w", err)
	}

	return &group, nil
}

// DeleteGroup removes the status page service group identified by id,
// scoped to both tenantID and statusPageID (see UpdateGroup's doc comment
// on why both filters are required in addition to RLS). Any
// status_page_services row whose group_id referenced it is left in place
// with group_id set to NULL by the column's ON DELETE SET NULL foreign key
// (migration 0046), not by Go code here. It returns ErrNotFound if no
// group matches id within tenantID and statusPageID.
func (r *StatusPageRepository) DeleteGroup(ctx context.Context, tenantID, statusPageID, id string) error {
	tag, err := r.pool.Exec(ctx,
		"DELETE FROM status_page_service_groups WHERE id = $1 AND status_page_id = $2 AND tenant_id = $3",
		id, statusPageID, tenantID)
	if err != nil {
		return fmt.Errorf("db: failed to delete status page service group: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// ListGroups returns every StatusPageServiceGroup belonging to
// statusPageID, ordered by position, with id as a deterministic tie-break
// (CreateGroup's COALESCE(MAX(position), -1)+1 formula makes a duplicate
// position far rarer than the old COUNT(*) one, but two groups can still
// legitimately share a position after a concurrent CreateGroup race or a
// manual UpdateGroup - without the tie-break, Postgres is free to return
// them in either order on different calls, which would make the group
// list visibly reorder itself between page loads for no reason a user did).
func (r *StatusPageRepository) ListGroups(ctx context.Context, statusPageID string) ([]StatusPageServiceGroup, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id, status_page_id, name, position, created_at FROM status_page_service_groups "+
			"WHERE status_page_id = $1 ORDER BY position, id",
		statusPageID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list status page service groups: %w", err)
	}
	defer rows.Close()

	var groups []StatusPageServiceGroup
	for rows.Next() {
		var group StatusPageServiceGroup
		if err := rows.Scan(&group.ID, &group.StatusPageID, &group.Name, &group.Position, &group.CreatedAt); err != nil {
			return nil, fmt.Errorf("db: failed to scan status page service group: %w", err)
		}
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate status page service groups: %w", err)
	}

	return groups, nil
}

// serviceIDsByStatusPage batch-loads every status_page_services link for
// pageIDs into a map, so List can attach ServiceIDs to each row with one
// extra query instead of one per page.
func (r *StatusPageRepository) serviceIDsByStatusPage(ctx context.Context, pageIDs []string) (map[string][]string, error) {
	result := make(map[string][]string, len(pageIDs))
	if len(pageIDs) == 0 {
		return result, nil
	}

	rows, err := r.pool.Query(ctx,
		"SELECT status_page_id, service_id FROM status_page_services WHERE status_page_id = ANY($1)", pageIDs)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list service links: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var statusPageID, serviceID string
		if err := rows.Scan(&statusPageID, &serviceID); err != nil {
			return nil, fmt.Errorf("db: failed to scan service link: %w", err)
		}
		result[statusPageID] = append(result[statusPageID], serviceID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to list service links: %w", err)
	}

	return result, nil
}

// ListServicePlacements returns the full per-service placement detail
// (service_id, group_id, parent_service_id, position) for every service
// linked to statusPageID, position-ordered (T7: GET
// /api/status-pages/{id}/services). Unlike serviceIDsByStatusPage, which
// only returns bare service IDs for the unchanged GET /api/status-pages/{id}
// response, this exposes each placement's group/parent/position so the
// admin tree editor can render it alongside T5's ListGroups. A page with no
// linked services returns an empty (nil) slice, not an error.
func (r *StatusPageRepository) ListServicePlacements(ctx context.Context, statusPageID string) ([]ServicePlacement, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT service_id, group_id, parent_service_id, position FROM status_page_services "+
			"WHERE status_page_id = $1 ORDER BY position",
		statusPageID,
	)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list status page service placements: %w", err)
	}
	defer rows.Close()

	var placements []ServicePlacement
	for rows.Next() {
		var p ServicePlacement
		if err := rows.Scan(&p.ServiceID, &p.GroupID, &p.ParentServiceID, &p.Position); err != nil {
			return nil, fmt.Errorf("db: failed to scan status page service placement: %w", err)
		}
		placements = append(placements, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to iterate status page service placements: %w", err)
	}

	return placements, nil
}

// AttachedNamesByDomainIDs batch-loads, per domain ID in domainIDs, the
// names of every status page whose domain_id matches it - ordered
// created_at ASC so the caller (DomainsHandler.List) can treat the first
// name in each slice as the "primary" attached page (design.md's "Aponta
// para" column, spec.md DSP-03/DSP-04). A domain with zero attached pages
// is simply absent from the returned map, matching serviceIDsByStatusPage's
// convention. An empty domainIDs slice returns an empty map with no query
// executed, so DomainsHandler.List can call this unconditionally even on
// an empty page.
func (r *StatusPageRepository) AttachedNamesByDomainIDs(ctx context.Context, domainIDs []string) (map[string][]string, error) {
	result := make(map[string][]string, len(domainIDs))
	if len(domainIDs) == 0 {
		return result, nil
	}

	rows, err := r.pool.Query(ctx,
		"SELECT domain_id, name FROM status_pages WHERE domain_id = ANY($1) ORDER BY domain_id, created_at ASC", domainIDs)
	if err != nil {
		return nil, fmt.Errorf("db: failed to list attached status page names: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var domainID, name string
		if err := rows.Scan(&domainID, &name); err != nil {
			return nil, fmt.Errorf("db: failed to scan attached status page name: %w", err)
		}
		result[domainID] = append(result[domainID], name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: failed to list attached status page names: %w", err)
	}

	return result, nil
}

// AttachDomain sets domain_id/subdomain on the status page identified by
// id, exactly once (SPD-06). It locks the target row with SELECT ... FOR
// UPDATE inside an explicit transaction, rather than a conditional UPDATE
// ... WHERE domain_id IS NULL, because the row lock lets a single query
// resolve which of 4 distinguishable outcomes applies before deciding what
// to do - a conditional UPDATE affecting 0 rows can't tell "page doesn't
// exist" apart from "page already has a domain" (design.md Tech
// Decisions).
//
// It returns ErrNotFound if no status page matches id, ErrDomainAlreadyAttached
// if the page's domain_id is already non-null (SPD-07), ErrInvalidDomain if
// domainID does not reference an existing Domain (SPD-07), or
// ErrDuplicateDomainSubdomain if the (domainID, subdomain) pair is already
// used by another status page (SPD-09, enforced by the partial unique index
// added in migration 0013). On any error the row is left unmodified.
func (r *StatusPageRepository) AttachDomain(ctx context.Context, id, domainID, subdomain string) (*StatusPage, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: failed to begin attach domain transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var currentDomainID *string
	row := tx.QueryRow(ctx, "SELECT domain_id FROM status_pages WHERE id = $1 FOR UPDATE", id)
	if err := row.Scan(&currentDomainID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to lock status page for domain attach: %w", err)
	}
	if currentDomainID != nil {
		return nil, ErrDomainAlreadyAttached
	}

	var sp StatusPage
	sp.ID = id
	row = tx.QueryRow(ctx,
		"UPDATE status_pages SET domain_id = $1, subdomain = $2, state = $3 WHERE id = $4 "+
			"RETURNING name, subdomain, domain_id, state, tls_last_error, environment, created_at",
		domainID, subdomain, pendingTLSState, id,
	)
	if err := row.Scan(&sp.Name, &sp.Subdomain, &sp.DomainID, &sp.State, &sp.TLSLastError, &sp.Environment, &sp.CreatedAt); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case pgerrcode.ForeignKeyViolation:
				return nil, ErrInvalidDomain
			case pgerrcode.UniqueViolation:
				return nil, ErrDuplicateDomainSubdomain
			}
		}
		return nil, fmt.Errorf("db: failed to attach domain to status page: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("db: failed to commit attach domain transaction: %w", err)
	}

	return &sp, nil
}

// UpdateEnvironment sets the public banner environment tag
// (status-page-environment-banner SPEB-02) on the status page identified by
// id to one of "", "staging", "development" - the handler validates the
// value against that fixed enum before calling this, enforced here too by
// 0048's CHECK constraint as defense in depth. Unlike Service.Environment
// (a visual-only admin tag with no public exposure), this value also drives
// PublicStatusHandler's banner (SPEB-04/05). Returns ErrNotFound if no
// status page matches id.
func (r *StatusPageRepository) UpdateEnvironment(ctx context.Context, id, environment string) (*StatusPage, error) {
	var sp StatusPage
	sp.ID = id
	row := r.pool.QueryRow(ctx,
		"UPDATE status_pages SET environment = $2 WHERE id = $1 "+
			"RETURNING name, subdomain, domain_id, state, tls_last_error, environment, created_at",
		id, environment,
	)
	if err := row.Scan(&sp.Name, &sp.Subdomain, &sp.DomainID, &sp.State, &sp.TLSLastError, &sp.Environment, &sp.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to update status page environment: %w", err)
	}
	return &sp, nil
}

// ListPaginated returns one page of registered status pages, ordered by
// name, each with its linked service_ids (PAG-08). total is computed via
// COUNT(*) OVER() in the same query, with a zero-row fallback COUNT(*),
// same pattern as Incident/Domain/ServiceRepository's ListPaginated. The
// serviceIDsByStatusPage batch lookup now runs against only this page's
// IDs instead of every status page in the table.
func (r *StatusPageRepository) ListPaginated(ctx context.Context, page, pageSize int) ([]StatusPage, int, error) {
	offset := (page - 1) * pageSize

	rows, err := r.pool.Query(ctx,
		`SELECT id, name, subdomain, domain_id, state, tls_last_error, environment, created_at, COUNT(*) OVER() AS total
		 FROM status_pages
		 ORDER BY name
		 LIMIT $1 OFFSET $2`,
		pageSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("db: failed to list status pages: %w", err)
	}
	defer rows.Close()

	statusPages := []StatusPage{}
	total := 0
	for rows.Next() {
		var sp StatusPage
		if err := rows.Scan(&sp.ID, &sp.Name, &sp.Subdomain, &sp.DomainID, &sp.State, &sp.TLSLastError, &sp.Environment, &sp.CreatedAt, &total); err != nil {
			return nil, 0, fmt.Errorf("db: failed to scan status page: %w", err)
		}
		statusPages = append(statusPages, sp)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("db: failed to iterate status pages: %w", err)
	}

	if len(statusPages) == 0 {
		total, err = r.countStatusPages(ctx)
		if err != nil {
			return nil, 0, err
		}
	}

	pageIDs := make([]string, len(statusPages))
	for i, sp := range statusPages {
		pageIDs[i] = sp.ID
	}
	serviceIDsByPage, err := r.serviceIDsByStatusPage(ctx, pageIDs)
	if err != nil {
		return nil, 0, err
	}
	for i := range statusPages {
		statusPages[i].ServiceIDs = serviceIDsByPage[statusPages[i].ID]
	}

	return statusPages, total, nil
}

// Delete removes the status page identified by id, along with its
// status_page_services links and status_page_service_groups (both deleted
// first, in the same transaction, since neither table has an ON DELETE
// CASCADE from status_pages - 0046). It returns ErrNotFound if no status
// page matches id.
func (r *StatusPageRepository) Delete(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: failed to begin delete status page transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "DELETE FROM status_page_services WHERE status_page_id = $1", id); err != nil {
		return fmt.Errorf("db: failed to clear service links before delete: %w", err)
	}

	if _, err := tx.Exec(ctx, "DELETE FROM status_page_service_groups WHERE status_page_id = $1", id); err != nil {
		return fmt.Errorf("db: failed to clear service groups before delete: %w", err)
	}

	tag, err := tx.Exec(ctx, "DELETE FROM status_pages WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("db: failed to delete status page: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: failed to commit delete status page transaction: %w", err)
	}

	return nil
}

// PublicHostnameByID returns the full public hostname (subdomain + root
// domain, same shape hostnameMatch/StateByHostname join on) for the status
// page identified by id - used by the admin-facing domain verification
// endpoint to know what hostname to check DNS/TLS for. It returns
// ErrNotFound if no status page matches id, or ErrNoDomainAttached if the
// page exists but has no domain_id/subdomain set yet.
func (r *StatusPageRepository) PublicHostnameByID(ctx context.Context, id string) (string, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT sp.subdomain || '.' || d.hostname FROM status_pages sp "+
			"LEFT JOIN domains d ON d.id = sp.domain_id WHERE sp.id = $1",
		id,
	)

	var hostname *string
	if err := row.Scan(&hostname); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("db: failed to get public hostname by id: %w", err)
	}
	if hostname == nil {
		return "", ErrNoDomainAttached
	}

	return *hostname, nil
}

// countStatusPages is the zero-row fallback for ListPaginated's total
// (PAG-08).
func (r *StatusPageRepository) countStatusPages(ctx context.Context) (int, error) {
	var total int
	row := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM status_pages")
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("db: failed to count status pages: %w", err)
	}
	return total, nil
}

// GetByID returns the full StatusPage row with id. It returns ErrNotFound
// if no status page matches.
func (r *StatusPageRepository) GetByID(ctx context.Context, id string) (*StatusPage, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT id, name, subdomain, domain_id, state, tls_last_error, environment, created_at FROM status_pages WHERE id = $1",
		id,
	)

	var sp StatusPage
	if err := row.Scan(&sp.ID, &sp.Name, &sp.Subdomain, &sp.DomainID, &sp.State, &sp.TLSLastError, &sp.Environment, &sp.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get status page by id: %w", err)
	}

	serviceIDsByPage, err := r.serviceIDsByStatusPage(ctx, []string{sp.ID})
	if err != nil {
		return nil, err
	}
	sp.ServiceIDs = serviceIDsByPage[sp.ID]

	return &sp, nil
}

// hostnameMatch is the shared WHERE-clause fragment matching a status
// page's public hostname: its subdomain joined to its domain's root
// hostname (e.g. "status" + "empresa.com" = "status.empresa.com").
const hostnameMatch = "d.id = sp.domain_id AND sp.subdomain || '.' || d.hostname = $1"

// StateByHostname returns the State of the status page published at
// hostname, joining status_pages to its parent domain. It returns
// ErrNotFound if no status page matches - CertMagic's HostPolicy (T30)
// treats that the same as any other lookup failure: reject the ACME
// request.
func (r *StatusPageRepository) StateByHostname(ctx context.Context, hostname string) (string, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT sp.state FROM status_pages sp JOIN domains d ON "+hostnameMatch,
		hostname,
	)

	var state string
	if err := row.Scan(&state); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("db: failed to get status page state by hostname: %w", err)
	}

	return state, nil
}

// GetByHostname returns the full StatusPage row published at hostname,
// joining status_pages to its parent domain. It returns ErrNotFound if no
// status page matches. Unlike StateByHostname (used only by tls.HostPolicy,
// which needs just the state to gate ACME issuance), this returns the
// StatusPage's ID and TenantID too, so callers like router.HostRouter can
// thread the ID down to the scoped public queries that implement SP-15 (a
// status page shows only its own linked services/incidents) and the tenant
// id into the request's RLS session settings (0024) - the hostname is the
// only tenant signal an anonymous visitor carries.
func (r *StatusPageRepository) GetByHostname(ctx context.Context, hostname string) (*StatusPage, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT sp.id, sp.tenant_id, sp.name, sp.subdomain, sp.domain_id, sp.state, sp.tls_last_error, sp.environment, sp.created_at "+
			"FROM status_pages sp JOIN domains d ON "+hostnameMatch,
		hostname,
	)

	var sp StatusPage
	if err := row.Scan(&sp.ID, &sp.TenantID, &sp.Name, &sp.Subdomain, &sp.DomainID, &sp.State, &sp.TLSLastError, &sp.Environment, &sp.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("db: failed to get status page by hostname: %w", err)
	}

	return &sp, nil
}

// MarkPublished sets the status page at hostname to "published" and clears
// any prior tls_last_error (SP-12: a successful certificate issuance
// publishes the page). It returns ErrNotFound if no status page matches.
func (r *StatusPageRepository) MarkPublished(ctx context.Context, hostname string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE status_pages sp SET state = 'published', tls_last_error = NULL FROM domains d WHERE "+hostnameMatch,
		hostname,
	)
	if err != nil {
		return fmt.Errorf("db: failed to mark status page published: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkTLSFailed sets the status page at hostname to "tls_failed" and
// records reason (SP-13: a failed certificate issuance keeps the page
// unpublished and surfaces why to the admin). It returns ErrNotFound if no
// status page matches.
func (r *StatusPageRepository) MarkTLSFailed(ctx context.Context, hostname, reason string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE status_pages sp SET state = 'tls_failed', tls_last_error = $2 FROM domains d WHERE "+hostnameMatch,
		hostname, reason,
	)
	if err != nil {
		return fmt.Errorf("db: failed to mark status page tls_failed: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}
