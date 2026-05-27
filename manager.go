package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProjectInfo holds the public-facing information about a deployed project.
type ProjectInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"` // "dynamic" or "static"
	Status     string `json:"status"`
	Port       int    `json:"port,omitempty"`
	Error      string `json:"error,omitempty"`
	Restarts   int    `json:"restart_count,omitempty"`
	Updated    string `json:"updated"`
	URL        string `json:"url"`
	LogTail    []string `json:"log_tail,omitempty"`
}

// Manager orchestrates the deployment lifecycle.
type Manager struct {
	cfg    *Config
	ports  *PortManager
	procs  *ProcessManager
	nginx  *NginxManager
}

// NewManager creates a new Manager.
func NewManager(cfg *Config, ports *PortManager, procs *ProcessManager, nginx *NginxManager) *Manager {
	return &Manager{
		cfg:   cfg,
		ports: ports,
		procs: procs,
		nginx: nginx,
	}
}

// Deploy extracts a tar.gz archive, deploys the project, and configures nginx.
func (m *Manager) Deploy(project string, data []byte) (*ProjectInfo, error) {
	// Validate project name
	if !safeProjectName(project) {
		return nil, fmt.Errorf("invalid project name: %s", project)
	}

	projectDir := filepath.Join(m.cfg.SitesDir, project)
	tmpDir := filepath.Join(m.cfg.SitesDir, fmt.Sprintf("%s.tmp.%d", project, os.Getpid()))

	// Clean up on failure
	defer os.RemoveAll(tmpDir)

	// Extract to temp directory
	if err := extractTarGz(data, tmpDir); err != nil {
		return nil, fmt.Errorf("extract failed: %w", err)
	}

	// Check for start.sh
	hasStart := fileExists(filepath.Join(tmpDir, m.cfg.StartScript))
	if hasStart {
		if err := os.Chmod(filepath.Join(tmpDir, m.cfg.StartScript), 0755); err != nil {
			return nil, fmt.Errorf("chmod start.sh: %w", err)
		}
	}

	// Atomic replacement of existing project
	if _, err := os.Stat(projectDir); err == nil {
		oldDir := filepath.Join(m.cfg.SitesDir, fmt.Sprintf("%s.old.%d", project, os.Getpid()))
		if err := os.Rename(projectDir, oldDir); err != nil {
			return nil, fmt.Errorf("backup existing: %w", err)
		}
		os.RemoveAll(oldDir)
	}

	if err := os.Rename(tmpDir, projectDir); err != nil {
		return nil, fmt.Errorf("move to target: %w", err)
	}

	// Handle dynamic/static project
	if hasStart {
		return m.deployDynamic(project, projectDir)
	}
	return m.deployStatic(project, projectDir)
}

func (m *Manager) deployDynamic(project string, projectDir string) (*ProjectInfo, error) {
	// Allocate port
	port, err := m.ports.Allocate(project)
	if err != nil {
		return nil, fmt.Errorf("port allocation: %w", err)
	}

	// Write nginx config
	if err := m.nginx.WriteProjectConfig(project, projectDir, port, true); err != nil {
		return nil, fmt.Errorf("nginx config: %w", err)
	}

	// Start process
	m.procs.Register(project, projectDir, port)

	// Reload nginx
	m.nginx.Reload()

	log.Printf("deploy: %s (dynamic, port=%d)", project, port)
	return m.GetInfo(project), nil
}

func (m *Manager) deployStatic(project string, projectDir string) (*ProjectInfo, error) {
	// Write nginx config for static serving
	if err := m.nginx.WriteProjectConfig(project, projectDir, 0, false); err != nil {
		return nil, fmt.Errorf("nginx config: %w", err)
	}

	// Reload nginx
	m.nginx.Reload()

	log.Printf("deploy: %s (static)", project)
	return m.GetInfo(project), nil
}

// Remove stops the project, removes files, and cleans up nginx config.
func (m *Manager) Remove(project string) error {
	// Stop process
	m.procs.Remove(project)

	// Release port
	m.ports.Release(project)

	// Remove nginx config
	m.nginx.RemoveProjectConfig(project)

	// Remove project files
	projectDir := filepath.Join(m.cfg.SitesDir, project)
	if err := os.RemoveAll(projectDir); err != nil {
		return fmt.Errorf("remove project dir: %w", err)
	}

	// Reload nginx
	m.nginx.Reload()

	log.Printf("delete: %s", project)
	return nil
}

// Restart restarts a dynamic project's backend process.
func (m *Manager) Restart(project string) error {
	mp := m.procs.Get(project)
	if mp == nil {
		return fmt.Errorf("no running process for %s", project)
	}
	mp.Restart(m.cfg)
	return nil
}

