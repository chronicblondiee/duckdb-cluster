package index

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

// FieldType represents a DuckDB column type.
type FieldType string

const (
	FieldTypeVarchar   FieldType = "VARCHAR"
	FieldTypeInteger   FieldType = "INTEGER"
	FieldTypeBigInt    FieldType = "BIGINT"
	FieldTypeFloat     FieldType = "FLOAT"
	FieldTypeDouble    FieldType = "DOUBLE"
	FieldTypeBoolean   FieldType = "BOOLEAN"
	FieldTypeTimestamp  FieldType = "TIMESTAMP"
	FieldTypeBlob      FieldType = "BLOB"
	FieldTypeStruct    FieldType = "STRUCT"
	FieldTypeList      FieldType = "LIST"
	FieldTypeMap       FieldType = "MAP"
)

// FieldMapping describes a single field in an index mapping.
type FieldMapping struct {
	Name     string                  `json:"name"`
	Type     FieldType               `json:"type"`
	Fields   map[string]*FieldMapping `json:"fields,omitempty"`    // for STRUCT
	ItemType *FieldMapping           `json:"item_type,omitempty"` // for LIST element type
	KeyType  *FieldMapping           `json:"key_type,omitempty"`  // for MAP key type
	ValType  *FieldMapping           `json:"val_type,omitempty"`  // for MAP value type
}

// Mapping describes the schema of documents in an index.
type Mapping struct {
	Fields  map[string]*FieldMapping `json:"fields"`
	Dynamic bool                     `json:"dynamic"` // true = auto-add new fields
}

// DefaultDocTable is the table name used for document-based ingestion.
const DefaultDocTable = "_docs"

// InferFieldFromValue infers a FieldMapping from a Go value (as decoded from JSON).
func InferFieldFromValue(name string, value any) *FieldMapping {
	if value == nil {
		return &FieldMapping{Name: name, Type: FieldTypeVarchar}
	}

	switch v := value.(type) {
	case bool:
		return &FieldMapping{Name: name, Type: FieldTypeBoolean}
	case float64:
		// JSON numbers are float64. If it looks like an integer, use BIGINT.
		if v == math.Trunc(v) && !math.IsInf(v, 0) && !math.IsNaN(v) {
			return &FieldMapping{Name: name, Type: FieldTypeBigInt}
		}
		return &FieldMapping{Name: name, Type: FieldTypeDouble}
	case string:
		return &FieldMapping{Name: name, Type: FieldTypeVarchar}
	case map[string]any:
		fm := &FieldMapping{
			Name:   name,
			Type:   FieldTypeStruct,
			Fields: make(map[string]*FieldMapping, len(v)),
		}
		for k, val := range v {
			fm.Fields[k] = InferFieldFromValue(k, val)
		}
		return fm
	case []any:
		fm := &FieldMapping{Name: name, Type: FieldTypeList}
		if len(v) > 0 {
			fm.ItemType = InferFieldFromValue("_item", v[0])
		} else {
			fm.ItemType = &FieldMapping{Name: "_item", Type: FieldTypeVarchar}
		}
		return fm
	default:
		return &FieldMapping{Name: name, Type: FieldTypeVarchar}
	}
}

// InferMappingFromDocument infers a full Mapping from a JSON document.
func InferMappingFromDocument(doc map[string]any) *Mapping {
	m := &Mapping{
		Fields:  make(map[string]*FieldMapping, len(doc)),
		Dynamic: true,
	}
	for k, v := range doc {
		m.Fields[k] = InferFieldFromValue(k, v)
	}
	return m
}

// DuckDBType returns the DuckDB SQL type string for this field mapping.
func (fm *FieldMapping) DuckDBType() string {
	switch fm.Type {
	case FieldTypeStruct:
		if len(fm.Fields) == 0 {
			return "VARCHAR" // fallback for empty struct
		}
		// Sort fields for deterministic output
		names := make([]string, 0, len(fm.Fields))
		for n := range fm.Fields {
			names = append(names, n)
		}
		sort.Strings(names)
		parts := make([]string, len(names))
		for i, n := range names {
			parts[i] = fmt.Sprintf("%s %s", quoteIdent(n), fm.Fields[n].DuckDBType())
		}
		return fmt.Sprintf("STRUCT(%s)", strings.Join(parts, ", "))
	case FieldTypeList:
		if fm.ItemType != nil {
			return fmt.Sprintf("%s[]", fm.ItemType.DuckDBType())
		}
		return "VARCHAR[]"
	case FieldTypeMap:
		kt := "VARCHAR"
		vt := "VARCHAR"
		if fm.KeyType != nil {
			kt = fm.KeyType.DuckDBType()
		}
		if fm.ValType != nil {
			vt = fm.ValType.DuckDBType()
		}
		return fmt.Sprintf("MAP(%s, %s)", kt, vt)
	default:
		return string(fm.Type)
	}
}

// GenerateCreateTableSQL generates a CREATE TABLE statement from the mapping.
func (m *Mapping) GenerateCreateTableSQL(tableName string) string {
	if len(m.Fields) == 0 {
		return ""
	}

	// Sort field names for deterministic output
	names := make([]string, 0, len(m.Fields))
	for n := range m.Fields {
		names = append(names, n)
	}
	sort.Strings(names)

	cols := make([]string, len(names))
	for i, n := range names {
		cols[i] = fmt.Sprintf("    %s %s", quoteIdent(n), m.Fields[n].DuckDBType())
	}

	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n%s\n)", quoteIdent(tableName), strings.Join(cols, ",\n"))
}

// NewFields returns field names present in doc but not in the current mapping.
func (m *Mapping) NewFields(doc map[string]any) []string {
	var newFields []string
	for k := range doc {
		if _, exists := m.Fields[k]; !exists {
			newFields = append(newFields, k)
		}
	}
	sort.Strings(newFields)
	return newFields
}

// EvolveSchema checks for new fields in the document and, if dynamic mapping is enabled,
// adds them to the mapping and runs ALTER TABLE on all shards.
func (m *Mapping) EvolveSchema(ctx context.Context, manager *shard.Manager, tableName string, doc map[string]any) error {
	newFields := m.NewFields(doc)
	if len(newFields) == 0 {
		return nil
	}

	if !m.Dynamic {
		return fmt.Errorf("dynamic mapping disabled: unknown fields %v", newFields)
	}

	for _, name := range newFields {
		fm := InferFieldFromValue(name, doc[name])
		m.Fields[name] = fm

		alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s",
			quoteIdent(tableName), quoteIdent(name), fm.DuckDBType())
		if err := manager.ExecuteOnAll(ctx, alterSQL); err != nil {
			return fmt.Errorf("evolve schema for field %q: %w", name, err)
		}
	}

	return nil
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
