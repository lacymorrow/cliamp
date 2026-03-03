package spotify

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	librespot "github.com/devgianlu/go-librespot"
	librespotPlayer "github.com/devgianlu/go-librespot/player"
	"github.com/devgianlu/go-librespot/session"
	devicespb "github.com/devgianlu/go-librespot/proto/spotify/connectstate/devices"
)

// authLogger captures the OAuth2 URL from go-librespot's log output
// so we can display it to the user and offer to open a browser.
type authLogger struct {
	librespot.NullLogger
	authURL string
}

func (l *authLogger) Infof(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if strings.Contains(msg, "to complete authentication visit") {
		// Extract URL — it's the last argument
		if len(args) > 0 {
			if u, ok := args[len(args)-1].(string); ok {
				l.authURL = u
			}
		}
	}
}

func (l *authLogger) Info(args ...interface{})                       {}
func (l *authLogger) WithField(string, interface{}) librespot.Logger { return l }
func (l *authLogger) WithError(error) librespot.Logger               { return l }

// openBrowser tries to open a URL in the user's default browser.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return fmt.Errorf("unsupported platform")
	}
}

// storedCreds holds persisted Spotify credentials for re-authentication.
type storedCreds struct {
	Username string `json:"username"`
	Data     []byte `json:"data"`
	DeviceID string `json:"device_id"`
}

// Session manages a go-librespot session and player for Spotify integration.
type Session struct {
	mu     sync.Mutex
	sess   *session.Session
	player *librespotPlayer.Player
	devID  string
}

// NewSession creates a go-librespot session, using stored credentials if
// available, otherwise starting an interactive OAuth2 flow.
func NewSession(ctx context.Context) (*Session, error) {
	creds, err := loadCreds()
	if err == nil && creds.Username != "" && len(creds.Data) > 0 {
		s, err := newSessionFromStored(ctx, creds)
		if err == nil {
			return s, nil
		}
		// Stored credentials failed (expired/revoked), fall through to interactive.
		fmt.Fprintf(os.Stderr, "spotify: stored credentials failed, re-authenticating: %v\n", err)
	}
	return newInteractiveSession(ctx)
}

