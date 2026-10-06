package db

import (
	"errors"
	"testing"
)

// TestValidateServicePlacements covers T3's done-when for SGD-08, SGD-10,
// SGD-11, SGD-12: all 4 rejection sentinels, plus the happy paths (group
// alone, parent alone, group+parent combined on the same placement, and
// neither set), plus the empty-input edge cases.
func TestValidateServicePlacements(t *testing.T) {
	groupID := "group-1"
	otherGroupID := "group-2"

	tests := []struct {
		name          string
		placements    []ServicePlacement
		validGroupIDs map[string]bool
		wantErr       error
	}{
		{
			name:          "empty placements slice is valid",
			placements:    []ServicePlacement{},
			validGroupIDs: map[string]bool{},
			wantErr:       nil,
		},
		{
			name: "neither group nor parent set is valid",
			placements: []ServicePlacement{
				{ServiceID: "svc-1", Position: 0},
			},
			validGroupIDs: map[string]bool{},
			wantErr:       nil,
		},
		{
			name: "group alone is valid when group id is in validGroupIDs",
			placements: []ServicePlacement{
				{ServiceID: "svc-1", GroupID: &groupID, Position: 0},
			},
			validGroupIDs: map[string]bool{groupID: true},
			wantErr:       nil,
		},
		{
			name: "parent alone is valid when parent references another placement in the same set",
			placements: []ServicePlacement{
				{ServiceID: "svc-parent", Position: 0},
				{ServiceID: "svc-child", ParentServiceID: placementStrPtr("svc-parent"), Position: 1},
			},
			validGroupIDs: map[string]bool{},
			wantErr:       nil,
		},
		{
			name: "group and parent combined on the same placement is valid",
			placements: []ServicePlacement{
				{ServiceID: "svc-parent", GroupID: &groupID, Position: 0},
				{ServiceID: "svc-child", GroupID: &groupID, ParentServiceID: placementStrPtr("svc-parent"), Position: 1},
			},
			validGroupIDs: map[string]bool{groupID: true},
			wantErr:       nil,
		},
		{
			name: "group id not in validGroupIDs is rejected with ErrGroupCrossPage",
			placements: []ServicePlacement{
				{ServiceID: "svc-1", GroupID: &otherGroupID, Position: 0},
			},
			validGroupIDs: map[string]bool{groupID: true},
			wantErr:       ErrGroupCrossPage,
		},
		{
			name: "parent id not matching any ServiceID in the set is rejected with ErrParentNotInSet",
			placements: []ServicePlacement{
				{ServiceID: "svc-1", ParentServiceID: placementStrPtr("svc-does-not-exist"), Position: 0},
			},
			validGroupIDs: map[string]bool{},
			wantErr:       ErrParentNotInSet,
		},
		{
			name: "parent id equal to its own ServiceID is rejected with ErrSelfReference, not ErrParentNotInSet",
			placements: []ServicePlacement{
				{ServiceID: "svc-1", ParentServiceID: placementStrPtr("svc-1"), Position: 0},
			},
			validGroupIDs: map[string]bool{},
			wantErr:       ErrSelfReference,
		},
		{
			name: "a placement that is itself a parent cannot also have its own parent (depth cap)",
			placements: []ServicePlacement{
				{ServiceID: "svc-grandparent", Position: 0},
				{ServiceID: "svc-parent", ParentServiceID: placementStrPtr("svc-grandparent"), Position: 1},
				{ServiceID: "svc-child", ParentServiceID: placementStrPtr("svc-parent"), Position: 2},
			},
			validGroupIDs: map[string]bool{},
			wantErr:       ErrMaxDepthExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateServicePlacements(tt.placements, tt.validGroupIDs)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("validateServicePlacements() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateServicePlacements() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func placementStrPtr(s string) *string {
	return &s
}
