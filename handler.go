package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// ── API Routes ────────────────────────────────────────────────────────────────

// handleAPI dispatches /api/{project} and sub-routes.
func handleAPI(w http.ResponseWriter, r *http.Request, mgr *Manager) {
	path := strings.TrimPrefix(r.URL.Path, "/api/")
	path = strings.TrimPrefix(path, "/")

	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing project name"})
		return
	}

	parts := strings.SplitN(path, "/", 2)
	project := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "restart" && r.Method == http.MethodPost:
		handleRestart(w, r, mgr, project)
	case action == "" && r.Method == http.MethodGet:
		handleProjectStatus(w, r, mgr, project)
	case action == "" && r.Method == http.MethodPost:
		handleDeploy(w, r, mgr, project)
	case action == "" && r.Method == http.MethodDelete:
		handleDelete(w, r, mgr, project)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleList returns JSON for all deployed projects.
func handleList(w http.ResponseWriter, r *http.Request, mgr *Manager) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	projects := mgr.ListAll()
	sslStatus := "未配置"
	if mgr.nginx.HasSSL() {
		sslStatus = "已启用"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"projects":   projects,
		"count":      len(projects),
		"ssl_status": sslStatus,
	})
}

// handleProjectStatus returns status and logs for a project.
func handleProjectStatus(w http.ResponseWriter, r *http.Request, mgr *Manager, project string) {
	info := mgr.GetInfo(project)
	if info == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("project %s not found", project)})
		return
	}

	// Add extra detail: run start.sh status for dynamic projects
	if info.Type == "dynamic" {
		mp := mgr.procs.Get(project)
		if mp != nil {
			info.LogTail = mp.GetLogTail(50)
		}
	}

	writeJSON(w, http.StatusOK, info)
}

// handleDeploy accepts a tar.gz upload and deploys the project.
func handleDeploy(w http.ResponseWriter, r *http.Request, mgr *Manager, project string) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, mgr.cfg.MaxUploadBytes)

	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge,
			map[string]string{"error": fmt.Sprintf("upload too large (max %d MB)", mgr.cfg.MaxUploadBytes/(1024*1024))})
		return
	}
	defer r.Body.Close()

	if len(data) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty upload"})
		return
	}

	info, err := mgr.Deploy(project, data)
	if err != nil {
		log.Printf("deploy error: %s: %v", project, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, info)
}

// handleDelete stops and removes a project.
func handleDelete(w http.ResponseWriter, r *http.Request, mgr *Manager, project string) {
	if err := mgr.Remove(project); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "project": project})
}

// handleRestart restarts a project's backend process.
func handleRestart(w http.ResponseWriter, r *http.Request, mgr *Manager, project string) {
	if err := mgr.Restart(project); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restarting", "project": project})
}

// ── SSL Handlers ──────────────────────────────────────────────────────────────

// handleSSLUpload accepts an SSL certificate and key.
func handleSSLUpload(w http.ResponseWriter, r *http.Request, nm *NginxManager) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// Parse multipart form (max 10 MB for certs)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to parse form: " + err.Error()})
		return
	}

	certFile, _, err := r.FormFile("cert")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'cert' field"})
		return
	}
	defer certFile.Close()

	keyFile, _, err := r.FormFile("key")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing 'key' field"})
		return
	}
	defer keyFile.Close()

	certPEM, err := io.ReadAll(certFile)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read cert"})
		return
	}
	keyPEM, err := io.ReadAll(keyFile)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read key"})
		return
	}

	if err := nm.InstallSSL(certPEM, keyPEM); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ssl_installed"})
}

// handleSSLDelete removes the SSL certificate.
func handleSSLDelete(w http.ResponseWriter, r *http.Request, nm *NginxManager) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	if err := nm.RemoveSSL(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ssl_removed"})
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