// GetInfo returns the public info for a project.
func (m *Manager) GetInfo(project string) *ProjectInfo {
	mp := m.procs.Get(project)
	projectDir := filepath.Join(m.cfg.SitesDir, project)

	if mp != nil {
		return &ProjectInfo{
			Name:     project,
			Type:     "dynamic",
			Status:   mp.Status,
			Port:     mp.Port,
			Error:    mp.Error,
			Restarts: mp.RestartCount,
			Updated:  mp.Updated,
			URL:      fmt.Sprintf("http://%s/deploy/%s/", m.cfg.PublicHost, project),
			LogTail:  mp.GetLogTail(50),
		}
	}

	if fi, err := os.Stat(projectDir); err == nil && fi.IsDir() {
		return &ProjectInfo{
			Name:    project,
			Type:    "static",
			Status:  "serving",
			Updated: fi.ModTime().UTC().Format(time.RFC3339),
			URL:     fmt.Sprintf("http://%s/deploy/%s/", m.cfg.PublicHost, project),
		}
	}

	return nil
}

// ListAll returns info for all deployed projects.
func (m *Manager) ListAll() []*ProjectInfo {
	var result []*ProjectInfo

	// Active dynamic processes
	for _, mp := range m.procs.List() {
		result = append(result, &ProjectInfo{
			Name:     mp.Project,
			Type:     "dynamic",
			Status:   mp.Status,
			Port:     mp.Port,
			Error:    mp.Error,
			Restarts: mp.RestartCount,
			Updated:  mp.Updated,
			URL:      fmt.Sprintf("http://%s/deploy/%s/", m.cfg.PublicHost, mp.Project),
		})
	}

	// Scan disk for static projects not in process list
	entries, err := os.ReadDir(m.cfg.SitesDir)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		// Skip if already in process list
		if m.procs.Get(name) != nil {
			continue
		}
		projectDir := filepath.Join(m.cfg.SitesDir, name)
		fi, err := os.Stat(projectDir)
		if err != nil {
			continue
		}
		result = append(result, &ProjectInfo{
			Name:    name,
			Type:    "static",
			Status:  "serving",
			Updated: fi.ModTime().UTC().Format(time.RFC3339),
			URL:     fmt.Sprintf("http://%s/deploy/%s/", m.cfg.PublicHost, name),
		})
	}

	return result
}

// RecoverAll scans disk for deployed projects and restores them.
func (m *Manager) RecoverAll() {
	entries, err := os.ReadDir(m.cfg.SitesDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		projectDir := filepath.Join(m.cfg.SitesDir, name)
		startScript := filepath.Join(projectDir, m.cfg.StartScript)

		if fileExists(startScript) {
			port, err := m.ports.Allocate(name)
			if err != nil {
				log.Printf("recover: %s: port allocation failed: %v", name, err)
				continue
			}

			// Write nginx config
			if err := m.nginx.WriteProjectConfig(name, projectDir, port, true); err != nil {
				log.Printf("recover: %s: nginx config failed: %v", name, err)
				continue
			}

			// Start process
			m.procs.Register(name, projectDir, port)
			log.Printf("recover: %s (dynamic, port=%d)", name, port)
		} else {
			// Static project
			if err := m.nginx.WriteProjectConfig(name, projectDir, 0, false); err != nil {
				log.Printf("recover: %s: nginx config failed: %v", name, err)
				continue
			}
			log.Printf("recover: %s (static)", name)
		}
	}

	// Reload nginx once after all configs are written
	if len(entries) > 0 {
		m.nginx.Reload()
	}
}

// safeProjectName validates project name (alphanumeric, hyphens, underscores, no path traversal).
func safeProjectName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// extractTarGz extracts a .tar.gz archive to a destination directory.
func extractTarGz(data []byte, dest string) error {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}

	gr, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		// Try without gzip (raw tar)
		return extractTar(data, dest)
	}
	defer gr.Close()
	return extractTarReader(tar.NewReader(gr), dest)
}

func extractTar(data []byte, dest string) error {
	return extractTarReader(tar.NewReader(strings.NewReader(string(data))), dest)
}

func extractTarReader(tr *tar.Reader, dest string) error {
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar read: %w", err)
		}

		// Security: reject absolute paths and path traversal
		name := header.Name
		if filepath.IsAbs(name) || strings.Contains(name, "..") {
			return fmt.Errorf("unsafe path in archive: %s", name)
		}

		target := filepath.Join(dest, name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("create dir %s: %w", name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("create parent dir for %s: %w", name, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("create file %s: %w", name, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write file %s: %w", name, err)
			}
			f.Close()
		case tar.TypeSymlink:
			// Skip symlinks for security
			log.Printf("skipping symlink: %s -> %s", name, header.Linkname)
		}
	}
	return nil
}
