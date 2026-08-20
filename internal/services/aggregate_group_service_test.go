package services

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gpt-load/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var aggregateServiceTestDBID atomic.Uint64

type legacyGroupSubGroup struct {
	ID         uint `gorm:"primaryKey;autoIncrement"`
	GroupID    uint `gorm:"not null;uniqueIndex:idx_group_sub"`
	SubGroupID uint `gorm:"not null;uniqueIndex:idx_group_sub"`
	Weight     int  `gorm:"default:0"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (legacyGroupSubGroup) TableName() string {
	return "group_sub_groups"
}

func openAggregateServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf(
		"file:%s-%d?mode=memory&cache=shared",
		strings.ReplaceAll(t.Name(), "/", "-"),
		aggregateServiceTestDBID.Add(1),
	)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get test database handle: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return db
}

func newAggregateServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db := openAggregateServiceTestDB(t)
	if err := db.AutoMigrate(&models.Group{}, &models.GroupSubGroup{}, &models.APIKey{}); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return db
}

func createAggregateServiceTestGroups(t *testing.T, db *gorm.DB) (models.Group, models.Group) {
	t.Helper()

	standard := models.Group{
		Name:               "standard-one",
		GroupType:          "standard",
		ChannelType:        "openai",
		TestModel:          "gpt-test",
		ValidationEndpoint: "/v1/chat/completions",
		Upstreams:          datatypes.JSON("[]"),
		HeaderRules:        datatypes.JSON("[]"),
	}
	aggregate := models.Group{
		Name:        "aggregate-one",
		GroupType:   "aggregate",
		ChannelType: "openai",
		TestModel:   "-",
		Upstreams:   datatypes.JSON("[]"),
		HeaderRules: datatypes.JSON("[]"),
	}

	if err := db.Create(&standard).Error; err != nil {
		t.Fatalf("create standard group: %v", err)
	}
	if err := db.Create(&aggregate).Error; err != nil {
		t.Fatalf("create aggregate group: %v", err)
	}
	return standard, aggregate
}

// TestValidateSubGroupsNormalizesModelLists proves API input becomes the
// canonical list representation before it reaches persistence.
func TestValidateSubGroupsNormalizesModelLists(t *testing.T) {
	db := newAggregateServiceTestDB(t)
	standard, _ := createAggregateServiceTestGroups(t, db)
	service := NewAggregateGroupService(db, &GroupManager{})

	result, err := service.ValidateSubGroups(context.Background(), "openai", []SubGroupInput{{
		GroupID:     standard.ID,
		Weight:      1,
		RouteModels: []string{" model-a ", "model-a", "", "Model-B", " model-b "},
	}}, "")
	if err != nil {
		t.Fatalf("ValidateSubGroups() returned error: %v", err)
	}

	if got := models.DecodeModelList(result.SubGroups[0].RouteModels); !reflect.DeepEqual(
		got,
		[]string{"model-a", "Model-B", "model-b"},
	) {
		t.Fatalf("normalized route models = %#v", got)
	}
}

// TestAddAndUpdateSubGroupPersistCanonicalModelLists proves add and unified
// update write the trimmed/deduplicated route list as a JSON array.
func TestAddAndUpdateSubGroupPersistCanonicalModelLists(t *testing.T) {
	db := newAggregateServiceTestDB(t)
	standard, aggregate := createAggregateServiceTestGroups(t, db)
	service := NewAggregateGroupService(db, &GroupManager{})

	err := service.AddSubGroups(context.Background(), aggregate.ID, []SubGroupInput{{
		GroupID:     standard.ID,
		Weight:      3,
		RouteModels: []string{" model-a ", "model-a"},
	}})
	if err != nil {
		t.Fatalf("AddSubGroups() returned error: %v", err)
	}

	var relation models.GroupSubGroup
	if err := db.Where("group_id = ? AND sub_group_id = ?", aggregate.ID, standard.ID).
		First(&relation).Error; err != nil {
		t.Fatalf("load added relation: %v", err)
	}
	if got := string(relation.RouteModels); got != `["model-a"]` {
		t.Fatalf("stored route models = %s, want canonical array", got)
	}

	err = service.UpdateSubGroupConfig(context.Background(), aggregate.ID, standard.ID, SubGroupUpdateInput{
		Weight:      4,
		RouteModels: []string{" model-b ", ""},
	})
	if err != nil {
		t.Fatalf("UpdateSubGroupConfig() returned error: %v", err)
	}

	if err := db.First(&relation, relation.ID).Error; err != nil {
		t.Fatalf("reload updated relation: %v", err)
	}
	if got := string(relation.RouteModels); got != `["model-b"]` {
		t.Fatalf("updated route models = %s, want canonical array", got)
	}
}

// TestGetSubGroupsTreatsHistoricalNullRouteModelsAsEmpty proves old nullable
// route lists are exposed to callers as non-nil empty lists.
func TestGetSubGroupsTreatsHistoricalNullRouteModelsAsEmpty(t *testing.T) {
	db := newAggregateServiceTestDB(t)
	standard, aggregate := createAggregateServiceTestGroups(t, db)
	if err := db.Create(&models.GroupSubGroup{
		GroupID:     aggregate.ID,
		SubGroupID:  standard.ID,
		Weight:      1,
		RouteModels: nil,
	}).Error; err != nil {
		t.Fatalf("create historical relation: %v", err)
	}

	service := NewAggregateGroupService(db, &GroupManager{})
	subGroups, err := service.GetSubGroups(context.Background(), aggregate.ID)
	if err != nil {
		t.Fatalf("GetSubGroups() returned error: %v", err)
	}
	if len(subGroups) != 1 {
		t.Fatalf("sub-group count = %d, want 1", len(subGroups))
	}
	if subGroups[0].RouteModels == nil || len(subGroups[0].RouteModels) != 0 {
		t.Fatalf("route models = %#v, want non-nil empty list", subGroups[0].RouteModels)
	}
}

// TestUpdateSubGroupConfigAllowsUnchangedValues proves saving the same
// complete configuration remains successful on drivers that report no rows changed.
func TestUpdateSubGroupConfigAllowsUnchangedValues(t *testing.T) {
	db := newAggregateServiceTestDB(t)
	standard, aggregate := createAggregateServiceTestGroups(t, db)
	service := NewAggregateGroupService(db, &GroupManager{})

	config := SubGroupInput{
		GroupID:     standard.ID,
		Weight:      3,
		RouteModels: []string{"model-a"},
	}
	if err := service.AddSubGroups(context.Background(), aggregate.ID, []SubGroupInput{config}); err != nil {
		t.Fatalf("AddSubGroups() returned error: %v", err)
	}

	if err := service.UpdateSubGroupConfig(context.Background(), aggregate.ID, standard.ID, SubGroupUpdateInput{
		Weight:      config.Weight,
		RouteModels: config.RouteModels,
	}); err != nil {
		t.Fatalf("unchanged UpdateSubGroupConfig() returned error: %v", err)
	}
}

// TestUpdateSubGroupWeightPreservesRouteModels proves the legacy endpoint only
// changes weight and leaves the existing route rules untouched.
func TestUpdateSubGroupWeightPreservesRouteModels(t *testing.T) {
	db := newAggregateServiceTestDB(t)
	standard, aggregate := createAggregateServiceTestGroups(t, db)
	service := NewAggregateGroupService(db, &GroupManager{})

	if err := service.AddSubGroups(context.Background(), aggregate.ID, []SubGroupInput{{
		GroupID:     standard.ID,
		Weight:      3,
		RouteModels: []string{"model-a"},
	}}); err != nil {
		t.Fatalf("AddSubGroups() returned error: %v", err)
	}

	if err := service.UpdateSubGroupWeight(context.Background(), aggregate.ID, standard.ID, 7); err != nil {
		t.Fatalf("UpdateSubGroupWeight() returned error: %v", err)
	}

	var relation models.GroupSubGroup
	if err := db.Where("group_id = ? AND sub_group_id = ?", aggregate.ID, standard.ID).
		First(&relation).Error; err != nil {
		t.Fatalf("load updated relation: %v", err)
	}
	if relation.Weight != 7 {
		t.Fatalf("updated weight = %d, want 7", relation.Weight)
	}
	if got := models.DecodeModelList(relation.RouteModels); !reflect.DeepEqual(got, []string{"model-a"}) {
		t.Fatalf("route models after legacy update = %#v, want [model-a]", got)
	}
}

// TestGroupSubGroupAutoMigrateAddsRouteModelsColumn proves an existing SQLite
// association table gains the route JSON column through AutoMigrate.
func TestGroupSubGroupAutoMigrateAddsRouteModelsColumn(t *testing.T) {
	db := openAggregateServiceTestDB(t)
	if err := db.AutoMigrate(&legacyGroupSubGroup{}); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	if err := db.AutoMigrate(&models.GroupSubGroup{}); err != nil {
		t.Fatalf("migrate current schema: %v", err)
	}

	columnTypes, err := db.Migrator().ColumnTypes(&models.GroupSubGroup{})
	if err != nil {
		t.Fatalf("inspect migrated columns: %v", err)
	}
	columns := make(map[string]struct{}, len(columnTypes))
	for _, column := range columnTypes {
		columns[strings.ToLower(column.Name())] = struct{}{}
	}
	for _, name := range []string{"route_models"} {
		if _, exists := columns[name]; !exists {
			t.Fatalf("migrated schema is missing %s column", name)
		}
	}

	if err := db.Exec(
		"INSERT INTO group_sub_groups (group_id, sub_group_id, weight) VALUES (?, ?, ?)",
		1,
		2,
		1,
	).Error; err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}
	var relation models.GroupSubGroup
	if err := db.First(&relation, "group_id = ? AND sub_group_id = ?", 1, 2).Error; err != nil {
		t.Fatalf("read migrated legacy row: %v", err)
	}
	if len(models.DecodeModelList(relation.RouteModels)) != 0 {
		t.Fatal("NULL migrated model lists should decode as empty")
	}
}
