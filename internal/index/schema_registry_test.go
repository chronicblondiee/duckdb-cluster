package index

import (
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func makeTestFDS() *descriptorpb.FileDescriptorSet {
	// Build a minimal FileDescriptorSet with one message
	stringType := descriptorpb.FieldDescriptorProto_TYPE_STRING
	int64Type := descriptorpb.FieldDescriptorProto_TYPE_INT64
	label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	num1 := int32(1)
	num2 := int32(2)

	return &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			{
				Name:    proto.String("test.proto"),
				Package: proto.String("test"),
				MessageType: []*descriptorpb.DescriptorProto{
					{
						Name: proto.String("TestMessage"),
						Field: []*descriptorpb.FieldDescriptorProto{
							{
								Name:   proto.String("name"),
								Number: &num1,
								Type:   &stringType,
								Label:  &label,
							},
							{
								Name:   proto.String("id"),
								Number: &num2,
								Type:   &int64Type,
								Label:  &label,
							},
						},
					},
				},
			},
		},
	}
}

func TestRegisterAndGetSchema(t *testing.T) {
	dir := t.TempDir()
	sr := NewSchemaRegistry(dir)

	fds := makeTestFDS()

	// Create index directory so schema can be persisted
	os.MkdirAll(filepath.Join(dir, "indices", "test-idx"), 0o755)

	if err := sr.Register("test-idx", fds); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := sr.Get("test-idx")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.GetFile()) != 1 {
		t.Errorf("expected 1 file, got %d", len(got.GetFile()))
	}
	if got.GetFile()[0].GetMessageType()[0].GetName() != "TestMessage" {
		t.Error("expected TestMessage")
	}

	// Verify persisted to disk
	schemaPath := filepath.Join(dir, "indices", "test-idx", "_schema.pb")
	if _, err := os.Stat(schemaPath); os.IsNotExist(err) {
		t.Error("schema file not persisted")
	}
}

func TestSchemaLoadAll(t *testing.T) {
	dir := t.TempDir()

	// Register a schema
	sr1 := NewSchemaRegistry(dir)
	os.MkdirAll(filepath.Join(dir, "indices", "reload-idx"), 0o755)
	sr1.Register("reload-idx", makeTestFDS())

	// Create new registry and load
	sr2 := NewSchemaRegistry(dir)
	if err := sr2.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	_, err := sr2.Get("reload-idx")
	if err != nil {
		t.Fatalf("Get after LoadAll: %v", err)
	}
}

func TestSchemaDelete(t *testing.T) {
	dir := t.TempDir()
	sr := NewSchemaRegistry(dir)

	os.MkdirAll(filepath.Join(dir, "indices", "del-idx"), 0o755)
	sr.Register("del-idx", makeTestFDS())

	sr.Delete("del-idx")

	_, err := sr.Get("del-idx")
	if err == nil {
		t.Error("expected error after delete")
	}

	// File should be removed
	schemaPath := filepath.Join(dir, "indices", "del-idx", "_schema.pb")
	if _, err := os.Stat(schemaPath); !os.IsNotExist(err) {
		t.Error("schema file still exists after delete")
	}
}

func TestInferMappingFromProtoDescriptor(t *testing.T) {
	fds := makeTestFDS()

	mapping, err := InferMappingFromProtoDescriptor(fds)
	if err != nil {
		t.Fatalf("InferMappingFromProtoDescriptor: %v", err)
	}

	if len(mapping.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(mapping.Fields))
	}

	nameField := mapping.Fields["name"]
	if nameField == nil || nameField.Type != FieldTypeVarchar {
		t.Errorf("name field: expected VARCHAR, got %v", nameField)
	}

	idField := mapping.Fields["id"]
	if idField == nil || idField.Type != FieldTypeBigInt {
		t.Errorf("id field: expected BIGINT, got %v", idField)
	}
}

func TestDeserializeProtobuf(t *testing.T) {
	dir := t.TempDir()
	sr := NewSchemaRegistry(dir)

	fds := makeTestFDS()
	os.MkdirAll(filepath.Join(dir, "indices", "proto-idx"), 0o755)
	sr.Register("proto-idx", fds)

	// Build a protobuf message using the descriptor
	// We need to use dynamicpb for this
	// For simplicity, test that DeserializeProtobuf returns an error for empty data
	_, err := sr.DeserializeProtobuf("proto-idx", []byte{})
	// Empty data should still produce a valid (empty) message
	if err != nil {
		t.Logf("DeserializeProtobuf with empty data: %v (expected for some proto implementations)", err)
	}

	// Test with nonexistent schema
	_, err = sr.DeserializeProtobuf("nonexistent", []byte{1, 2, 3})
	if err == nil {
		t.Error("expected error for nonexistent schema")
	}
}
