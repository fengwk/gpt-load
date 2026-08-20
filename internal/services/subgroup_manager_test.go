package services

import (
	"fmt"
	"testing"

	"gpt-load/internal/models"
	"gpt-load/internal/store"
)

type selectorTestStore struct {
	store.Store
	lengths map[string]int64
}

func (s *selectorTestStore) LLen(key string) (int64, error) {
	return s.lengths[key], nil
}

func newSelectorTestStore(activeGroupIDs ...uint) *selectorTestStore {
	lengths := make(map[string]int64, len(activeGroupIDs))
	for _, groupID := range activeGroupIDs {
		lengths[fmt.Sprintf("group:%d:active_keys", groupID)] = 1
	}
	return &selectorTestStore{lengths: lengths}
}

func testSubGroup(
	id uint,
	name string,
	weight int,
	routeModels []string,
) models.GroupSubGroup {
	return models.GroupSubGroup{
		SubGroupID:   id,
		SubGroupName: name,
		Weight:       weight,
		RouteModels:  models.EncodeModelList(routeModels),
	}
}

func testAggregateGroup(id uint, subGroups ...models.GroupSubGroup) *models.Group {
	return &models.Group{
		ID:          id,
		Name:        fmt.Sprintf("aggregate-%d", id),
		GroupType:   "aggregate",
		SubGroups:   subGroups,
		ChannelType: "openai",
	}
}

func newTestManager(group *models.Group, activeGroupIDs ...uint) *SubGroupManager {
	manager := NewSubGroupManager(newSelectorTestStore(activeGroupIDs...))
	manager.RebuildSelectors(map[string]*models.Group{group.Name: group})
	return manager
}

func candidateIDs(candidates []subGroupItem) []uint {
	ids := make([]uint, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.subGroupID)
	}
	return ids
}

// TestSubGroupSelectorRouteAndDefaultPool proves explicit route matches take
// priority, while unmatched or missing models use only unrestricted sub-groups.
func TestSubGroupSelectorRouteAndDefaultPool(t *testing.T) {
	group := testAggregateGroup(
		1,
		testSubGroup(10, "route-a", 1, []string{"model-a"}),
		testSubGroup(20, "fallback", 1, nil),
	)
	manager := newTestManager(group, 10, 20)
	selector := manager.selectors[group.ID]

	if got := candidateIDs(selector.finalCandidates("model-a")); len(got) != 1 || got[0] != 10 {
		t.Fatalf("route-matched candidate IDs = %v, want [10]", got)
	}
	if got := candidateIDs(selector.finalCandidates("unknown-model")); len(got) != 1 || got[0] != 20 {
		t.Fatalf("unmatched candidate IDs = %v, want default pool [20]", got)
	}
	if got := candidateIDs(selector.finalCandidates("Model-A")); len(got) != 1 || got[0] != 20 {
		t.Fatalf("case-mismatched candidate IDs = %v, want default pool [20]", got)
	}
	if got := candidateIDs(selector.finalCandidates("")); len(got) != 1 || got[0] != 20 {
		t.Fatalf("missing-model candidate IDs = %v, want default pool [20]", got)
	}

	selected, err := manager.SelectSubGroup(group, "model-a")
	if err != nil {
		t.Fatalf("route selection returned error: %v", err)
	}
	if selected != "route-a" {
		t.Fatalf("route selection = %q, want %q", selected, "route-a")
	}

	selected, err = manager.SelectSubGroup(group, "unknown-model")
	if err != nil {
		t.Fatalf("default-pool selection returned error: %v", err)
	}
	if selected != "fallback" {
		t.Fatalf("default-pool selection = %q, want %q", selected, "fallback")
	}
}

// TestSubGroupSelectorUnmatchedRouteWithoutDefaultPool proves a sub-group with
// route models cannot receive any other model when no unrestricted pool exists.
func TestSubGroupSelectorUnmatchedRouteWithoutDefaultPool(t *testing.T) {
	group := testAggregateGroup(
		1,
		testSubGroup(10, "route-a", 1, []string{"model-a"}),
	)
	manager := newTestManager(group, 10)

	if _, err := manager.SelectSubGroup(group, "unknown-model"); err == nil {
		t.Fatal("unmatched model selected a route-restricted sub-group")
	}
}

