package services

import (
	"fmt"
	"gpt-load/internal/models"
	"gpt-load/internal/store"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
)

// SubGroupManager manages weighted round-robin selection for all aggregate groups
type SubGroupManager struct {
	store     store.Store
	selectors map[uint]*selector
	mu        sync.RWMutex
}

// subGroupItem represents a sub-group with its weight and current weight for round-robin
type subGroupItem struct {
	name          string
	subGroupID    uint
	weight        int
	currentWeight int
	routeModels   map[string]struct{}
}

// NewSubGroupManager creates a new sub-group manager service
func NewSubGroupManager(store store.Store) *SubGroupManager {
	return &SubGroupManager{
		store:     store,
		selectors: make(map[uint]*selector),
	}
}

// SelectSubGroup selects an appropriate sub-group for the given aggregate group
func (m *SubGroupManager) SelectSubGroup(group *models.Group, requestModel string) (string, error) {
	if group.GroupType != "aggregate" {
		return "", nil
	}

	selector := m.getSelector(group)
	if selector == nil {
		return "", fmt.Errorf("no valid sub-groups available for aggregate group '%s'", group.Name)
	}

	selectedName := selector.selectNext(requestModel)
	if selectedName == "" {
		return "", fmt.Errorf("no sub-groups with active keys for aggregate group '%s'", group.Name)
	}

	logrus.WithFields(logrus.Fields{
		"aggregate_group": group.Name,
		"selected_group":  selectedName,
	}).Debug("Selected sub-group from aggregate")

	return selectedName, nil
}

// RebuildSelectors rebuild all selectors based on the incoming group
func (m *SubGroupManager) RebuildSelectors(groups map[string]*models.Group) {
	newSelectors := make(map[uint]*selector)

	for _, group := range groups {
		if group.GroupType == "aggregate" && len(group.SubGroups) > 0 {
			if sel := m.createSelector(group); sel != nil {
				newSelectors[group.ID] = sel
			}
		}
	}

	m.mu.Lock()
	m.selectors = newSelectors
	m.mu.Unlock()

	logrus.WithField("new_count", len(newSelectors)).Debug("Rebuilt selectors for aggregate groups")
}

