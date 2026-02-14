package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/chronicblondiee/duckdb-cluster/internal/index"
)

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template name required"})
		return
	}

	var tmpl index.Template
	if err := json.NewDecoder(r.Body).Decode(&tmpl); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	tmpl.Name = name

	if err := s.templateManager.Put(&tmpl); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "required") {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"acknowledged": true, "template": name})
}

func (s *Server) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	tmpl, err := s.templateManager.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, tmpl)
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.templateManager.Delete(name); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": true})
}

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	templates := s.templateManager.List()
	writeJSON(w, http.StatusOK, map[string]any{"templates": templates})
}
