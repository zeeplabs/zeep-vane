package db

import (
	"errors"
	"testing"
)

// TestValidateAndSanitizeServicePlacements covers T3's done-when for
// SGD-08, SGD-10, SGD-11, SGD-12, plus SGD-13's dangling-parent-reference
// sanitization: all rejection sentinels, the happy paths (group alone,
// parent alone, group+parent combined on the same placement, and neither
// set), the empty-input edge cases, and the SGD-13 cases (a parent absent
// from the submitted slice is cleared when it was previously on the page,
// rejected when it never was).
func TestValidateAndSanitizeServicePlacements(t *testing.T) {
	groupID := "group-1"
	otherGroupID := "group-2"

	tests := []struct {
		name               string
		placements         []ServicePlacement
		validGroupIDs      map[string]bool
		existingServiceIDs map[string]bool
		wantErr            error
		// wantParentless, when non-empty, asserts that exactly these
		// ServiceIDs end up with a nil ParentServiceID in the sanitized
		// result (SGD-13 cases only - nil for every other case, meaning
		// "don't check").
		wantParentless []string
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
			name: "parent id not matching any ServiceID in the set, and never on the page before, is rejected with ErrParentNotInSet",
			placements: []ServicePlacement{
				{ServiceID: "svc-1", ParentServiceID: placementStrPtr("svc-does-not-exist"), Position: 0},
			},
			validGroupIDs:      map[string]bool{},
			existingServiceIDs: map[string]bool{},
			wantErr:            ErrParentNotInSet,
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
		{
			// SGD-13: the page previously had svc-parent linked (it's in
			// existingServiceIDs) and this call's own placements omit it -
			// i.e. this call is removing it from the page. svc-child still
			// references it, but that must not reject the whole save; the
			// reference is cleared instead.
			name: "parent absent from this call but previously on the page is cleared, not rejected (SGD-13)",
			placements: []ServicePlacement{
				{ServiceID: "svc-child", ParentServiceID: placementStrPtr("svc-parent"), Position: 0},
			},
			validGroupIDs:      map[string]bool{},
			existingServiceIDs: map[string]bool{"svc-parent": true},
			wantErr:            nil,
			wantParentless:     []string{"svc-child"},
		},
		{
			// Same shape as the ErrParentNotInSet case above, but spelled
			// out alongside the SGD-13 case for contrast: a parent that was
			// never on the page at all (not in existingServiceIDs either)
			// is still rejected - SGD-13 only relaxes the "just removed"
			// case, not an invalid/cross-page reference.
			name: "parent absent from this call and never on the page is still rejected (SGD-11)",
			placements: []ServicePlacement{
				{ServiceID: "svc-child", ParentServiceID: placementStrPtr("svc-cross-page-parent"), Position: 0},
			},
			validGroupIDs:      map[string]bool{},
			existingServiceIDs: map[string]bool{"svc-unrelated": true},
			wantErr:            ErrParentNotInSet,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized, err := validateAndSanitizeServicePlacements(tt.placements, tt.validGroupIDs, tt.existingServiceIDs)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("validateAndSanitizeServicePlacements() error = %v, want nil", err)
				}
				if tt.wantParentless != nil {
					wantSet := make(map[string]bool, len(tt.wantParentless))
					for _, id := range tt.wantParentless {
						wantSet[id] = true
					}
					for _, p := range sanitized {
						gotParentless := p.ParentServiceID == nil
						if wantSet[p.ServiceID] && !gotParentless {
							t.Errorf("placement %s: ParentServiceID = %v, want nil", p.ServiceID, *p.ParentServiceID)
						}
					}
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateAndSanitizeServicePlacements() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func placementStrPtr(s string) *string {
	return &s
}
