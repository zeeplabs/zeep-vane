//go:build integration

package db

import (
	"context"
	"testing"
	"time"
)

// newNotificationPreferenceRepositoryForTest returns a repository backed by a
// pool connected to a fresh scratch database with all migrations applied, so
// each test owns its data and no lock/cleanup coordination with other
// packages' tests is needed.
func newNotificationPreferenceRepositoryForTest(t *testing.T) (*NotificationPreferenceRepository, *Pool) {
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

	return NewNotificationPreferenceRepository(pool), pool
}

// notificationFixtureUser inserts a fixture user and returns its id; the
// cleanup deletes the user, which ON DELETE CASCADEs its preference rows.
func notificationFixtureUser(t *testing.T, pool *Pool) string {
	t.Helper()
	var userID string
	if err := pool.QueryRow(context.Background(),
		"INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id",
		uniqueTestEmail(t), "hash-notifpref-repo-test",
	).Scan(&userID); err != nil {
		t.Fatalf("insert fixture user returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})
	return userID
}

func TestNotificationPreferenceRepository_Get_NoRows_ReturnsEmptyMap(t *testing.T) {
	repo, pool := newNotificationPreferenceRepositoryForTest(t)
	userID := notificationFixtureUser(t, pool)

	got, err := repo.Get(context.Background(), userID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Get() = %v, want empty map for a user with no stored rows", got)
	}
}

func TestNotificationPreferenceRepository_Upsert_NewKey_StoredAndReadBack(t *testing.T) {
	repo, pool := newNotificationPreferenceRepositoryForTest(t)
	ctx := context.Background()
	userID := notificationFixtureUser(t, pool)

	if err := repo.Upsert(ctx, userID, map[string]bool{NotificationTypeWeeklyDigest: true}); err != nil {
		t.Fatalf("Upsert() returned unexpected error: %v", err)
	}

	got, err := repo.Get(ctx, userID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if len(got) != 1 || !got[NotificationTypeWeeklyDigest] {
		t.Errorf("Get() = %v, want {%s: true}", got, NotificationTypeWeeklyDigest)
	}
}

func TestNotificationPreferenceRepository_Upsert_MultipleKeys_AllStored(t *testing.T) {
	repo, pool := newNotificationPreferenceRepositoryForTest(t)
	ctx := context.Background()
	userID := notificationFixtureUser(t, pool)

	if err := repo.Upsert(ctx, userID, map[string]bool{
		NotificationTypeIncidentOpened:   false,
		NotificationTypeIncidentResolved: true,
	}); err != nil {
		t.Fatalf("Upsert() returned unexpected error: %v", err)
	}

	got, err := repo.Get(ctx, userID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Get() = %v, want exactly 2 stored keys", got)
	}
	if got[NotificationTypeIncidentOpened] {
		t.Errorf("Get()[%s] = true, want false", NotificationTypeIncidentOpened)
	}
	if !got[NotificationTypeIncidentResolved] {
		t.Errorf("Get()[%s] = false, want true", NotificationTypeIncidentResolved)
	}
}

func TestNotificationPreferenceRepository_Upsert_PartialUpdate_LeavesOmittedKeysUntouched(t *testing.T) {
	repo, pool := newNotificationPreferenceRepositoryForTest(t)
	ctx := context.Background()
	userID := notificationFixtureUser(t, pool)

	if err := repo.Upsert(ctx, userID, map[string]bool{
		NotificationTypeIncidentOpened: true,
		NotificationTypeWeeklyDigest:   true,
	}); err != nil {
		t.Fatalf("initial Upsert() returned unexpected error: %v", err)
	}

	// Update only one key.
	if err := repo.Upsert(ctx, userID, map[string]bool{NotificationTypeIncidentOpened: false}); err != nil {
		t.Fatalf("second Upsert() returned unexpected error: %v", err)
	}

	got, err := repo.Get(ctx, userID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if got[NotificationTypeIncidentOpened] {
		t.Errorf("Get()[%s] = true, want false after update", NotificationTypeIncidentOpened)
	}
	if !got[NotificationTypeWeeklyDigest] {
		t.Errorf("Get()[%s] = false, want true - omitted key must be untouched", NotificationTypeWeeklyDigest)
	}
}

func TestNotificationPreferenceRepository_Upsert_ExistingKey_OverwritesValue(t *testing.T) {
	repo, pool := newNotificationPreferenceRepositoryForTest(t)
	ctx := context.Background()
	userID := notificationFixtureUser(t, pool)

	if err := repo.Upsert(ctx, userID, map[string]bool{NotificationTypeIncidentResolved: true}); err != nil {
		t.Fatalf("first Upsert() returned unexpected error: %v", err)
	}
	if err := repo.Upsert(ctx, userID, map[string]bool{NotificationTypeIncidentResolved: false}); err != nil {
		t.Fatalf("second Upsert() returned unexpected error: %v", err)
	}

	got, err := repo.Get(ctx, userID)
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if len(got) != 1 || got[NotificationTypeIncidentResolved] {
		t.Errorf("Get() = %v, want {%s: false}", got, NotificationTypeIncidentResolved)
	}
}

func TestNotificationPreferenceRepository_ResolveEnabledForUsers_MissingRow_AppliesDefaults(t *testing.T) {
	repo, pool := newNotificationPreferenceRepositoryForTest(t)
	ctx := context.Background()
	userID := notificationFixtureUser(t, pool)

	opened, err := repo.ResolveEnabledForUsers(ctx, []string{userID}, NotificationTypeIncidentOpened)
	if err != nil {
		t.Fatalf("ResolveEnabledForUsers(incident_opened) returned unexpected error: %v", err)
	}
	if !opened[userID] {
		t.Errorf("incident_opened default = false, want true for a user with no row")
	}

	digest, err := repo.ResolveEnabledForUsers(ctx, []string{userID}, NotificationTypeWeeklyDigest)
	if err != nil {
		t.Fatalf("ResolveEnabledForUsers(weekly_digest) returned unexpected error: %v", err)
	}
	if digest[userID] {
		t.Errorf("weekly_digest default = true, want false for a user with no row")
	}
}

func TestNotificationPreferenceRepository_ResolveEnabledForUsers_StoredValuesWin(t *testing.T) {
	repo, pool := newNotificationPreferenceRepositoryForTest(t)
	ctx := context.Background()
	userID := notificationFixtureUser(t, pool)

	// Store the opposite of each default.
	if err := repo.Upsert(ctx, userID, map[string]bool{
		NotificationTypeIncidentOpened: false,
		NotificationTypeWeeklyDigest:   true,
	}); err != nil {
		t.Fatalf("Upsert() returned unexpected error: %v", err)
	}

	opened, err := repo.ResolveEnabledForUsers(ctx, []string{userID}, NotificationTypeIncidentOpened)
	if err != nil {
		t.Fatalf("ResolveEnabledForUsers(incident_opened) returned unexpected error: %v", err)
	}
	if opened[userID] {
		t.Errorf("incident_opened = true, want stored false to override the default")
	}

	digest, err := repo.ResolveEnabledForUsers(ctx, []string{userID}, NotificationTypeWeeklyDigest)
	if err != nil {
		t.Fatalf("ResolveEnabledForUsers(weekly_digest) returned unexpected error: %v", err)
	}
	if !digest[userID] {
		t.Errorf("weekly_digest = false, want stored true to override the default")
	}
}

func TestNotificationPreferenceRepository_ResolveEnabledForUsers_MixedRecipients_ReturnsEveryCandidate(t *testing.T) {
	repo, pool := newNotificationPreferenceRepositoryForTest(t)
	ctx := context.Background()
	optedOut := notificationFixtureUser(t, pool)
	noRow := notificationFixtureUser(t, pool)

	if err := repo.Upsert(ctx, optedOut, map[string]bool{NotificationTypeIncidentOpened: false}); err != nil {
		t.Fatalf("Upsert() returned unexpected error: %v", err)
	}

	got, err := repo.ResolveEnabledForUsers(ctx, []string{optedOut, noRow}, NotificationTypeIncidentOpened)
	if err != nil {
		t.Fatalf("ResolveEnabledForUsers() returned unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ResolveEnabledForUsers() returned %d entries, want 2 (one per candidate)", len(got))
	}
	if got[optedOut] {
		t.Errorf("opted-out user resolved true, want false")
	}
	if !got[noRow] {
		t.Errorf("user with no row resolved false, want the incident_opened default true")
	}
}

func TestNotificationPreferenceRepository_ResolveEnabledForUsers_EmptyInput_ReturnsEmptyMap(t *testing.T) {
	repo, _ := newNotificationPreferenceRepositoryForTest(t)

	got, err := repo.ResolveEnabledForUsers(context.Background(), nil, NotificationTypeIncidentOpened)
	if err != nil {
		t.Fatalf("ResolveEnabledForUsers(nil) returned unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ResolveEnabledForUsers(nil) = %v, want empty map", got)
	}
}
