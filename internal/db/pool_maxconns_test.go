package db

import "testing"

// TestNewPool_CapsMaxConns guards against connection-pool exhaustion under
// heavy test-suite parallelism: every integration test opens its own
// *Pool, and pgxpool's own default MaxConns (max(4, runtime.NumCPU())) is
// large enough that, across the dozens of packages this project's
// integration suite spans, concurrent pools can collectively approach or
// exceed Postgres's max_connections (100 by default) - reproduced live via
// "FATAL: sorry, too many clients already (SQLSTATE 53300)" under `go test
// -tags=integration ./...`. NewPool must cap MaxConns low enough that even
// worst-case package parallelism stays well under that ceiling. No live
// database is required - ParseConfig/NewWithConfig never eagerly connect.
func TestNewPool_CapsMaxConns(t *testing.T) {
	pool, err := NewPool(t.Context(), "postgres://user:pass@localhost:5432/db")
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	defer pool.Close()

	got := pool.Config().MaxConns
	if got > 4 {
		t.Errorf("MaxConns = %d, want <= 4 (uncapped defaults to NumCPU, risking connection exhaustion under full-suite test parallelism)", got)
	}
}
