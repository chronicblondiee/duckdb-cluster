package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/chronicblondiee/duckdb-cluster/internal/ism"
	"gopkg.in/yaml.v3"
)

func (s *Server) handleCreateISMPolicy(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "policy name required"})
		return
	}

	if s.ismManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ISM is not enabled"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
		return
	}

	var p ism.Policy
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "yaml") {
		if err := yaml.Unmarshal(body, &p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid YAML: " + err.Error()})
			return
		}
	} else {
		if err := json.Unmarshal(body, &p); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
			return
		}
	}
	p.Name = name

	if err := s.ismManager.PutPolicy(&p); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "not found in states") ||
			strings.Contains(err.Error(), "unknown action") || strings.Contains(err.Error(), "duplicate") {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"acknowledged": true, "policy": name, "version": p.Version})
}

func (s *Server) handleGetISMPolicy(w http.ResponseWriter, r *http.Request) {
	if s.ismManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ISM is not enabled"})
		return
	}

	name := r.PathValue("name")
	p, err := s.ismManager.GetPolicy(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeleteISMPolicy(w http.ResponseWriter, r *http.Request) {
	if s.ismManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ISM is not enabled"})
		return
	}

	name := r.PathValue("name")
	if err := s.ismManager.DeletePolicy(name); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": true})
}

func (s *Server) handleListISMPolicies(w http.ResponseWriter, r *http.Request) {
	if s.ismManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ISM is not enabled"})
		return
	}

	policies := s.ismManager.ListPolicies()
	writeJSON(w, http.StatusOK, map[string]any{"policies": policies})
}

func (s *Server) handleAttachISMPolicy(w http.ResponseWriter, r *http.Request) {
	if s.ismManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ISM is not enabled"})
		return
	}

	indexName := r.PathValue("index")
	if indexName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "index name required"})
		return
	}

	var req struct {
		Policy string `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	if req.Policy == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "policy field is required"})
		return
	}

	if err := s.ismManager.AttachPolicy(indexName, req.Policy); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": true, "index": indexName, "policy": req.Policy})
}

func (s *Server) handleDetachISMPolicy(w http.ResponseWriter, r *http.Request) {
	if s.ismManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ISM is not enabled"})
		return
	}

	indexName := r.PathValue("index")
	if err := s.ismManager.DetachPolicy(indexName); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "no ISM policy") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": true, "index": indexName})
}

func (s *Server) handleISMStatus(w http.ResponseWriter, r *http.Request) {
	if s.ismManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ISM is not enabled"})
		return
	}

	indexName := r.PathValue("index")
	status, err := s.ismManager.GetStatus(indexName)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleISMStatusAll(w http.ResponseWriter, r *http.Request) {
	if s.ismManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ISM is not enabled"})
		return
	}

	statuses := s.ismManager.GetAllStatuses()
	writeJSON(w, http.StatusOK, map[string]any{"statuses": statuses})
}

func (s *Server) handleISMRetry(w http.ResponseWriter, r *http.Request) {
	if s.ismManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ISM is not enabled"})
		return
	}

	indexName := r.PathValue("index")
	if err := s.ismManager.RetryFailed(indexName); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no ISM policy") {
			status = http.StatusNotFound
		}
		if strings.Contains(err.Error(), "not in a failed state") {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": true, "index": indexName})
}
