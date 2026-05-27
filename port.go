package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
)

// PortManager handles allocation and release of backend ports.
// Port assignments are persisted to a JSON file for durability across restarts.
type PortManager struct {
	cfg  *Config
	mu   sync.Mutex
	data map[string]int // project name -> port
	used map[int]bool   // set of allocated ports
}

// NewPortManager creates a new PortManager and loads existing assignments from disk.
func NewPortManager(cfg *Config) *PortManager {
	pm := &PortManager{
		cfg:  cfg,
		data: make(map[string]int),
		used: make(map[int]bool),
	}
	pm.load()
	return pm
}

func (pm *PortManager) load() {
	data, err := os.ReadFile(pm.cfg.PortsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("port-manager: failed to read ports file: %v", err)
		return
	}
	if err := json.Unmarshal(data, &pm.data); err != nil {
		log.Printf("port-manager: failed to parse ports file: %v", err)
		return
	}
	for _, port := range pm.data {
		pm.used[port] = true
	}
	log.Printf("port-manager: loaded %d entries", len(pm.data))
}

func (pm *PortManager) save() {
	data, err := json.MarshalIndent(pm.data, "", "  ")
	if err != nil {
		log.Printf("port-manager: failed to marshal ports: %v", err)
		return
	}
	tmpFile := pm.cfg.PortsFile + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		log.Printf("port-manager: failed to write ports file: %v", err)
		return
	}
	if err := os.Rename(tmpFile, pm.cfg.PortsFile); err != nil {
		log.Printf("port-manager: failed to rename ports file: %v", err)
		return
	}
}

// Allocate assigns a port to a project. Returns existing port if already allocated.
func (pm *PortManager) Allocate(project string) (int, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if port, ok := pm.data[project]; ok {
		return port, nil
	}

	for port := pm.cfg.PortStart; port <= pm.cfg.PortEnd; port++ {
		if !pm.used[port] {
			pm.data[project] = port
			pm.used[port] = true
			pm.save()
			log.Printf("port-manager: allocated port %d for %s", port, project)
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", pm.cfg.PortStart, pm.cfg.PortEnd)
}

// Release frees the port assigned to a project.
func (pm *PortManager) Release(project string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	port, ok := pm.data[project]
	if !ok {
		return
	}
	delete(pm.data, project)
	delete(pm.used, port)
	pm.save()
	log.Printf("port-manager: released port %d for %s", port, project)
}

// Get returns the port assigned to a project, or 0 if not found.
func (pm *PortManager) Get(project string) int {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.data[project]
}
