package applemusic

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Helper manages the lifecycle of the cliamp-apple-music Swift helper process
// and provides a JSON-line IPC interface for communicating with it.
type Helper struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner

	mu        sync.Mutex
	responses chan Response
	state     Response // latest state event
	stateMu   sync.RWMutex

	// stateCallbacks is called whenever a new state event arrives.
	stateCallbacks []func(Response)
}

// findHelperBinary searches for cliamp-apple-music in standard locations.
func findHelperBinary() (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("apple music provider is only available on macOS")
	}

	// 1. Same directory as the running cliamp binary
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "cliamp-apple-music")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// 2. Config directory
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".config", "cliamp", "bin", "cliamp-apple-music")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// 3. Development: tools build output
	if exe, err := os.Executable(); err == nil {
		// Look relative to the repo for development builds
		repoDir := filepath.Dir(filepath.Dir(exe))
		candidate := filepath.Join(repoDir, "tools", "cliamp-apple-music", ".build", "release", "cliamp-apple-music")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		candidate = filepath.Join(repoDir, "tools", "cliamp-apple-music", ".build", "debug", "cliamp-apple-music")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// 4. PATH
	if path, err := exec.LookPath("cliamp-apple-music"); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("cliamp-apple-music helper binary not found; see docs/apple-music-spec.md for build instructions")
}

// NewHelper starts the Swift helper binary and returns a Helper for IPC.
func NewHelper() (*Helper, error) {
	bin, err := findHelperBinary()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(bin)
	cmd.Stderr = os.Stderr // Forward helper stderr for debugging

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("apple music helper stdin: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("apple music helper stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("apple music helper start: %w", err)
	}

	h := &Helper{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    bufio.NewScanner(stdoutPipe),
		responses: make(chan Response, 64),
	}

	// Start reading stdout in background
	go h.readLoop()

	// Wait for auth response
	select {
	case resp := <-h.responses:
		if resp.Type == "error" {
			cmd.Process.Kill()
			return nil, fmt.Errorf("apple music auth failed: %s", resp.Message)
		}
		if resp.Type == "auth" && resp.Status == "authorized" {
			// Good to go
		}
	case <-time.After(30 * time.Second):
		cmd.Process.Kill()
		return nil, fmt.Errorf("apple music helper timed out waiting for auth")
	}

	return h, nil
}

// readLoop reads JSON lines from the helper and dispatches them.
func (h *Helper) readLoop() {
	for h.stdout.Scan() {
		line := h.stdout.Bytes()
		var resp Response
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}

		switch resp.Type {
		case "state":
			h.stateMu.Lock()
			h.state = resp
			h.stateMu.Unlock()
			for _, cb := range h.stateCallbacks {
				cb(resp)
			}
		default:
			// Send to responses channel for synchronous waiters
			select {
			case h.responses <- resp:
			default:
				// Channel full, drop oldest
				select {
				case <-h.responses:
				default:
				}
				h.responses <- resp
			}
		}
	}
}

// Send sends a command to the helper and waits for a response.
func (h *Helper) Send(cmd Command) (Response, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	data, err := json.Marshal(cmd)
	if err != nil {
		return Response{}, fmt.Errorf("marshal command: %w", err)
	}
	data = append(data, '\n')

	if _, err := h.stdin.Write(data); err != nil {
		return Response{}, fmt.Errorf("write to helper: %w", err)
	}

	// Wait for response (with timeout)
	select {
	case resp := <-h.responses:
		return resp, nil
	case <-time.After(30 * time.Second):
		return Response{}, fmt.Errorf("helper response timeout")
	}
}

// SendAsync sends a command without waiting for a response (fire-and-forget).
func (h *Helper) SendAsync(cmd Command) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}
	data = append(data, '\n')

	_, err = h.stdin.Write(data)
	return err
}

// State returns the latest playback state from the helper.
func (h *Helper) State() Response {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	return h.state
}

// OnStateChange registers a callback for state change events.
func (h *Helper) OnStateChange(cb func(Response)) {
	h.stateCallbacks = append(h.stateCallbacks, cb)
}

// Close stops the helper process.
func (h *Helper) Close() error {
	h.stdin.Close()
	if h.cmd.Process != nil {
		h.cmd.Process.Kill()
	}
	return h.cmd.Wait()
}
