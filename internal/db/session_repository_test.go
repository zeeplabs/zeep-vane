//go:build integration

package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// newSessionRepositoryForTest returns a SessionRepository backed by a
// pool connected to a fresh scratch database with all migrations applied.
// Using newScratchDatabase (rather than the shared TEST_DATABASE_URL)
// sidesteps state-sharing problems with other packages' tests that
// bulk-clear the shared `users` table - each test here owns its own
// database, so no locks or cleanup coordination needed.
func newSessionRepositoryForTest(t *testing.T) (*SessionRepository, *Pool) {
	t.Helper()
	dsn := newScratchDatabase(t)

	if err := MigrateUp(dsn, "migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	return NewSessionRepository(pool), pool
}

// sessionFixtureUser inserts a fixture user and returns its id; the
// Cleanup deletes the user (which ON DELETE CASCADEs every session for
// that user), so each test's session rows are cleaned up automatically
// without per-test DELETE statements against `sessions`.
func sessionFixtureUser(t *testing.T, pool *Pool) string {
	t.Helper()
	email := uniqueTestEmail(t)
	var userID string
	if err := pool.QueryRow(context.Background(),
		"INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id",
		email, "hash-sessions-repo-test",
	).Scan(&userID); err != nil {
		t.Fatalf("insert fixture user returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})
	return userID
}

// TestSessionRepository_Create_GetByID_RoundTrip exercises the basic
// create/get path: insert with UA + IP populated, read back, fields
// match byte-for-byte; insert with empty UA + empty IP, read back,
// both surfaced as sql.NullString{Valid:false} (NULL at the DB level,
// not empty strings).
func TestSessionRepository_Create_GetByID_RoundTrip(t *testing.T) {
	repo, pool := newSessionRepositoryForTest(t)
	ctx := context.Background()
	userID := sessionFixtureUser(t, pool)

	// Populated UA + IP.
	id1, err := repo.Create(ctx, userID, "Mozilla/5.0 test", "10.0.0.42")
	if err != nil {
		t.Fatalf("Create (populated) returned unexpected error: %v", err)
	}
	if id1 == "" {
		t.Fatal("Create (populated) returned empty id")
	}

	got1, err := repo.GetByID(ctx, id1)
	if err != nil {
		t.Fatalf("GetByID (populated) returned unexpected error: %v", err)
	}
	if got1.ID != id1 {
		t.Errorf("GetByID.ID = %q, want %q", got1.ID, id1)
	}
	if got1.UserID != userID {
		t.Errorf("GetByID.UserID = %q, want %q", got1.UserID, userID)
	}
	if !got1.UserAgent.Valid || got1.UserAgent.String != "Mozilla/5.0 test" {
		t.Errorf("GetByID.UserAgent = %+v, want {Valid:true, String:%q}", got1.UserAgent, "Mozilla/5.0 test")
	}
	if !got1.IP.Valid || got1.IP.String != "10.0.0.42" {
		t.Errorf("GetByID.IP = %+v, want {Valid:true, String:%q}", got1.IP, "10.0.0.42")
	}
	if got1.RevokedAt.Valid {
		t.Errorf("GetByID.RevokedAt.Valid = true, want false for fresh session")
	}

	// Empty UA + empty IP must round-trip as NULL, not "".
	id2, err := repo.Create(ctx, userID, "", "")
	if err != nil {
		t.Fatalf("Create (empty UA/IP) returned unexpected error: %v", err)
	}
	got2, err := repo.GetByID(ctx, id2)
	if err != nil {
		t.Fatalf("GetByID (empty UA/IP) returned unexpected error: %v", err)
	}
	if got2.UserAgent.Valid {
		t.Errorf("GetByID.UserAgent.Valid = true for empty input, want false (NULL)")
	}
	if got2.IP.Valid {
		t.Errorf("GetByID.IP.Valid = true for empty input, want false (NULL)")
	}

	// GetByID on a UUID nobody owns returns ErrNotFound.
	if _, err := repo.GetByID(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByID on unknown id: err = %v, want ErrNotFound", err)
	}
}

// TestSessionRepository_ListForUser_ExcludesRevokedAndExpired covers
// the three filters in one table-driven test: revoked sessions are
// hidden, sessions older than 24h are hidden, and the list is ordered
// created_at DESC. A revoked row from a fixture user, a backdated
// expired row, and three fresh rows in known insert order exercise all
// paths.
func TestSessionRepository_ListForUser_ExcludesRevokedAndExpired(t *testing.T) {
	repo, pool := newSessionRepositoryForTest(t)
	ctx := context.Background()
	userID := sessionFixtureUser(t, pool)

	// Fresh session that we revoke.
	revokedID, err := repo.Create(ctx, userID, "revoked-ua", "10.0.0.1")
	if err != nil {
		t.Fatalf("Create (revoked fixture) returned unexpected error: %v", err)
	}
	if err := repo.Revoke(ctx, revokedID); err != nil {
		t.Fatalf("Revoke returned unexpected error: %v", err)
	}

	// Backdated expired session (created_at older than 24h). The
	// schema's DEFAULT now() doesn't let us backdate via Create, so
	// we INSERT directly with an explicit timestamp.
	expiredID, err := insertBackdatedSession(ctx, pool, userID, "expired-ua", "10.0.0.2", time.Now().Add(-25*time.Hour))
	if err != nil {
		t.Fatalf("insertBackdatedSession returned unexpected error: %v", err)
	}

	// Three fresh sessions in known insert order. Slight sleeps between
	// inserts so created_at actually differs (Postgres now() has µs
	// resolution, but timestamp-with-tz columns can collide on identical
	// microsecond timestamps within a single statement).
	freshIDs := make([]string, 0, 3)
	for i, ua := range []string{"fresh-1", "fresh-2", "fresh-3"} {
		id, err := repo.Create(ctx, userID, ua, fmt.Sprintf("10.0.0.%d", 100+i))
		if err != nil {
			t.Fatalf("Create (fresh-%d) returned unexpected error: %v", i, err)
		}
		freshIDs = append(freshIDs, id)
		time.Sleep(2 * time.Millisecond)
	}

	got, err := repo.ListForUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListForUser returned unexpected error: %v", err)
	}

	// Expected: exactly the three fresh rows, in reverse insert order
	// (fresh-3 first, fresh-1 last).
	if len(got) != 3 {
		t.Fatalf("ListForUser returned %d sessions, want 3 (got sessions: %+v)", len(got), got)
	}
	for i, s := range got {
		want := freshIDs[len(freshIDs)-1-i]
		if s.ID != want {
			t.Errorf("ListForUser[%d].ID = %q, want %q", i, s.ID, want)
		}
		if !strings.HasPrefix(s.UserAgent.String, "fresh-") {
			t.Errorf("ListForUser[%d].UserAgent = %q, want prefix %q", i, s.UserAgent.String, "fresh-")
		}
	}

	// Revoked and expired sessions must NOT appear anywhere in the list.
	for _, s := range got {
		if s.ID == revokedID {
			t.Errorf("revoked session %q appeared in ListForUser, want excluded", revokedID)
		}
		if s.ID == expiredID {
			t.Errorf("expired session %q appeared in ListForUser, want excluded", expiredID)
		}
	}
}

// TestSessionRepository_GetByIDAndUser_AntiEnumeration proves the
// ownership check is enforced at the SQL level: GetByIDAndUser with a
// wrong user returns ErrNotFound (the same error as "id doesn't exist
// at all") - never a different error that would leak "this id exists
// but belongs to someone else".
func TestSessionRepository_GetByIDAndUser_AntiEnumeration(t *testing.T) {
	repo, pool := newSessionRepositoryForTest(t)
	ctx := context.Background()
	owner := sessionFixtureUser(t, pool)
	other := sessionFixtureUser(t, pool)

	id, err := repo.Create(ctx, owner, "ua", "10.0.0.1")
	if err != nil {
		t.Fatalf("Create returned unexpected error: %v", err)
	}

	// Owner sees the row.
	got, err := repo.GetByIDAndUser(ctx, id, owner)
	if err != nil {
		t.Fatalf("GetByIDAndUser (owner) returned unexpected error: %v", err)
	}
	if got.ID != id {
		t.Errorf("GetByIDAndUser (owner).ID = %q, want %q", got.ID, id)
	}

	// Other user gets the SAME ErrNotFound as a non-existent id - so
	// the response is indistinguishable from "id doesn't exist" and
	// enumeration is impossible.
	if _, err := repo.GetByIDAndUser(ctx, id, other); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByIDAndUser (other user): err = %v, want ErrNotFound (anti-enumeration)", err)
	}
	if _, err := repo.GetByIDAndUser(ctx, "00000000-0000-0000-0000-000000000000", owner); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetByIDAndUser (unknown id): err = %v, want ErrNotFound", err)
	}
}

