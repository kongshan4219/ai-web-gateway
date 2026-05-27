package main

import (
	"os"
	"strconv"
)

// Config holds all configuration loaded from environment variables.
type Config struct {
	ListenAddr       string // Go gateway listen address, e.g. ":9000"
	SitesDir         string // Root directory for deployed projects
	RuntimeDir       string // Runtime data directory (.runtime)
	PortsFile        string // Port registry JSON file
	NginxConfDir     string // Nginx conf.d directory for project configs
	SSLUploadDir     string // Directory for uploaded SSL cert/key
	PortStart        int    // Backend port range start
	PortEnd          int    // Backend port range end
	MaxUploadBytes   int64  // Max upload size in bytes
	PublicHost       string // Public-facing host:port for URLs
	NginxReloadCmd   string // Command to reload nginx
	LogMaxLines      int    // Max log lines to keep per project
	MaxRestarts      int    // Max auto-restarts
	RestartDelaySec  int    // Seconds between restart attempts
	PortReadyTimeout int    // Seconds to wait for port readiness
	StartScript      string // Name of the start script (default: start.sh)
}

// LoadConfig reads configuration from environment variables.
func LoadConfig() *Config {
	return &Config{
		ListenAddr:       envStr("LISTEN_ADDR", ":9000"),
		SitesDir:         envStr("SITES_DIR", "/sites"),
		RuntimeDir:       envStr("RUNTIME_DIR", "/sites/.runtime"),
		PortsFile:        envStr("PORTS_FILE", "/sites/.runtime/ports.json"),
		NginxConfDir:     envStr("NGINX_CONF_DIR", "/etc/nginx/conf.d/projects"),
		SSLUploadDir:     envStr("SSL_UPLOAD_DIR", "/sites/.runtime/ssl"),
		PortStart:        envInt("BACKEND_PORT_START", 10000),
		PortEnd:          envInt("BACKEND_PORT_END", 19999),
		MaxUploadBytes:   int64(envInt("MAX_UPLOAD_MB", 512)) * 1024 * 1024,
		PublicHost:       envStr("PUBLIC_HOST", "192.168.254.240:8080"),
		NginxReloadCmd:   envStr("NGINX_RELOAD_CMD", "nginx -s reload"),
		LogMaxLines:      envInt("LOG_MAX_LINES", 200),
		MaxRestarts:      envInt("PROC_MAX_RESTARTS", 5),
		RestartDelaySec:  envInt("PROC_RESTART_DELAY", 3),
		PortReadyTimeout: envInt("PORT_READY_TIMEOUT", 15),
		StartScript:      envStr("START_SCRIPT", "start.sh"),
	}
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
