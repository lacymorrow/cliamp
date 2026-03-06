// Package telemetry sends an anonymous monthly ping so we can count
// active users (MAU). No personal data is collected — just an opaque
// UUID and the app version.
package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const endpoint = "https://telemetry.cliamp.stream/ping"

type state struct {
	ID        string `json:"id"`
	LastMonth string `json:"last_month"` // "2006-01"
}

func stateFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "cliamp", ".telemetry_id"), nil
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 2
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func load() (state, string, error) {
	path, err := stateFile()
	if err != nil {
		return state{}, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return state{}, path, err
	}
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		return state{}, path, err
	}
	return s, path, nil
}

func save(path string, s state) {
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, data, 0o644)
}

// Ping sends an anonymous telemetry ping if one hasn't been sent this
// month. It is fire-and-forget: errors are silently ignored so they
// never affect the user experience.
func Ping(version string) {
	s, path, err := load()
	if err != nil {
		// First run or corrupt file — generate a new ID.
		p, perr := stateFile()
		if perr != nil {
			return
		}
		path = p
		s = state{ID: newUUID()}
	}
	if s.ID == "" {
		s.ID = newUUID()
	}

	thisMonth := time.Now().UTC().Format("2006-01")
	if s.LastMonth == thisMonth {
		return // already pinged this month
	}

	// Update state before sending so we don't retry on every launch
	// if the server is temporarily down.
	s.LastMonth = thisMonth
	save(path, s)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?id="+s.ID+"&v="+version, nil)
		if err != nil {
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		resp.Body.Close()
	}()
}