// TestSessionRepository_Revoke_Idempotent proves calling Revoke twice
// in a row on the same session is safe - the second call updates 0
// rows but does not return an error. Logout can be hit twice by a
// double-clicking browser or a retry-after-network-blip without ever
// surfacing a spurious 5xx.
func TestSessionRepository_Revoke_Idempotent(t *testing.T) {
	repo, pool := newSessionRepositoryForTest(t)
	ctx := context.Background()
	userID := sessionFixtureUser(t, pool)

	id, err := repo.Create(ctx, userID, "ua", "10.0.0.1")
	if err != nil {
		t.Fatalf("Create returned unexpected error: %v", err)
	}

	// First call sets revoked_at.
	if err := repo.Revoke(ctx, id); err != nil {
		t.Fatalf("first Revoke returned unexpected error: %v", err)
	}
	got, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID after first Revoke returned unexpected error: %v", err)
	}
	if !got.RevokedAt.Valid {
		t.Fatal("RevokedAt not set after first Revoke")
	}
	firstRevokedAt := got.RevokedAt.Time

	// Second call must succeed (idempotent) without changing revoked_at.
	time.Sleep(10 * time.Millisecond)
	if err := repo.Revoke(ctx, id); err != nil {
		t.Fatalf("second Revoke returned unexpected error: %v", err)
	}
	got2, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID after second Revoke returned unexpected error: %v", err)
	}
	if !got2.RevokedAt.Valid {
		t.Fatal("RevokedAt lost after second Revoke")
	}
	if !got2.RevokedAt.Time.Equal(firstRevokedAt) {
		t.Errorf("RevokedAt changed between calls: first=%v second=%v, want unchanged", firstRevokedAt, got2.RevokedAt.Time)
	}

	// Revoke on a UUID nobody owns is also a no-op (caller is wrong,
	// but the operation doesn't blow up).
	if err := repo.Revoke(ctx, "00000000-0000-0000-0000-000000000000"); err != nil {
		t.Errorf("Revoke on unknown id: err = %v, want nil (idempotent no-op)", err)
	}
}

