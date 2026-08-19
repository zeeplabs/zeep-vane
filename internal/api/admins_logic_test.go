package api

import (
	"testing"

	"github.com/zeeplabs/zeep-vane/internal/db"
)

// TestWouldLeaveZeroOwners covers the ADM-06 lockout decision boundary as a
// pure function, deterministically - CountActiveOwners' absolute value is
// otherwise only observable against the shared integration test database,
// where ambient rows from concurrently running packages make an exact
// global count of 1 unreliable to assert on (see
// TestUpdateAdminRole_SelfDemotionAsLastOwner_409 for the corresponding
// end-to-end case).
func TestWouldLeaveZeroOwners(t *testing.T) {
	tests := []struct {
		name           string
		currentRole    string
		keepsOwnerRole bool
		ownerCount     int
		want           bool
	}{
		{"owner demoted, last owner", db.RoleOwner, false, 1, true},
		{"owner demoted, another owner remains", db.RoleOwner, false, 2, false},
		{"owner reassigned to owner, last owner", db.RoleOwner, true, 1, false},
		{"non-owner demoted, count irrelevant", db.RoleOperator, false, 1, false},
		{"non-owner, zero owners already (unrelated to this action)", db.RoleViewer, false, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wouldLeaveZeroOwners(tt.currentRole, tt.keepsOwnerRole, tt.ownerCount)
			if got != tt.want {
				t.Errorf("wouldLeaveZeroOwners(%q, %v, %d) = %v, want %v",
					tt.currentRole, tt.keepsOwnerRole, tt.ownerCount, got, tt.want)
			}
		})
	}
}
