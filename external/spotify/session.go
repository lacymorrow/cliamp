package spotify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	librespot "github.com/devgianlu/go-librespot"
	librespotPlayer "github.com/devgianlu/go-librespot/player"
	"github.com/devgianlu/go-librespot/session"
	devicespb "github.com/devgianlu/go-librespot/proto/spotify/connectstate/devices"
)

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

	fmt.Println("Spotify: Starting OAuth2 authentication...")
	fmt.Println("A browser window should open. If not, check the terminal output for a URL.")

	sess, err := session.NewSessionFromOptions(ctx, &session.Options{
		Log:        &librespot.NullLogger{},
		DeviceType: devicespb.DeviceType_COMPUTER,
		DeviceId:   devID,
		Credentials: session.InteractiveCredentials{
			CallbackPort: 0, // auto-pick port
		},
	})
	if err != nil {
		return nil, fmt.Errorf("spotify: interactive auth: %w", err)
	}

	fmt.Printf("Spotify: Authenticated as %s\n", sess.Username())

	// Persist credentials for future sessions.
	_ = saveCreds(&storedCreds{
		Username: sess.Username(),
		Data:     sess.StoredCredentials(),
		DeviceID: devID,
	})

	s := &Session{sess: sess, devID: devID}
	if err := s.initPlayer(); err != nil {
		sess.Close()
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
