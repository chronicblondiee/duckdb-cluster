package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/chronicblondiee/duckdb-cluster/internal/index"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// --- Index CRUD ---

type createIndexRequest struct {
	Settings *index.Settings `json:"settings,omitempty"`
	Mappings *index.Mapping  `json:"mappings,omitempty"`
}

func (s *Server) handleCreateIndex(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "index name required"})
		return
	}

	if err := index.ValidateName(name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var req createIndexRequest
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}

	settings := index.Settings{
		ShardCount:        3, // default
		PartitionKeyField: "_id",
	}
	if req.Settings != nil {
		if req.Settings.ShardCount > 0 {
			settings.ShardCount = req.Settings.ShardCount
		}
		if req.Settings.PartitionKeyField != "" {
			settings.PartitionKeyField = req.Settings.PartitionKeyField
		}
	}

	idx, err := s.registry.Create(name, settings)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	// Merge explicit mapping over any template-applied mapping
	if req.Mappings != nil {
		for k, v := range req.Mappings.Fields {
			idx.Mapping.Fields[k] = v
		}
		idx.Mapping.Dynamic = req.Mappings.Dynamic
		s.registry.SaveCatalog()
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"acknowledged": true,
		"index":        name,
		"shards":       settings.ShardCount,
	})
}

func (s *Server) handleListIndices(w http.ResponseWriter, r *http.Request) {
	indices := s.registry.List()
	writeJSON(w, http.StatusOK, map[string]any{"indices": indices})
}

func (s *Server) handleGetIndex(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	idx, err := s.registry.GetAny(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	shardCount := 0
	var shards []shardInfo
	if idx.Manager != nil && idx.Meta.State == index.StateOpen {
		shardCount = idx.Manager.ShardCount()
		shards = make([]shardInfo, shardCount)
		for i := 0; i < shardCount; i++ {
			sh := idx.Manager.GetShard(i)
			shards[i] = shardInfo{ID: sh.ID, Path: sh.Path, Status: "open"}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":     idx.Meta.Name,
		"state":    idx.Meta.State,
		"settings": idx.Meta.Settings,
		"mapping":  idx.Mapping,
		"shards":   shards,
		"created":  idx.Meta.CreatedAt,
	})
}

func (s *Server) handleDeleteIndex(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "_default" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cannot delete _default index"})
		return
	}

	if err := s.registry.Delete(name); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	// Also clean up schema registry
	s.schemaRegistry.Delete(name)

	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": true})
}

func (s *Server) handleCloseIndex(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.registry.CloseIndex(name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": true})
}

func (s *Server) handleOpenIndex(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.registry.OpenIndex(name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": true})
}

// --- Mapping ---

func (s *Server) handleGetMapping(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	idx, err := s.registry.GetAny(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, idx.Mapping)
}

func (s *Server) handlePutMapping(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	idx, err := s.registry.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	var mapping index.Mapping
	if err := json.NewDecoder(r.Body).Decode(&mapping); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid mapping JSON: " + err.Error()})
		return
	}

	// Merge new fields into existing mapping
	if idx.Mapping == nil {
		idx.Mapping = &mapping
	} else {
		for k, v := range mapping.Fields {
			idx.Mapping.Fields[k] = v
		}
		// Allow updating dynamic flag
		idx.Mapping.Dynamic = mapping.Dynamic
	}

	// If the mapping has fields and the _docs table doesn't exist yet, create it
	if len(idx.Mapping.Fields) > 0 {
		createSQL := idx.Mapping.GenerateCreateTableSQL(index.DefaultDocTable)
		if createSQL != "" {
			idx.Manager.ExecuteOnAll(r.Context(), createSQL)
		}
	}

	s.registry.SaveCatalog()

	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": true})
}

// --- Document Ingestion ---

func (s *Server) handleIndexDoc(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	idx, err := s.registry.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	contentType := r.Header.Get("Content-Type")

	var doc map[string]any

	if strings.HasPrefix(contentType, "application/x-protobuf") {
		// Protobuf document
		data, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
			return
		}
		doc, err = s.schemaRegistry.DeserializeProtobuf(name, data)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "deserialize protobuf: " + err.Error()})
			return
		}
	} else {
		// JSON document (default)
		if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
	}

	result, err := idx.IndexDocument(r.Context(), doc)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Persist mapping updates
	s.registry.SaveCatalog()

	writeJSON(w, http.StatusCreated, map[string]any{
		"result":   "created",
		"shard_id": result.ShardID,
	})
}

func (s *Server) handleBulkDocs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	idx, err := s.registry.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	// Read newline-delimited JSON
	var docs []map[string]any
	decoder := json.NewDecoder(r.Body)
	for decoder.More() {
		var doc map[string]any
		if err := decoder.Decode(&doc); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON in bulk body: " + err.Error()})
			return
		}
		docs = append(docs, doc)
	}

	affected, failed, err := idx.IndexDocumentBulk(r.Context(), docs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	s.registry.SaveCatalog()

	writeJSON(w, http.StatusOK, map[string]any{
		"took":      0,
		"items":     len(docs),
		"succeeded": len(docs) - failed,
		"failed":    failed,
		"affected":  affected,
	})
}

