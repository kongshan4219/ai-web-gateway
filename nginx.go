package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// NginxManager manages dynamic Nginx configuration for projects.
type NginxManager struct {
	cfg     *Config
	sslDir  string
	hasSSL  bool
	mu      sync.Mutex
}

// NewNginxManager creates a new NginxManager.
func NewNginxManager(cfg *Config) *NginxManager {
	return &NginxManager{
		cfg:    cfg,
		sslDir: cfg.SSLUploadDir,
	}
}

// CheckExistingSSL checks for an existing SSL certificate on startup.
func (nm *NginxManager) CheckExistingSSL() {
	certPath := filepath.Join(nm.sslDir, "cert.pem")
	keyPath := filepath.Join(nm.sslDir, "key.pem")
	if fileExists(certPath) && fileExists(keyPath) {
		nm.hasSSL = true
		log.Println("nginx: found existing SSL certificate")
	}
}

// InstallSSL writes the provided certificate and key, then reloads nginx.
func (nm *NginxManager) InstallSSL(certPEM, keyPEM []byte) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	if err := os.MkdirAll(nm.sslDir, 0700); err != nil {
		return fmt.Errorf("create ssl dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(nm.sslDir, "cert.pem"), certPEM, 0644); err != nil {
		return fmt.Errorf("write cert: %w", err)
	}
	if err := os.WriteFile(filepath.Join(nm.sslDir, "key.pem"), keyPEM, 0600); err != nil {
		return fmt.Errorf("write key: %w", err)
	}

	// Write SSL nginx config
	if err := nm.writeSSLConfig(); err != nil {
		return fmt.Errorf("write ssl config: %w", err)
	}

	nm.hasSSL = true
	log.Println("nginx: SSL certificate installed")
	return nm.reload()
}

// RemoveSSL removes the SSL certificate and disables HTTPS.
func (nm *NginxManager) RemoveSSL() error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	os.Remove(filepath.Join(nm.sslDir, "cert.pem"))
	os.Remove(filepath.Join(nm.sslDir, "key.pem"))

	sslConf := "/etc/nginx/conf.d/ssl.conf"
	os.Remove(sslConf)

	nm.hasSSL = false
	log.Println("nginx: SSL certificate removed")
	return nm.reload()
}

// HasSSL returns true if SSL is configured.
func (nm *NginxManager) HasSSL() bool {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	return nm.hasSSL
}

func (nm *NginxManager) writeSSLConfig() error {
	certPath := filepath.Join(nm.sslDir, "cert.pem")
	keyPath := filepath.Join(nm.sslDir, "key.pem")

	sslConf := fmt.Sprintf(`# Auto-generated SSL config
server {
    listen 443 ssl;
    http2 on;

    ssl_certificate     %s;
    ssl_certificate_key %s;
    ssl_protocols       TLSv1.2 TLSv1.3;
    ssl_ciphers         HIGH:!aNULL:!MD5;

    include /etc/nginx/conf.d/projects/*.conf;

    location / {
        return 200 '{"status":"ok","service":"ai-web-gateway"}';
        add_header Content-Type application/json;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:9000;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
`, certPath, keyPath)

	return os.WriteFile("/etc/nginx/conf.d/ssl.conf", []byte(sslConf), 0644)
}

// WriteProjectConfig writes (or updates) the Nginx config for a specific project.
func (nm *NginxManager) WriteProjectConfig(project string, projectDir string, port int, isDynamic bool) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	var location string
	if isDynamic {
		location = fmt.Sprintf(`# Auto-generated config for dynamic project: %s
location /deploy/%s/ {
    rewrite ^/deploy/%s/(.*) /$1 break;
    proxy_pass http://127.0.0.1:%d;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_read_timeout 300s;
    proxy_send_timeout 300s;
}
`, project, project, port)
	} else {
		location = fmt.Sprintf(`# Auto-generated config for static project: %s
location /deploy/%s/ {
    alias %s/;
    try_files $uri $uri/ /index.html;
    index index.html;
}
`, project, project, projectDir)
	}

	confPath := filepath.Join(nm.cfg.NginxConfDir, project+".conf")
	if err := os.WriteFile(confPath, []byte(location), 0644); err != nil {
		return fmt.Errorf("write nginx config for %s: %w", project, err)
	}
	log.Printf("nginx: wrote config for %s (dynamic=%v)", project, isDynamic)
	return nil
}

// RemoveProjectConfig deletes the Nginx config for a project.
func (nm *NginxManager) RemoveProjectConfig(project string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	confPath := filepath.Join(nm.cfg.NginxConfDir, project+".conf")
	if err := os.Remove(confPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove nginx config for %s: %w", project, err)
	}
	log.Printf("nginx: removed config for %s", project)
	return nil
}

// Reload tells Nginx to reload its configuration.
func (nm *NginxManager) Reload() error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	return nm.reload()
}

func (nm *NginxManager) reload() error {
	// Test config first
	testCmd := exec.Command("nginx", "-t")
	testOut, err := testCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nginx config test failed: %v\n%s", err, strings.TrimSpace(string(testOut)))
	}

	// Reload
	parts := strings.Fields(nm.cfg.NginxReloadCmd)
	cmd := exec.Command(parts[0], parts[1:]...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nginx reload failed: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	log.Println("nginx: reloaded successfully")
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
