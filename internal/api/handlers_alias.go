package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

type createAliasRequest struct {
	Indices []string `json:"indices"`
}

func (s *Server) handleCreateAlias(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "alias name required"})
		return
	}

	var req createAliasRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if len(req.Indices) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "indices field is required"})
		return
	}

	// Validate that referenced indices exist
	for _, idx := range req.Indices {
		if _, err := s.registry.GetAny(idx); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "referenced index not found: " + idx,
			})
			return
		}
	}

	if err := s.aliasManager.Put(name, req.Indices); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "invalid") {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"acknowledged": true, "alias": name})
}

func (s *Server) handleGetAlias(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	a, err := s.aliasManager.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleDeleteAlias(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.aliasManager.Delete(name); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": true})
}

func (s *Server) handleListAliases(w http.ResponseWriter, r *http.Request) {
	aliases := s.aliasManager.List()
	writeJSON(w, http.StatusOK, map[string]any{"aliases": aliases})
}