// --- Schema Registry ---

type registerSchemaRequest struct {
	Proto string `json:"proto"` // base64-encoded FileDescriptorSet
}

func (s *Server) handlePutSchema(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	// Verify index exists
	if _, err := s.registry.GetAny(name); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	contentType := r.Header.Get("Content-Type")
	var fdsBytes []byte

	if strings.HasPrefix(contentType, "application/x-protobuf") {
		// Raw binary FileDescriptorSet
		var err error
		fdsBytes, err = io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
			return
		}
	} else {
		// JSON with base64-encoded proto field
		var req registerSchemaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
		if req.Proto == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "proto field required (base64-encoded FileDescriptorSet)"})
			return
		}
		var err error
		fdsBytes, err = base64.StdEncoding.DecodeString(req.Proto)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid base64: " + err.Error()})
			return
		}
	}

	var fds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(fdsBytes, &fds); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid FileDescriptorSet: " + err.Error()})
		return
	}

	if err := s.schemaRegistry.Register(name, &fds); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Optionally infer mapping from proto
	mapping, err := index.InferMappingFromProtoDescriptor(&fds)
	if err == nil {
		idx, _ := s.registry.Get(name)
		if idx != nil && len(idx.Mapping.Fields) == 0 {
			idx.Mapping = mapping
			s.registry.SaveCatalog()
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"acknowledged": true,
		"files":        len(fds.GetFile()),
	})
}

func (s *Server) handleGetSchema(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	fds, err := s.schemaRegistry.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	// Return info about the registered schema
	files := make([]map[string]any, len(fds.GetFile()))
	for i, f := range fds.GetFile() {
		msgs := make([]string, len(f.GetMessageType()))
		for j, m := range f.GetMessageType() {
			msgs[j] = m.GetName()
		}
		files[i] = map[string]any{
			"name":     f.GetName(),
			"package":  f.GetPackage(),
			"messages": msgs,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"index": name,
		"files": files,
	})
}

func (s *Server) handleDeleteSchema(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.schemaRegistry.Delete(name)
	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": true})
}

// resolveIndex resolves an index by name, defaulting to _default.
// If the name matches an alias with a single index, resolves to that index.
func (s *Server) resolveIndex(name string) (*index.Index, error) {
	if name == "" {
		name = "_default"
	}

	// Check alias first
	if s.aliasManager != nil {
		if indices := s.aliasManager.Resolve(name); len(indices) == 1 {
			name = indices[0]
		}
	}

	return s.registry.Get(name)
}

// resolveMultipleIndices resolves a comma-separated or wildcard index spec to multiple indices.
func (s *Server) resolveMultipleIndices(spec string) ([]*index.Index, error) {
	// Expand aliases
	if s.aliasManager != nil {
		if indices := s.aliasManager.Resolve(spec); len(indices) > 0 {
			var result []*index.Index
			for _, name := range indices {
				idx, err := s.registry.Get(name)
				if err != nil {
					return nil, err
				}
				result = append(result, idx)
			}
			return result, nil
		}
	}

	// Split comma-separated names
	parts := strings.Split(spec, ",")
	seen := make(map[string]bool)
	var result []*index.Index

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "*") {
			// Wildcard expansion
			for _, meta := range s.registry.List() {
				if index.MatchGlob(part, meta.Name) && !seen[meta.Name] {
					idx, err := s.registry.Get(meta.Name)
					if err != nil {
						continue // skip closed indices
					}
					seen[meta.Name] = true
					result = append(result, idx)
				}
			}
		} else {
			// Check if this part is an alias
			if s.aliasManager != nil {
				if aliasIndices := s.aliasManager.Resolve(part); len(aliasIndices) > 0 {
					for _, name := range aliasIndices {
						if !seen[name] {
							idx, err := s.registry.Get(name)
							if err != nil {
								return nil, err
							}
							seen[name] = true
							result = append(result, idx)
						}
					}
					continue
				}
			}

			if !seen[part] {
				idx, err := s.registry.Get(part)
				if err != nil {
					return nil, err
				}
				seen[part] = true
				result = append(result, idx)
			}
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no indices matched %q", spec)
	}
	return result, nil
}

// resolveRouter resolves the router for a given index name.
func (s *Server) resolveRouter(name string) (*index.Index, error) {
	return s.resolveIndex(name)
}

// --- Helper to write location-based error messages ---

func writeIndexNotFound(w http.ResponseWriter, name string) {
	writeJSON(w, http.StatusNotFound, map[string]string{
		"error": fmt.Sprintf("index %q not found or closed", name),
	})
}
