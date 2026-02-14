package index

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// SchemaRegistry stores protobuf FileDescriptorSets per index.
type SchemaRegistry struct {
	mu      sync.RWMutex
	schemas map[string]*descriptorpb.FileDescriptorSet
	baseDir string
}

// NewSchemaRegistry creates a new schema registry.
func NewSchemaRegistry(baseDir string) *SchemaRegistry {
	return &SchemaRegistry{
		schemas: make(map[string]*descriptorpb.FileDescriptorSet),
		baseDir: baseDir,
	}
}

// Register stores a FileDescriptorSet for an index and persists it to disk.
func (sr *SchemaRegistry) Register(indexName string, fds *descriptorpb.FileDescriptorSet) error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	// Validate the descriptor set
	if len(fds.GetFile()) == 0 {
		return fmt.Errorf("FileDescriptorSet contains no file descriptors")
	}

	// Persist to disk
	data, err := proto.Marshal(fds)
	if err != nil {
		return fmt.Errorf("marshal FileDescriptorSet: %w", err)
	}

	schemaPath := sr.schemaPath(indexName)
	if err := os.MkdirAll(filepath.Dir(schemaPath), 0o755); err != nil {
		return fmt.Errorf("create schema directory: %w", err)
	}
	if err := os.WriteFile(schemaPath, data, 0o644); err != nil {
		return fmt.Errorf("write schema file: %w", err)
	}

	sr.schemas[indexName] = fds
	slog.Info("protobuf schema registered", "index", indexName, "files", len(fds.GetFile()))
	return nil
}

// Get retrieves the FileDescriptorSet for an index.
func (sr *SchemaRegistry) Get(indexName string) (*descriptorpb.FileDescriptorSet, error) {
	sr.mu.RLock()
	defer sr.mu.RUnlock()

	fds, ok := sr.schemas[indexName]
	if !ok {
		return nil, fmt.Errorf("no protobuf schema registered for index %q", indexName)
	}
	return fds, nil
}

// Delete removes the schema for an index.
func (sr *SchemaRegistry) Delete(indexName string) {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	delete(sr.schemas, indexName)
	os.Remove(sr.schemaPath(indexName))
}

// LoadAll reads all persisted schema files on startup.
func (sr *SchemaRegistry) LoadAll() error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	indicesDir := filepath.Join(sr.baseDir, "indices")
	entries, err := os.ReadDir(indicesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read indices dir: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		indexName := e.Name()
		schemaPath := filepath.Join(indicesDir, indexName, "_schema.pb")
		data, err := os.ReadFile(schemaPath)
		if err != nil {
			continue // no schema file, skip
		}

		var fds descriptorpb.FileDescriptorSet
		if err := proto.Unmarshal(data, &fds); err != nil {
			slog.Error("failed to parse schema file", "index", indexName, "error", err)
			continue
		}

		sr.schemas[indexName] = &fds
		slog.Info("protobuf schema loaded", "index", indexName)
	}

	return nil
}

// DeserializeProtobuf deserializes protobuf bytes using the registered schema
// for the given index. Returns the message fields as a map.
func (sr *SchemaRegistry) DeserializeProtobuf(indexName string, data []byte) (map[string]any, error) {
	fds, err := sr.Get(indexName)
	if err != nil {
		return nil, err
	}

	// Build file descriptors
	files, err := protodesc.NewFiles(fds)
	if err != nil {
		return nil, fmt.Errorf("build file descriptors: %w", err)
	}

	// Find the first message type in the last file (convention: main message is in last file)
	var msgDesc protoreflect.MessageDescriptor
	lastFile := fds.GetFile()[len(fds.GetFile())-1]
	fileDesc, err := files.FindFileByPath(lastFile.GetName())
	if err != nil {
		return nil, fmt.Errorf("find file descriptor: %w", err)
	}

	msgs := fileDesc.Messages()
	if msgs.Len() == 0 {
		return nil, fmt.Errorf("no message types found in schema")
	}
	msgDesc = msgs.Get(0) // Use first message type

	// Create a dynamic message and unmarshal
	msg := dynamicpb.NewMessage(msgDesc)
	if err := proto.Unmarshal(data, msg); err != nil {
		return nil, fmt.Errorf("unmarshal protobuf message: %w", err)
	}

	// Convert to map
	return protoMessageToMap(msg), nil
}

// protoMessageToMap converts a protobuf message to a map[string]any.
func protoMessageToMap(msg *dynamicpb.Message) map[string]any {
	result := make(map[string]any)
	desc := msg.Descriptor()
	fields := desc.Fields()

	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		name := string(fd.Name())

		if !msg.Has(fd) {
			continue
		}

		val := msg.Get(fd)
		result[name] = protoValueToGo(fd, val)
	}

	return result
}

