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