func newSessionFromStored(ctx context.Context, creds *storedCreds) (*Session, error) {
	devID := creds.DeviceID
	if devID == "" {
		devID = generateDeviceID()
	}

	sess, err := session.NewSessionFromOptions(ctx, &session.Options{
		Log:        &librespot.NullLogger{},
		DeviceType: devicespb.DeviceType_COMPUTER,
		DeviceId:   devID,
		Credentials: session.StoredCredentials{
			Username: creds.Username,
			Data:     creds.Data,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("spotify: stored auth: %w", err)
	}

	s := &Session{sess: sess, devID: devID}

	// Re-save credentials (they may have been refreshed).
	_ = saveCreds(&storedCreds{
		Username: sess.Username(),
		Data:     sess.StoredCredentials(),
		DeviceID: devID,
	})

	if err := s.initPlayer(); err != nil {
		sess.Close()
		return nil, err
	}
	return s, nil
}

func newInteractiveSession(ctx context.Context) (*Session, error) {
	devID := generateDeviceID()
	logger := &authLogger{}

	fmt.Println("Spotify: Starting OAuth2 authentication...")

	// Start session creation in a goroutine so we can capture the auth URL
	// from the logger and prompt the user while it waits for the callback.
	type sessionResult struct {
		sess *session.Session
		err  error
	}
	resultCh := make(chan sessionResult, 1)

	go func() {
		sess, err := session.NewSessionFromOptions(ctx, &session.Options{
			Log:        logger,
			DeviceType: devicespb.DeviceType_COMPUTER,
			DeviceId:   devID,
			Credentials: session.InteractiveCredentials{
				CallbackPort: 0, // auto-pick port
			},
		})
		resultCh <- sessionResult{sess, err}
	}()

	// Poll for the auth URL to appear (the logger captures it).
	// Once we have it, print it and try to open a browser.
	urlPrinted := false
	for !urlPrinted {
		select {
		case res := <-resultCh:
			// Session completed before we even printed URL (unlikely but handle it).
			if res.err != nil {
				return nil, fmt.Errorf("spotify: interactive auth: %w", res.err)
			}
			fmt.Printf("Spotify: Authenticated as %s\n", res.sess.Username())
			_ = saveCreds(&storedCreds{
				Username: res.sess.Username(),
				Data:     res.sess.StoredCredentials(),
				DeviceID: devID,
			})
			s := &Session{sess: res.sess, devID: devID}
			if err := s.initPlayer(); err != nil {
				res.sess.Close()
				return nil, err
			}
			return s, nil
		default:
			if logger.authURL != "" {
				urlPrinted = true
			}
		}
	}

	// We have the URL. Try opening browser, show URL, offer retry.
	fmt.Println()
	fmt.Printf("  Open this URL to authenticate with Spotify:\n\n")
	fmt.Printf("  %s\n\n", logger.authURL)

	if err := openBrowser(logger.authURL); err == nil {
		fmt.Println("  (Attempting to open in your browser...)")
	} else {
		fmt.Println("  (Could not open browser automatically.)")
	}
	fmt.Println("  Press Enter to retry opening the browser, or just complete auth in your browser.")
	fmt.Println("  (You can close the browser tab after authentication completes.)")
	fmt.Println("  Waiting for authentication callback...")

	// Start a goroutine to handle Enter presses for browser retry.
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			if logger.authURL != "" {
				_ = openBrowser(logger.authURL)
				fmt.Println("  (Retrying browser open...)")
			}
		}
	}()

	// Wait for session result.
	res := <-resultCh
	if res.err != nil {
		return nil, fmt.Errorf("spotify: interactive auth: %w", res.err)
	}

	fmt.Printf("\nSpotify: Authenticated as %s\n", res.sess.Username())

	// Persist credentials for future sessions.
	_ = saveCreds(&storedCreds{
		Username: res.sess.Username(),
		Data:     res.sess.StoredCredentials(),
		DeviceID: devID,
	})

	s := &Session{sess: res.sess, devID: devID}
	if err := s.initPlayer(); err != nil {
		res.sess.Close()
		return nil, err
	}
	return s, nil
}

// initPlayer creates the go-librespot player. We only use NewStream() for
// decoded AudioSources — audio output is routed through cliamp's Beep pipeline,
// not go-librespot's output backend.
func (s *Session) initPlayer() error {
	p, err := librespotPlayer.NewPlayer(&librespotPlayer.Options{
		Spclient:             s.sess.Spclient(),
		AudioKey:             s.sess.AudioKey(),
		Events:               s.sess.Events(),
		Log:                  &librespot.NullLogger{},
		NormalisationEnabled: true,
		AudioBackend:         "pipe",
		AudioOutputPipe:      os.DevNull,
	})
	if err != nil {
		return fmt.Errorf("spotify: player init: %w", err)
	}
	s.player = p
	return nil
}

// NewStream creates a decoded audio stream for the given Spotify track ID.
// Returns the Stream containing an AudioSource (float32 PCM at 44100Hz stereo).
func (s *Session) NewStream(ctx context.Context, spotID librespot.SpotifyId, bitrate int) (*librespotPlayer.Stream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.player.NewStream(ctx, http.DefaultClient, spotID, bitrate, 0)
}

// WebApi calls the Spotify Web API via the session, guarded by the session mutex.
func (s *Session) WebApi(ctx context.Context, method, path string, query url.Values) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sess.WebApi(ctx, method, path, query, nil, nil)
}

// Close releases all session and player resources.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.player != nil {
		s.player.Close()
	}
	if s.sess != nil {
		s.sess.Close()
	}
}

// configDir returns ~/.config/cliamp/.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "cliamp"), nil
}

func credsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "spotify_credentials.json"), nil
}

func generateDeviceID() string {
	b := make([]byte, 20) // 40 hex chars
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func loadCreds() (*storedCreds, error) {
	path, err := credsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var creds storedCreds
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

func saveCreds(creds *storedCreds) error {
	path, err := credsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