// protoValueToGo converts a protobuf value to a Go value.
func protoValueToGo(fd protoreflect.FieldDescriptor, val protoreflect.Value) any {
	if fd.IsList() {
		list := val.List()
		result := make([]any, list.Len())
		for i := 0; i < list.Len(); i++ {
			result[i] = protoScalarToGo(fd, list.Get(i))
		}
		return result
	}

	if fd.IsMap() {
		m := val.Map()
		result := make(map[string]any, m.Len())
		m.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
			key := fmt.Sprintf("%v", k.Value().Interface())
			result[key] = protoScalarToGo(fd.MapValue(), v)
			return true
		})
		return result
	}

	return protoScalarToGo(fd, val)
}

// protoScalarToGo converts a scalar protobuf value to a Go value.
func protoScalarToGo(fd protoreflect.FieldDescriptor, val protoreflect.Value) any {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return val.Bool()
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return int32(val.Int())
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return val.Int()
	case protoreflect.Uint32Kind:
		return int64(val.Uint())
	case protoreflect.Uint64Kind:
		return val.Uint()
	case protoreflect.FloatKind:
		return float32(val.Float())
	case protoreflect.DoubleKind:
		return val.Float()
	case protoreflect.StringKind:
		return val.String()
	case protoreflect.BytesKind:
		return val.Bytes()
	case protoreflect.EnumKind:
		return string(fd.Enum().Values().ByNumber(val.Enum()).Name())
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if dynMsg, ok := val.Message().Interface().(*dynamicpb.Message); ok {
			return protoMessageToMap(dynMsg)
		}
		return val.Message().Interface()
	default:
		return val.Interface()
	}
}

// InferMappingFromProtoDescriptor infers a Mapping from a protobuf FileDescriptorSet.
func InferMappingFromProtoDescriptor(fds *descriptorpb.FileDescriptorSet) (*Mapping, error) {
	files, err := protodesc.NewFiles(fds)
	if err != nil {
		return nil, fmt.Errorf("build file descriptors: %w", err)
	}

	// Find the first message type in the last file
	lastFile := fds.GetFile()[len(fds.GetFile())-1]
	fileDesc, err := files.FindFileByPath(lastFile.GetName())
	if err != nil {
		return nil, fmt.Errorf("find file descriptor: %w", err)
	}

	msgs := fileDesc.Messages()
	if msgs.Len() == 0 {
		return nil, fmt.Errorf("no message types found")
	}

	fields := protoFieldsToMapping(msgs.Get(0))
	return &Mapping{Fields: fields, Dynamic: true}, nil
}

// protoFieldsToMapping converts protobuf message fields to FieldMapping entries.
func protoFieldsToMapping(msgDesc protoreflect.MessageDescriptor) map[string]*FieldMapping {
	fields := msgDesc.Fields()
	result := make(map[string]*FieldMapping, fields.Len())

	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		name := string(fd.Name())
		fm := protoFieldToFieldMapping(fd)
		fm.Name = name
		result[name] = fm
	}

	return result
}

// protoFieldToFieldMapping converts a single protobuf field descriptor to a FieldMapping.
func protoFieldToFieldMapping(fd protoreflect.FieldDescriptor) *FieldMapping {
	if fd.IsMap() {
		return &FieldMapping{
			Type:    FieldTypeMap,
			KeyType: protoFieldToFieldMapping(fd.MapKey()),
			ValType: protoFieldToFieldMapping(fd.MapValue()),
		}
	}

	fm := &FieldMapping{}

	switch fd.Kind() {
	case protoreflect.BoolKind:
		fm.Type = FieldTypeBoolean
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		fm.Type = FieldTypeInteger
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind:
		fm.Type = FieldTypeBigInt
	case protoreflect.Uint64Kind:
		fm.Type = FieldTypeBigInt
	case protoreflect.FloatKind:
		fm.Type = FieldTypeFloat
	case protoreflect.DoubleKind:
		fm.Type = FieldTypeDouble
	case protoreflect.StringKind:
		fm.Type = FieldTypeVarchar
	case protoreflect.BytesKind:
		fm.Type = FieldTypeBlob
	case protoreflect.EnumKind:
		fm.Type = FieldTypeVarchar
	case protoreflect.MessageKind, protoreflect.GroupKind:
		fm.Type = FieldTypeStruct
		fm.Fields = protoFieldsToMapping(fd.Message())
	default:
		fm.Type = FieldTypeVarchar
	}

	if fd.IsList() {
		return &FieldMapping{
			Type:     FieldTypeList,
			ItemType: fm,
		}
	}

	return fm
}

func (sr *SchemaRegistry) schemaPath(indexName string) string {
	return filepath.Join(sr.baseDir, "indices", indexName, "_schema.pb")
}
