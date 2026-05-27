package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ManagedProcess wraps a project's backend process lifecycle.
type ManagedProcess struct {
	Project      string
	Port         int
	ProjectDir   string
	Status       string // stopped, starting, running, failed
	Error        string
	Updated      string
	RestartCount int
	LogLines     []string

	mu      sync.Mutex
	cmd     *exec.Cmd
	cancel  chan struct{}
	logLock sync.Mutex
}

// ProcessManager manages all backend processes.
type ProcessManager struct {
	cfg   *Config
	mu    sync.Mutex
	procs map[string]*ManagedProcess
}

// NewProcessManager creates a new ProcessManager.
func NewProcessManager(cfg *Config) *ProcessManager {
	return &ProcessManager{
		cfg:   cfg,
		procs: make(map[string]*ManagedProcess),
	}
}

// Get returns a managed process by project name, or nil.
func (pm *ProcessManager) Get(project string) *ManagedProcess {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.procs[project]
}

// Register adds a new managed process and starts it.
func (pm *ProcessManager) Register(project string, projectDir string, port int) *ManagedProcess {
	pm.mu.Lock()
	// Stop existing process if any
	if old, ok := pm.procs[project]; ok {
		old.Stop()
	}
	mp := &ManagedProcess{
		Project:    project,
		Port:       port,
		ProjectDir: projectDir,
		Status:     "starting",
		Updated:    time.Now().UTC().Format(time.RFC3339),
	}
	pm.procs[project] = mp
	pm.mu.Unlock()

	go mp.runLoop(pm.cfg)
	return mp
}

// Remove stops and removes a managed process.
func (pm *ProcessManager) Remove(project string) {
	pm.mu.Lock()
	mp, ok := pm.procs[project]
	if ok {
		delete(pm.procs, project)
	}
	pm.mu.Unlock()

	if mp != nil {
		mp.Stop()
	}
}

// List returns all managed processes.
func (pm *ProcessManager) List() map[string]*ManagedProcess {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	result := make(map[string]*ManagedProcess, len(pm.procs))
	for k, v := range pm.procs {
		result[k] = v
	}
	return result
}

// runLoop starts and monitors the process, with auto-restart.
func (mp *ManagedProcess) runLoop(cfg *Config) {
	mp.cancel = make(chan struct{})

	for mp.RestartCount <= cfg.MaxRestarts {
		select {
		case <-mp.cancel:
			mp.setStatus("stopped")
			return
		default:
		}

		script := mp.ProjectDir + "/" + cfg.StartScript
		if _, err := os.Stat(script); os.IsNotExist(err) {
			mp.setError("start.sh not found")
			mp.setStatus("failed")
			return
		}

		log.Printf("[%s] starting on port %d (restart#%d)", mp.Project, mp.Port, mp.RestartCount)

		cmd := exec.Command("/bin/bash", script, "start", fmt.Sprintf("%d", mp.Port))
		cmd.Dir = mp.ProjectDir
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, fmt.Sprintf("PORT=%d", mp.Port))
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			mp.setError(fmt.Sprintf("stdout pipe: %v", err))
			mp.setStatus("failed")
			return
		}
		cmd.Stderr = cmd.Stdout

		if err := cmd.Start(); err != nil {
			mp.setError(fmt.Sprintf("start failed: %v", err))
			mp.setStatus("failed")
			return
		}

		mp.setCmd(cmd)

		// Read logs in background
		done := make(chan struct{})
		go func() {
			defer close(done)
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				mp.appendLog(scanner.Text())
			}
		}()

		// Wait for port readiness
		if waitPort(mp.Port, time.Duration(cfg.PortReadyTimeout)*time.Second) {
			mp.setStatus("running")
			log.Printf("[%s] running (pid=%d)", mp.Project, cmd.Process.Pid)
		} else {
			log.Printf("[%s] port %d not ready in %ds", mp.Project, mp.Port, cfg.PortReadyTimeout)
		}

		// Wait for process to exit
		err = cmd.Wait()
		<-done

		mp.setCmd(nil)

		select {
		case <-mp.cancel:
			mp.setStatus("stopped")
			return
		default:
		}

		mp.RestartCount++
		if err != nil {
			log.Printf("[%s] exited with error: %v", mp.Project, err)
		} else {
			log.Printf("[%s] exited normally", mp.Project)
		}

		if mp.RestartCount > cfg.MaxRestarts {
			mp.setError(fmt.Sprintf("exited %d times, giving up", mp.RestartCount))
			mp.setStatus("failed")
			return
		}

		log.Printf("[%s] restarting in %ds", mp.Project, cfg.RestartDelaySec)
		select {
		case <-mp.cancel:
			mp.setStatus("stopped")
			return
		case <-time.After(time.Duration(cfg.RestartDelaySec) * time.Second):
		}
	}
}

// Stop terminates the managed process.
func (mp *ManagedProcess) Stop() {
	if mp.cancel != nil {
		close(mp.cancel)
	}
	mp.mu.Lock()
	cmd := mp.cmd
	mp.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		// Try start.sh stop first
		stopCmd := exec.Command("/bin/bash", mp.ProjectDir+"/start.sh", "stop")
		stopCmd.Dir = mp.ProjectDir
		stopCmd.Run() // best effort

		// Then SIGTERM the process group
		syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)

		// Wait up to 5 seconds
		done := make(chan struct{})
		go func() {
			cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}

	mp.setStatus("stopped")
	log.Printf("[%s] stopped", mp.Project)
}

// RunCommand executes start.sh with a subcommand (status, log, restart).
func (mp *ManagedProcess) RunCommand(subcmd string) string {
	script := mp.ProjectDir + "/start.sh"
	cmd := exec.Command("/bin/bash", script, subcmd)
	cmd.Dir = mp.ProjectDir
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("PORT=%d", mp.Port))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("error (exit=%v): %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

// Restart stops and restarts the managed process.
func (mp *ManagedProcess) Restart(cfg *Config) {
	mp.Stop()
	mp.mu.Lock()
	mp.RestartCount = 0
	mp.Error = ""
	mp.cancel = nil
	mp.cmd = nil
	mp.mu.Unlock()
	go mp.runLoop(cfg)
}

func (mp *ManagedProcess) setStatus(s string) {
	mp.mu.Lock()
	mp.Status = s
	mp.Updated = time.Now().UTC().Format(time.RFC3339)
	mp.mu.Unlock()
}

func (mp *ManagedProcess) setError(s string) {
	mp.mu.Lock()
	mp.Error = s
	mp.Updated = time.Now().UTC().Format(time.RFC3339)
	mp.mu.Unlock()
}

func (mp *ManagedProcess) setCmd(cmd *exec.Cmd) {
	mp.mu.Lock()
	mp.cmd = cmd
	mp.mu.Unlock()
}

func (mp *ManagedProcess) appendLog(line string) {
	mp.logLock.Lock()
	defer mp.logLock.Unlock()
	mp.LogLines = append(mp.LogLines, line)
	if len(mp.LogLines) > 500 {
		mp.LogLines = mp.LogLines[len(mp.LogLines)-500:]
	}
}

// GetLogTail returns the last n log lines.
func (mp *ManagedProcess) GetLogTail(n int) []string {
	mp.logLock.Lock()
	defer mp.logLock.Unlock()
	if len(mp.LogLines) <= n {
		result := make([]string, len(mp.LogLines))
		copy(result, mp.LogLines)
		return result
	}
	start := len(mp.LogLines) - n
	result := make([]string, n)
	copy(result, mp.LogLines[start:])
	return result
}

// waitPort polls until a TCP connection to the port succeeds or timeout.
func waitPort(port int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}