// TestSessionRepository_TouchLastSeen_Throttled proves the throttle:
// two TouchLastSeen calls in quick succession update only one row
// (last_seen_at advances on the first call, the second hits the
// 5-minute throttle and is a no-op).
func TestSessionRepository_TouchLastSeen_Throttled(t *testing.T) {
	repo, pool := newSessionRepositoryForTest(t)
	ctx := context.Background()
	userID := sessionFixtureUser(t, pool)

	id, err := repo.Create(ctx, userID, "ua", "10.0.0.1")
	if err != nil {
		t.Fatalf("Create returned unexpected error: %v", err)
	}

	// Fresh session has last_seen_at NULL.
	got, err := repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID (fresh) returned unexpected error: %v", err)
	}
	if got.LastSeenAt.Valid {
		t.Fatalf("fresh session LastSeenAt = %v, want NULL", got.LastSeenAt.Time)
	}

	// First touch populates last_seen_at.
	if err := repo.TouchLastSeen(ctx, id); err != nil {
		t.Fatalf("first TouchLastSeen returned unexpected error: %v", err)
	}
	got, err = repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID after first TouchLastSeen returned unexpected error: %v", err)
	}
	if !got.LastSeenAt.Valid {
		t.Fatal("LastSeenAt not populated after first TouchLastSeen")
	}
	firstSeen := got.LastSeenAt.Time

	// Second touch within the throttle window must not change last_seen_at.
	time.Sleep(10 * time.Millisecond)
	if err := repo.TouchLastSeen(ctx, id); err != nil {
		t.Fatalf("second TouchLastSeen returned unexpected error: %v", err)
	}
	got, err = repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID after second TouchLastSeen returned unexpected error: %v", err)
	}
	if !got.LastSeenAt.Time.Equal(firstSeen) {
		t.Errorf("LastSeenAt advanced within throttle window: first=%v second=%v, want unchanged", firstSeen, got.LastSeenAt.Time)
	}

	// Force last_seen_at to be old enough to clear the throttle and
	// confirm a third touch DOES advance it - proves the throttle is
	// the only thing keeping the second call a no-op (not the row
	// being somehow un-updatable).
	if _, err := pool.Exec(ctx,
		"UPDATE sessions SET last_seen_at = $1 WHERE id = $2",
		time.Now().Add(-10*time.Minute), id,
	); err != nil {
		t.Fatalf("backdating last_seen_at returned unexpected error: %v", err)
	}
	if err := repo.TouchLastSeen(ctx, id); err != nil {
		t.Fatalf("third TouchLastSeen returned unexpected error: %v", err)
	}
	got, err = repo.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID after third TouchLastSeen returned unexpected error: %v", err)
	}
	if !got.LastSeenAt.Time.After(firstSeen) {
		t.Errorf("LastSeenAt did not advance past the throttle: now=%v first=%v, want now > first", got.LastSeenAt.Time, firstSeen)
	}
}

