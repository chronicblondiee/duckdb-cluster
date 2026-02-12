package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type queryRequest struct {
	SQL          string `json:"sql"`
	PartitionKey string `json:"partition_key"`
}

type queryResponse struct {
	Success      bool             `json:"success"`
	Columns      []string         `json:"columns,omitempty"`
	Rows         []map[string]any `json:"rows"`
	RowsAffected int64            `json:"rows_affected,omitempty"`
	ShardID      int              `json:"shard_id"`
	Error        string           `json:"error,omitempty"`
}

type shardInfo struct {
	ID     int    `json:"id"`
	Path   string `json:"path"`
	Status string `json:"status"`
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, queryResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	if req.SQL == "" {
		writeJSON(w, http.StatusBadRequest, queryResponse{Error: "sql field is required"})
		return
	}

	result, err := s.Cluster.Router.Route(r.Context(), req.SQL, req.PartitionKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, queryResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, queryResponse{
		Success:      true,
		Columns:      result.Columns,
		Rows:         result.Rows,
		RowsAffected: result.RowsAffected,
		ShardID:      result.ShardID,
	})
}

func (s *Server) handleListShards(w http.ResponseWriter, r *http.Request) {
	count := s.Cluster.Manager.ShardCount()
	shards := make([]shardInfo, count)
	for i := 0; i < count; i++ {
		sh := s.Cluster.Manager.GetShard(i)
		shards[i] = shardInfo{
			ID:     sh.ID,
			Path:   sh.Path,
			Status: "open",
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"shards": shards})
}

func (s *Server) handleAddShard(w http.ResponseWriter, r *http.Request) {
	sh, err := s.Cluster.Manager.AddShard()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, shardInfo{
		ID:     sh.ID,
		Path:   sh.Path,
		Status: "open",
	})
}

func (s *Server) handleRemoveShard(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid shard id"})
		return
	}
	if err := s.Cluster.Manager.RemoveShard(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status, err := s.Cluster.Status()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

