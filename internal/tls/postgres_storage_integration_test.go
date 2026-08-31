//go:build integration

package tls

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// newTestPostgresStorageWithDSN is like newTestPostgresStorage but also
// returns the dsn, needed by tests that build a second, independent
// PostgresStorage instance (simulating a second replica) sharing the same
// database.
func newTestPostgresStorageWithDSN(t *testing.T) (*PostgresStorage, string) {
	t.Helper()
	dsn := testDatabaseURL(t)

	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	return NewPostgresStorage(pool, dsn), dsn
}

// newTestPostgresStorage returns a PostgresStorage backed by
// TEST_DATABASE_URL, migrated and connected the same way every other
// integration test in this repo is (see setUpStatusPageFixture in
// manager_integration_test.go).
func newTestPostgresStorage(t *testing.T) *PostgresStorage {
	t.Helper()
	dsn := testDatabaseURL(t)

	if err := db.MigrateUp(dsn, "../db/migrations"); err != nil {
		t.Fatalf("MigrateUp() returned unexpected error: %v", err)
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	return NewPostgresStorage(pool, dsn)
}

// testKey returns a unique key namespaced to this test run, so concurrent
// test runs against a shared database never collide.
func testKey(t *testing.T, suffix string) string {
	t.Helper()
	return fmt.Sprintf("certmagic-storage-test-%d/%s", time.Now().UnixNano(), suffix)
}

func TestPostgresStorage_StoreLoad_RoundTripsIdenticalBytes(t *testing.T) {
	s := newTestPostgresStorage(t)
	ctx := context.Background()
	key := testKey(t, "example.com.crt")
	want := []byte("-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----")

	if err := s.Store(ctx, key, want); err != nil {
		t.Fatalf("Store() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = s.Delete(context.Background(), key) })

	got, err := s.Load(ctx, key)
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("Load() = %q, want %q", got, want)
	}
}

func TestPostgresStorage_Store_OverwritesExistingValue(t *testing.T) {
	s := newTestPostgresStorage(t)
	ctx := context.Background()
	key := testKey(t, "overwrite.crt")

	if err := s.Store(ctx, key, []byte("first")); err != nil {
		t.Fatalf("first Store() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = s.Delete(context.Background(), key) })

	if err := s.Store(ctx, key, []byte("second")); err != nil {
		t.Fatalf("second Store() returned unexpected error: %v", err)
	}

	got, err := s.Load(ctx, key)
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if string(got) != "second" {
		t.Errorf("Load() = %q, want %q", got, "second")
	}
}

func TestPostgresStorage_Load_MissingKey_ReturnsErrNotExist(t *testing.T) {
	s := newTestPostgresStorage(t)
	ctx := context.Background()

	_, err := s.Load(ctx, testKey(t, "never-stored.crt"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Load() error = %v, want errors.Is(err, fs.ErrNotExist)", err)
	}
}

func TestPostgresStorage_Delete_ExactKey_RemovesIt(t *testing.T) {
	s := newTestPostgresStorage(t)
	ctx := context.Background()
	key := testKey(t, "delete-me.crt")

	if err := s.Store(ctx, key, []byte("bytes")); err != nil {
		t.Fatalf("Store() returned unexpected error: %v", err)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() returned unexpected error: %v", err)
	}

	if s.Exists(ctx, key) {
		t.Error("Exists() = true after Delete(), want false")
	}
}

func TestPostgresStorage_Delete_MissingKey_ReturnsErrNotExist(t *testing.T) {
	s := newTestPostgresStorage(t)
	ctx := context.Background()

	err := s.Delete(ctx, testKey(t, "never-stored.crt"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Delete() error = %v, want errors.Is(err, fs.ErrNotExist)", err)
	}
}

func TestPostgresStorage_Delete_DirectoryPrefix_RemovesEveryKeyUnderIt(t *testing.T) {
	s := newTestPostgresStorage(t)
	ctx := context.Background()
	dir := testKey(t, "bundle")
	keyA := dir + "/a.crt"
	keyB := dir + "/b.key"

	if err := s.Store(ctx, keyA, []byte("a")); err != nil {
		t.Fatalf("Store(a) returned unexpected error: %v", err)
	}
	if err := s.Store(ctx, keyB, []byte("b")); err != nil {
		t.Fatalf("Store(b) returned unexpected error: %v", err)
	}

	if err := s.Delete(ctx, dir); err != nil {
		t.Fatalf("Delete(dir) returned unexpected error: %v", err)
	}

	if s.Exists(ctx, keyA) {
		t.Error("Exists(keyA) = true after deleting its directory prefix, want false")
	}
	if s.Exists(ctx, keyB) {
		t.Error("Exists(keyB) = true after deleting its directory prefix, want false")
	}
}

func TestPostgresStorage_Exists(t *testing.T) {
	s := newTestPostgresStorage(t)
	ctx := context.Background()
	dir := testKey(t, "exists-dir")
	fileKey := dir + "/leaf.crt"

	if s.Exists(ctx, fileKey) {
		t.Error("Exists() = true before Store, want false")
	}

	if err := s.Store(ctx, fileKey, []byte("bytes")); err != nil {
		t.Fatalf("Store() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = s.Delete(context.Background(), dir) })

	if !s.Exists(ctx, fileKey) {
		t.Error("Exists(exact key) = false after Store, want true")
	}
	if !s.Exists(ctx, dir) {
		t.Error("Exists(directory prefix) = false with a key stored under it, want true")
	}
}

func TestPostgresStorage_List(t *testing.T) {
	s := newTestPostgresStorage(t)
	ctx := context.Background()
	dir := testKey(t, "list-dir")

	keys := []string{
		dir + "/sub1/a.crt",
		dir + "/sub1/b.crt",
		dir + "/sub2/c.crt",
	}
	for _, k := range keys {
		if err := s.Store(ctx, k, []byte("x")); err != nil {
			t.Fatalf("Store(%s) returned unexpected error: %v", k, err)
		}
	}
	t.Cleanup(func() { _ = s.Delete(context.Background(), dir) })

	recursive, err := s.List(ctx, dir, true)
	if err != nil {
		t.Fatalf("List(recursive) returned unexpected error: %v", err)
	}
	sort.Strings(recursive)
	wantRecursive := append([]string{}, keys...)
	sort.Strings(wantRecursive)
	if fmt.Sprint(recursive) != fmt.Sprint(wantRecursive) {
		t.Errorf("List(recursive=true) = %v, want %v", recursive, wantRecursive)
	}

	nonRecursive, err := s.List(ctx, dir, false)
	if err != nil {
		t.Fatalf("List(non-recursive) returned unexpected error: %v", err)
	}
	sort.Strings(nonRecursive)
	wantNonRecursive := []string{dir + "/sub1", dir + "/sub2"}
	if fmt.Sprint(nonRecursive) != fmt.Sprint(wantNonRecursive) {
		t.Errorf("List(recursive=false) = %v, want %v (deduplicated immediate segments)", nonRecursive, wantNonRecursive)
	}
}

func TestPostgresStorage_List_NoMatches_ReturnsEmptySliceNotError(t *testing.T) {
	s := newTestPostgresStorage(t)
	ctx := context.Background()

	got, err := s.List(ctx, testKey(t, "empty-dir"), true)
	if err != nil {
		t.Fatalf("List() returned unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List() = %v, want empty slice", got)
	}
}

func TestPostgresStorage_Stat_FileKey(t *testing.T) {
	s := newTestPostgresStorage(t)
	ctx := context.Background()
	key := testKey(t, "stat-file.crt")
	value := []byte("hello world")

	if err := s.Store(ctx, key, value); err != nil {
		t.Fatalf("Store() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = s.Delete(context.Background(), key) })

	info, err := s.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat() returned unexpected error: %v", err)
	}
	if info.Key != key {
		t.Errorf("Stat().Key = %q, want %q", info.Key, key)
	}
	if !info.IsTerminal {
		t.Error("Stat().IsTerminal = false for a file key, want true")
	}
	if info.Size != int64(len(value)) {
		t.Errorf("Stat().Size = %d, want %d", info.Size, len(value))
	}
	if info.Modified.IsZero() {
		t.Error("Stat().Modified is zero, want a real timestamp")
	}
}

func TestPostgresStorage_Stat_DirectoryOnlyPrefix(t *testing.T) {
	s := newTestPostgresStorage(t)
	ctx := context.Background()
	dir := testKey(t, "stat-dir")
	leaf := dir + "/leaf.crt"

	if err := s.Store(ctx, leaf, []byte("x")); err != nil {
		t.Fatalf("Store() returned unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = s.Delete(context.Background(), dir) })

	info, err := s.Stat(ctx, dir)
	if err != nil {
		t.Fatalf("Stat(directory) returned unexpected error: %v", err)
	}
	if info.IsTerminal {
		t.Error("Stat().IsTerminal = true for a directory-only prefix, want false")
	}
}

func TestPostgresStorage_Stat_MissingKey_ReturnsErrNotExist(t *testing.T) {
	s := newTestPostgresStorage(t)
	ctx := context.Background()

	_, err := s.Stat(ctx, testKey(t, "never-stored.crt"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Stat() error = %v, want errors.Is(err, fs.ErrNotExist)", err)
	}
}

// TestPostgresStorage_Lock_BlocksSecondInstanceUntilUnlocked covers
// HA-15: two PostgresStorage instances (simulating two replicas) sharing
// one database - a second Lock for the same name must block until the
// first Unlocks.
func TestPostgresStorage_Lock_BlocksSecondInstanceUntilUnlocked(t *testing.T) {
	s1, _ := newTestPostgresStorageWithDSN(t)
	s2, _ := newTestPostgresStorageWithDSN(t)
	name := fmt.Sprintf("lock-test-%d", time.Now().UnixNano())
	ctx := context.Background()

	if err := s1.Lock(ctx, name); err != nil {
		t.Fatalf("s1.Lock() returned unexpected error: %v", err)
	}

	locked := make(chan struct{}, 1)
	errs := make(chan error, 1)
	go func() {
		if err := s2.Lock(context.Background(), name); err != nil {
			errs <- err
			return
		}
		locked <- struct{}{}
	}()

	select {
	case <-locked:
		t.Fatal("s2.Lock() succeeded while s1 still holds the lock")
	case err := <-errs:
		t.Fatalf("s2.Lock() returned unexpected error: %v", err)
	case <-time.After(300 * time.Millisecond):
		// Expected: s2 is still blocked.
	}

	if err := s1.Unlock(ctx, name); err != nil {
		t.Fatalf("s1.Unlock() returned unexpected error: %v", err)
	}

	select {
	case <-locked:
		if err := s2.Unlock(context.Background(), name); err != nil {
			t.Errorf("s2.Unlock() returned unexpected error: %v", err)
		}
	case err := <-errs:
		t.Fatalf("s2.Lock() returned unexpected error after s1 unlocked: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("s2.Lock() did not succeed within 5s of s1 unlocking")
	}
}

// TestPostgresStorage_Unlock_RemovesTrackedHandle proves Unlock both
// releases the underlying advisory lock and forgets the handle, so a
// second Lock call for the same name from the same instance succeeds
// immediately afterward.
func TestPostgresStorage_Unlock_RemovesTrackedHandle(t *testing.T) {
	s, _ := newTestPostgresStorageWithDSN(t)
	name := fmt.Sprintf("lock-test-reuse-%d", time.Now().UnixNano())
	ctx := context.Background()

	if err := s.Lock(ctx, name); err != nil {
		t.Fatalf("first Lock() returned unexpected error: %v", err)
	}
	if err := s.Unlock(ctx, name); err != nil {
		t.Fatalf("Unlock() returned unexpected error: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- s.Lock(context.Background(), name) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second Lock() returned unexpected error: %v", err)
		}
		_ = s.Unlock(context.Background(), name)
	case <-time.After(2 * time.Second):
		t.Fatal("second Lock() for the same name did not succeed within 2s after Unlock()")
	}
}

// TestPostgresStorage_Lock_CrashedHolderAutoReleases covers HA-17: if a
// replica crashes while holding a CertMagic storage lock, Postgres's
// session-scoped advisory lock semantics release it automatically once
// that session's connection closes, so a second Lock call for the same
// name can proceed - no permanent deadlock survives the crash. This is
// exercised here by reaching into the tracked handle and closing its
// session directly (via Release, which is the same "unlock + close
// connection" pglock performs on a session end) rather than through s1's
// own Unlock, standing in for the process dying without ever calling
// Unlock - pglock.Handle's connection is unexported, so PostgresStorage
// (like any Locker caller) has no way to force-kill it any more directly
// than this from outside the pglock package; internal/pglock's own
// integration tests separately cover the literal "connection killed
// out-of-band" mechanism this relies on.
func TestPostgresStorage_Lock_CrashedHolderAutoReleases(t *testing.T) {
	s1, _ := newTestPostgresStorageWithDSN(t)
	s2, _ := newTestPostgresStorageWithDSN(t)
	name := fmt.Sprintf("lock-test-crash-%d", time.Now().UnixNano())
	ctx := context.Background()

	if err := s1.Lock(ctx, name); err != nil {
		t.Fatalf("s1.Lock() returned unexpected error: %v", err)
	}

	s1.mu.Lock()
	handle := s1.locks[name]
	s1.mu.Unlock()
	if handle == nil {
		t.Fatal("no tracked handle found for name after Lock()")
	}
	if err := handle.Release(context.Background()); err != nil {
		t.Fatalf("simulated crash: Release() returned unexpected error: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- s2.Lock(context.Background(), name) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("s2.Lock() after simulated crash returned unexpected error: %v", err)
		}
		_ = s2.Unlock(context.Background(), name)
	case <-time.After(5 * time.Second):
		t.Fatal("s2.Lock() did not succeed within 5s of s1's simulated crash")
	}
}

// killAdvisoryLockHolderByName finds the Postgres backend holding the
// session-scoped advisory lock that pglock.Acquire(dsn, name) would have
// taken (hashtextextended(name, 0), the same hash pglock.Acquire/Release
// use) and terminates it, simulating a replica's process dying mid-hold
// without ever calling Unlock - the same out-of-band mechanism
// internal/cli's killPollerLeaderBackend uses for the poller's leader lock,
// generalized here to a name-hashed key rather than a raw int64 one.
func killAdvisoryLockHolderByName(t *testing.T, pool *db.Pool, name string) bool {
	t.Helper()
	ctx := context.Background()

	var key int64
	if err := pool.QueryRow(ctx, "SELECT hashtextextended($1, 0)", name).Scan(&key); err != nil {
		t.Fatalf("hashtextextended() returned unexpected error: %v", err)
	}
	// pg_advisory_lock(bigint) stores the 64-bit key as two unsigned 32-bit
	// halves (classid = high bits, objid = low bits) in pg_locks, with
	// objsubid = 1 - cast both sides to bigint so the comparison is exact
	// regardless of the hashed key's sign.
	classid := int64(uint32(uint64(key) >> 32))
	objid := int64(uint32(uint64(key)))

	var pid int
	err := pool.QueryRow(ctx,
		`SELECT pid FROM pg_locks WHERE locktype = 'advisory' AND classid::bigint = $1 AND objid::bigint = $2 AND objsubid = 1 LIMIT 1`,
		classid, objid,
	).Scan(&pid)
	if err != nil {
		return false
	}
	if _, err := pool.Exec(ctx, "SELECT pg_terminate_backend($1)", pid); err != nil {
		t.Fatalf("pg_terminate_backend() returned unexpected error: %v", err)
	}
	return true
}

// TestPostgresStorage_Lock_OutOfBandKill_AutoReleases closes the real HA-17
// gap TestPostgresStorage_Lock_CrashedHolderAutoReleases's own doc comment
// disclosed: that test simulates a crash via handle.Release() (a graceful
// unlock+close), not a true severed connection. This test instead kills
// s1's actual advisory-lock backend directly at the Postgres level - the
// same real out-of-band kill internal/cli's HA-04 poller test performs -
// and proves PostgresStorage.Lock itself (not just the underlying pglock
// package in isolation) observes the resulting auto-release and lets a
// second instance proceed.
func TestPostgresStorage_Lock_OutOfBandKill_AutoReleases(t *testing.T) {
	s1, dsn := newTestPostgresStorageWithDSN(t)
	s2, _ := newTestPostgresStorageWithDSN(t)
	name := fmt.Sprintf("lock-test-oob-kill-%d", time.Now().UnixNano())
	ctx := context.Background()

	if err := s1.Lock(ctx, name); err != nil {
		t.Fatalf("s1.Lock() returned unexpected error: %v", err)
	}

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	if !killAdvisoryLockHolderByName(t, pool, name) {
		t.Fatal("could not find s1's advisory-lock backend to kill - test setup problem")
	}

	done := make(chan error, 1)
	go func() { done <- s2.Lock(context.Background(), name) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("s2.Lock() after out-of-band kill returned unexpected error: %v", err)
		}
		_ = s2.Unlock(context.Background(), name)
	case <-time.After(5 * time.Second):
		t.Fatal("s2.Lock() did not succeed within 5s of s1's backend being killed out-of-band (HA-17)")
	}
}