// TestSessionRepository_RevokeAllForUser_ScopedToUser proves the
// per-user revocation is actually scoped: sessions for OTHER users
// are untouched, and already-revoked sessions for the target user are
// also untouched (idempotent at the row level).
func TestSessionRepository_RevokeAllForUser_ScopedToUser(t *testing.T) {
	repo, pool := newSessionRepositoryForTest(t)
	ctx := context.Background()
	target := sessionFixtureUser(t, pool)
	bystander := sessionFixtureUser(t, pool)

	// Three active sessions for target + one already-revoked + one
	// for bystander. After RevokeAllForUser(target), only the three
	// active target sessions should flip to revoked; the already-
	// revoked one keeps its original revoked_at, bystander untouched.
	idA, err := repo.Create(ctx, target, "a", "10.0.0.1")
	if err != nil {
		t.Fatalf("Create (target A) returned unexpected error: %v", err)
	}
	idB, err := repo.Create(ctx, target, "b", "10.0.0.2")
	if err != nil {
		t.Fatalf("Create (target B) returned unexpected error: %v", err)
	}
	idC, err := repo.Create(ctx, target, "c", "10.0.0.3")
	if err != nil {
		t.Fatalf("Create (target C) returned unexpected error: %v", err)
	}
	idRevoked, err := repo.Create(ctx, target, "already-revoked", "10.0.0.4")
	if err != nil {
		t.Fatalf("Create (target already-revoked) returned unexpected error: %v", err)
	}
	if err := repo.Revoke(ctx, idRevoked); err != nil {
		t.Fatalf("Revoke (pre-existing) returned unexpected error: %v", err)
	}
	revokedBefore, err := repo.GetByID(ctx, idRevoked)
	if err != nil {
		t.Fatalf("GetByID (idRevoked) returned unexpected error: %v", err)
	}
	originalRevokedAt := revokedBefore.RevokedAt.Time

	idBystander, err := repo.Create(ctx, bystander, "bystander", "10.0.0.99")
	if err != nil {
		t.Fatalf("Create (bystander) returned unexpected error: %v", err)
	}

	// Bulk revoke for target only.
	if err := repo.RevokeAllForUser(ctx, target); err != nil {
		t.Fatalf("RevokeAllForUser returned unexpected error: %v", err)
	}

	// Three active target sessions are now revoked.
	for _, id := range []string{idA, idB, idC} {
		s, err := repo.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("GetByID (%s) returned unexpected error: %v", id, err)
		}
		if !s.RevokedAt.Valid {
			t.Errorf("target session %s RevokedAt not set after RevokeAllForUser", id)
		}
	}

	// Already-revoked target session keeps its original revoked_at
	// (UPDATE ... WHERE revoked_at IS NULL skips it).
	revokedAfter, err := repo.GetByID(ctx, idRevoked)
	if err != nil {
		t.Fatalf("GetByID (idRevoked after) returned unexpected error: %v", err)
	}
	if !revokedAfter.RevokedAt.Time.Equal(originalRevokedAt) {
		t.Errorf("already-revoked session revoked_at advanced: original=%v now=%v, want unchanged",
			originalRevokedAt, revokedAfter.RevokedAt.Time)
	}

	// Bystander's session is untouched.
	bystanderAfter, err := repo.GetByID(ctx, idBystander)
	if err != nil {
		t.Fatalf("GetByID (bystander) returned unexpected error: %v", err)
	}
	if bystanderAfter.RevokedAt.Valid {
		t.Errorf("bystander session %s was revoked by RevokeAllForUser(target), want untouched", idBystander)
	}

	// RevokeAllForUser on a user with no sessions is a no-op, not an
	// error (callers like UpdateRole don't pre-check session count).
	if err := repo.RevokeAllForUser(ctx, "00000000-0000-0000-0000-000000000000"); err != nil {
		t.Errorf("RevokeAllForUser on user with no sessions: err = %v, want nil", err)
	}
}

// insertBackdatedSession inserts a session with an explicit older
// created_at - Create() doesn't let us backdate (created_at has
// DEFAULT now()), but the ListForUser "exclude >24h" filter is the
// behavior under test here.
func insertBackdatedSession(ctx context.Context, pool *Pool, userID, ua, ip string, createdAt time.Time) (string, error) {
	var id string
	err := pool.QueryRow(ctx,
		`INSERT INTO sessions (user_id, user_agent, ip, created_at)
		 VALUES ($1, NULLIF($2, ''), NULLIF($3, '')::inet, $4)
		 RETURNING id`,
		userID, ua, ip, createdAt,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insertBackdatedSession: %w", err)
	}
	return id, nil
}
