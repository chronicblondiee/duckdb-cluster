package index

import (
	"context"
	"strings"
	"testing"

	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

func TestInferFieldFromValue(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		wantType FieldType
	}{
		{"string", "hello", FieldTypeVarchar},
		{"int", float64(42), FieldTypeBigInt},
		{"float", float64(3.14), FieldTypeDouble},
		{"bool", true, FieldTypeBoolean},
		{"nil", nil, FieldTypeVarchar},
		{"object", map[string]any{"key": "val"}, FieldTypeStruct},
		{"array", []any{1, 2, 3}, FieldTypeList},
	}

	for _, tt := range tests {
		fm := InferFieldFromValue(tt.name, tt.value)
		if fm.Type != tt.wantType {
			t.Errorf("InferFieldFromValue(%q, %v) type = %s, want %s", tt.name, tt.value, fm.Type, tt.wantType)
		}
		if fm.Name != tt.name {
			t.Errorf("InferFieldFromValue(%q, %v) name = %s", tt.name, tt.value, fm.Name)
		}
	}
}

func TestInferMappingFromDocument(t *testing.T) {
	doc := map[string]any{
		"name":  "Alice",
		"age":   float64(30),
		"score": float64(99.5),
		"active": true,
	}

	m := InferMappingFromDocument(doc)
	if len(m.Fields) != 4 {
		t.Errorf("expected 4 fields, got %d", len(m.Fields))
	}
	if m.Fields["name"].Type != FieldTypeVarchar {
		t.Errorf("name type = %s, want VARCHAR", m.Fields["name"].Type)
	}
	if m.Fields["age"].Type != FieldTypeBigInt {
		t.Errorf("age type = %s, want BIGINT", m.Fields["age"].Type)
	}
	if m.Fields["score"].Type != FieldTypeDouble {
		t.Errorf("score type = %s, want DOUBLE", m.Fields["score"].Type)
	}
}

func TestNestedStructMapping(t *testing.T) {
	doc := map[string]any{
		"address": map[string]any{
			"city":  "NYC",
			"zip":   float64(10001),
		},
	}

	m := InferMappingFromDocument(doc)
	fm := m.Fields["address"]
	if fm.Type != FieldTypeStruct {
		t.Fatalf("expected STRUCT, got %s", fm.Type)
	}
	if len(fm.Fields) != 2 {
		t.Fatalf("expected 2 sub-fields, got %d", len(fm.Fields))
	}
	if fm.Fields["city"].Type != FieldTypeVarchar {
		t.Errorf("city type = %s, want VARCHAR", fm.Fields["city"].Type)
	}

	// Check DuckDB type output
	duckType := fm.DuckDBType()
	if !strings.Contains(duckType, "STRUCT") {
		t.Errorf("expected STRUCT in DuckDB type, got %s", duckType)
	}
}

func TestListMapping(t *testing.T) {
	doc := map[string]any{
		"tags": []any{"go", "duckdb"},
	}

	m := InferMappingFromDocument(doc)
	fm := m.Fields["tags"]
	if fm.Type != FieldTypeList {
		t.Fatalf("expected LIST, got %s", fm.Type)
	}
	if fm.ItemType == nil {
		t.Fatal("expected item type")
	}
	if fm.ItemType.Type != FieldTypeVarchar {
		t.Errorf("item type = %s, want VARCHAR", fm.ItemType.Type)
	}

	duckType := fm.DuckDBType()
	if duckType != "VARCHAR[]" {
		t.Errorf("expected VARCHAR[], got %s", duckType)
	}
}

func TestGenerateCreateTableSQL(t *testing.T) {
	m := &Mapping{
		Fields: map[string]*FieldMapping{
			"id":   {Name: "id", Type: FieldTypeBigInt},
			"name": {Name: "name", Type: FieldTypeVarchar},
		},
	}

	sql := m.GenerateCreateTableSQL("_docs")
	if !strings.Contains(sql, "CREATE TABLE") {
		t.Error("expected CREATE TABLE in SQL")
	}
	if !strings.Contains(sql, `"id" BIGINT`) {
		t.Errorf("expected id BIGINT in SQL, got: %s", sql)
	}
	if !strings.Contains(sql, `"name" VARCHAR`) {
		t.Errorf("expected name VARCHAR in SQL, got: %s", sql)
	}
}

func TestMappingEvolution(t *testing.T) {
	dir := t.TempDir()
	m, err := shard.NewManager(dir, 2)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.CloseAll()

	ctx := context.Background()

	// Create initial table
	mapping := &Mapping{
		Fields: map[string]*FieldMapping{
			"id": {Name: "id", Type: FieldTypeBigInt},
		},
		Dynamic: true,
	}
	createSQL := mapping.GenerateCreateTableSQL("_docs")
	if err := m.ExecuteOnAll(ctx, createSQL); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}

	// Evolve with new field
	doc := map[string]any{"id": float64(1), "name": "test"}
	if err := mapping.EvolveSchema(ctx, m, "_docs", doc); err != nil {
		t.Fatalf("EvolveSchema: %v", err)
	}

	if _, ok := mapping.Fields["name"]; !ok {
		t.Error("expected 'name' field to be added to mapping")
	}

	// Verify column exists by inserting
	s := m.GetShard(0)
	_, err = s.Execute(ctx, `INSERT INTO "_docs" ("id", "name") VALUES (1, 'test')`)
	if err != nil {
		t.Fatalf("INSERT after evolution: %v", err)
	}
}

func TestMappingRejectUnknown(t *testing.T) {
	dir := t.TempDir()
	m, err := shard.NewManager(dir, 1)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.CloseAll()

	mapping := &Mapping{
		Fields: map[string]*FieldMapping{
			"id": {Name: "id", Type: FieldTypeBigInt},
		},
		Dynamic: false, // reject unknown fields
	}

	doc := map[string]any{"id": float64(1), "unknown_field": "val"}
	err = mapping.EvolveSchema(context.Background(), m, "_docs", doc)
	if err == nil {
		t.Error("expected error for unknown field with dynamic=false")
	}
}