// TestSubGroupSelectorExplicitRouteWeightZeroDoesNotFallback proves a
// weight-zero explicit route cannot fall back to an unmatched association.
func TestSubGroupSelectorExplicitRouteWeightZeroDoesNotFallback(t *testing.T) {
	group := testAggregateGroup(
		1,
		testSubGroup(10, "disabled-route", 0, []string{"model-a"}),
		testSubGroup(20, "unmatched", 1, nil),
	)
	manager := newTestManager(group, 10, 20)

	if _, err := manager.SelectSubGroup(group, "model-a"); err == nil {
		t.Fatal("explicit route with only weight-zero candidate should not fall back")
	}
}

// TestSubGroupSelectorExplicitRouteWithoutActiveKeysDoesNotFallback proves a
// matched route remains isolated even when its only candidate has no active key.
func TestSubGroupSelectorExplicitRouteWithoutActiveKeysDoesNotFallback(t *testing.T) {
	group := testAggregateGroup(
		1,
		testSubGroup(10, "inactive-route", 1, []string{"model-a"}),
		testSubGroup(20, "default", 1, nil),
	)
	manager := newTestManager(group, 20)
	selector := manager.selectors[group.ID]

	if got := candidateIDs(selector.finalCandidates("model-a")); len(got) != 1 || got[0] != 10 {
		t.Fatalf("matched candidate IDs = %v, want [10]", got)
	}
	if _, err := manager.SelectSubGroup(group, "model-a"); err == nil {
		t.Fatal("matched route without active keys fell back to the default pool")
	}
}

// TestSubGroupSelectorTriesOtherActiveCandidate proves an unavailable weighted
// candidate is skipped in favor of another final candidate with active keys.
func TestSubGroupSelectorTriesOtherActiveCandidate(t *testing.T) {
	group := testAggregateGroup(
		1,
		testSubGroup(10, "inactive", 10, []string{"model-a"}),
		testSubGroup(20, "active", 1, []string{"model-a"}),
	)
	manager := newTestManager(group, 20)

	selected, err := manager.SelectSubGroup(group, "model-a")
	if err != nil {
		t.Fatalf("selection returned error: %v", err)
	}
	if selected != "active" {
		t.Fatalf("selection = %q, want active candidate %q", selected, "active")
	}
}

// TestSubGroupSelectorIsolatesCandidateSetState proves smooth-round-robin
// state is shared by equal ID sets but isolated between different sets.
func TestSubGroupSelectorIsolatesCandidateSetState(t *testing.T) {
	group := testAggregateGroup(
		1,
		testSubGroup(10, "shared", 1, []string{"first", "other"}),
		testSubGroup(20, "first-only", 1, []string{"first"}),
		testSubGroup(30, "other-only", 1, []string{"other"}),
	)
	manager := newTestManager(group, 10, 20, 30)
	selector := manager.selectors[group.ID]

	if got := selector.selectNext("first"); got != "shared" {
		t.Fatalf("first candidate-set selection = %q, want %q", got, "shared")
	}
	if got := selector.selectNext("other"); got != "shared" {
		t.Fatalf("second candidate-set selection = %q, want %q", got, "shared")
	}
	if got := selector.selectNext("first"); got != "first-only" {
		t.Fatalf("isolated candidate-set selection = %q, want %q", got, "first-only")
	}

	if len(selector.states) != 2 {
		t.Fatalf("selector state count = %d, want 2 candidate-set states", len(selector.states))
	}
}

// TestSubGroupSelectorDefaultWeightDistribution proves the default candidate
// set preserves the expected smooth weighted distribution.
func TestSubGroupSelectorDefaultWeightDistribution(t *testing.T) {
	group := testAggregateGroup(
		1,
		testSubGroup(10, "heavy", 3, nil),
		testSubGroup(20, "light", 1, nil),
	)
	manager := newTestManager(group, 10, 20)

	counts := map[string]int{}
	for i := 0; i < 8; i++ {
		selected, err := manager.SelectSubGroup(group, "")
		if err != nil {
			t.Fatalf("selection %d returned error: %v", i, err)
		}
		counts[selected]++
	}

	if counts["heavy"] != 6 || counts["light"] != 2 {
		t.Fatalf("weight distribution = %#v, want heavy=6 and light=2", counts)
	}
}
