package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[gateway] ")

	cfg := LoadConfig()

	// Ensure directories exist
	for _, d := range []string{cfg.SitesDir, cfg.RuntimeDir, cfg.NginxConfDir, cfg.SSLUploadDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			log.Fatalf("failed to create directory %s: %v", d, err)
		}
	}

	// Initialize subsystems
	portMgr := NewPortManager(cfg)
	nginxMgr := NewNginxManager(cfg)

	// Check for existing SSL cert on startup
	nginxMgr.CheckExistingSSL()

	procMgr := NewProcessManager(cfg)
	mgr := NewManager(cfg, portMgr, procMgr, nginxMgr)

	// Recover projects from disk
	log.Println("recovering projects from disk...")
	mgr.RecoverAll()

	// Build HTTP routes
	mux := http.NewServeMux()

	// Dashboard
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		handleDashboard(w, r, mgr)
	})

	// API routes
	mux.HandleFunc("/api/list", func(w http.ResponseWriter, r *http.Request) {
		handleList(w, r, mgr)
	})
	mux.HandleFunc("/api/ssl/upload", func(w http.ResponseWriter, r *http.Request) {
		handleSSLUpload(w, r, nginxMgr)
	})
	mux.HandleFunc("/api/ssl", func(w http.ResponseWriter, r *http.Request) {
		handleSSLDelete(w, r, nginxMgr)
	})
	// /api/{project} and sub-routes
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		handleAPI(w, r, mgr)
	})

	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: mux,
	}

	// Graceful shutdown on SIGTERM/SIGINT
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		log.Printf("received signal %v, shutting down...", sig)
		server.Close()
	}()

	log.Printf("listening on %s", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
	log.Println("server stopped")
}