// getSelector retrieves or creates a selector for the aggregate group
func (m *SubGroupManager) getSelector(group *models.Group) *selector {
	m.mu.RLock()
	if sel, exists := m.selectors[group.ID]; exists {
		m.mu.RUnlock()
		return sel
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	if sel, exists := m.selectors[group.ID]; exists {
		return sel
	}

	sel := m.createSelector(group)
	if sel != nil {
		m.selectors[group.ID] = sel
		logrus.WithFields(logrus.Fields{
			"group_id":        group.ID,
			"group_name":      group.Name,
			"sub_group_count": len(sel.subGroups),
		}).Debug("Created sub-group selector")
	}

	return sel
}

// createSelector creates a new selector for an aggregate group
func (m *SubGroupManager) createSelector(group *models.Group) *selector {
	if group.GroupType != "aggregate" || len(group.SubGroups) == 0 {
		return nil
	}

	var items []subGroupItem
	for _, sg := range group.SubGroups {
		items = append(items, subGroupItem{
			name:        sg.SubGroupName,
			subGroupID:  sg.SubGroupID,
			weight:      sg.Weight,
			routeModels: modelSet(models.DecodeModelList(sg.RouteModels)),
		})
	}

	if len(items) == 0 {
		return nil
	}

	return &selector{
		groupID:   group.ID,
		groupName: group.Name,
		subGroups: items,
		states:    make(map[string]*selectionState),
		store:     m.store,
	}
}

// selector encapsulates the weighted round-robin algorithm for a single aggregate group
type selector struct {
	groupID   uint
	groupName string
	subGroups []subGroupItem
	states    map[string]*selectionState
	store     store.Store
	mu        sync.Mutex
}

type selectionState struct {
	subGroups []subGroupItem
}

// selectNext uses weighted round-robin algorithm to select a sub-group with active keys
func (s *selector) selectNext(requestModel string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	candidates := s.finalCandidates(requestModel)
	if len(candidates) == 0 {
		return ""
	}

	state := s.getState(candidates)
	if len(state.subGroups) == 1 {
		if s.hasActiveKeys(state.subGroups[0].subGroupID) {
			return state.subGroups[0].name
		}
		logrus.WithFields(logrus.Fields{
			"group_id":   state.subGroups[0].subGroupID,
			"group_name": state.subGroups[0].name,
		}).Debug("Single sub-group has no active keys")
		return ""
	}

	attempted := make(map[uint]bool)
	for len(attempted) < len(state.subGroups) {
		item := state.selectByWeight()
		if item == nil {
			break
		}

		if attempted[item.subGroupID] {
			continue
		}
		attempted[item.subGroupID] = true

		if s.hasActiveKeys(item.subGroupID) {
			logrus.WithFields(logrus.Fields{
				"aggregate_group": s.groupName,
				"selected_group":  item.name,
				"attempts":        len(attempted),
			}).Debug("Selected sub-group with active keys")
			return item.name
		}

		logrus.WithFields(logrus.Fields{
			"group_id":   item.subGroupID,
			"group_name": item.name,
			"attempts":   len(attempted),
		}).Debug("Sub-group has no active keys, trying next")
	}

	logrus.WithFields(logrus.Fields{
		"aggregate_group":  s.groupName,
		"total_sub_groups": len(state.subGroups),
	}).Warn("No sub-groups with active keys available")

	return ""
}

func (s *selector) finalCandidates(requestModel string) []subGroupItem {
	routeMatched := false
	if requestModel != "" {
		for _, item := range s.subGroups {
			if containsModel(item.routeModels, requestModel) {
				routeMatched = true
				break
			}
		}
	}

	candidates := make([]subGroupItem, 0, len(s.subGroups))
	for _, item := range s.subGroups {
		if routeMatched {
			if !containsModel(item.routeModels, requestModel) {
				continue
			}
		} else if len(item.routeModels) > 0 {
			continue
		}
		if item.weight <= 0 {
			continue
		}
		candidates = append(candidates, item)
	}
	return candidates
}

func (s *selector) getState(candidates []subGroupItem) *selectionState {
	key := candidateSetKey(candidates)
	if state, exists := s.states[key]; exists {
		return state
	}

	items := make([]subGroupItem, len(candidates))
	copy(items, candidates)
	state := &selectionState{subGroups: items}
	s.states[key] = state
	return state
}

// selectByWeight implements smooth weighted round-robin algorithm.
func (s *selectionState) selectByWeight() *subGroupItem {
	totalWeight := 0
	var best *subGroupItem

	for i := range s.subGroups {
		item := &s.subGroups[i]
		totalWeight += item.weight
		item.currentWeight += item.weight

		if best == nil || item.currentWeight > best.currentWeight {
			best = item
		}
	}

	if best == nil || totalWeight <= 0 {
		return nil
	}

	best.currentWeight -= totalWeight
	return best
}

func modelSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func containsModel(configuredModels map[string]struct{}, requestModel string) bool {
	_, exists := configuredModels[requestModel]
	return exists
}

func candidateSetKey(candidates []subGroupItem) string {
	ids := make([]uint, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.subGroupID)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})

	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatUint(uint64(id), 10)
	}
	return strings.Join(parts, ",")
}

// hasActiveKeys checks if a sub-group has available API keys
func (s *selector) hasActiveKeys(groupID uint) bool {
	key := fmt.Sprintf("group:%d:active_keys", groupID)
	length, err := s.store.LLen(key)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"group_id": groupID,
			"error":    err,
		}).Debug("Error checking active keys, assuming available")
		return true
	}
	return length > 0
}
