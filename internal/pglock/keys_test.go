package pglock

import "testing"

// TestReservedLockKeys_Unique is the guard for the reserved-key registry:
// a duplicate key makes the losing production job silently skip its work
// instead of erroring, and it is invisible at the call site because each
// caller holds a distinct constant name. Every registered key must be
// pairwise distinct.
func TestReservedLockKeys_Unique(t *testing.T) {
	seen := make(map[int64]string, len(reservedLockKeys))
	for owner, key := range reservedLockKeys {
		if prev, dup := seen[key]; dup {
			t.Errorf("advisory lock key %d is shared by %q and %q; each production lock needs its own key", key, prev, owner)
			continue
		}
		seen[key] = owner
	}
}
