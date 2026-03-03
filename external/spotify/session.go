package spotify

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	librespot "github.com/devgianlu/go-librespot"
	librespotPlayer "github.com/devgianlu/go-librespot/player"
	"github.com/devgianlu/go-librespot/session"
	devicespb "github.com/devgianlu/go-librespot/proto/spotify/connectstate/devices"
	"golang.org/x/oauth2"
	spotifyoauth2 "golang.org/x/oauth2/spotify"
)

// storedCreds holds persisted Spotify credentials for re-authentication.
type storedCreds struct {
	Username string `json:"username"`
	Data     []byte `json:"data"`
	DeviceID string `json:"device_id"`
}

// Session manages a go-librespot session and player for Spotify integration.
type Session struct {
	mu         sync.Mutex
	sess       *session.Session
	player     *librespotPlayer.Player
	devID      string
	webAPIToken string // OAuth2 access token for Spotify Web API calls
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

	// For stored credentials, we need a fresh Web API token.
	// Get it from the spclient (this is the login5 token — not ideal but works
	// for stored credential sessions since we can't re-do OAuth2).
	webToken, _ := sess.Spclient().GetAccessToken(ctx, false)

	s := &Session{sess: sess, devID: devID, webAPIToken: webToken}

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

// oauthScopes are the same scopes go-librespot uses for its interactive auth.
var oauthScopes = []string{
	"app-remote-control",
	"playlist-modify",
	"playlist-modify-private",
	"playlist-modify-public",
	"playlist-read",
	"playlist-read-collaborative",
	"playlist-read-private",
	"streaming",
	"ugc-image-upload",
	"user-follow-modify",
	"user-follow-read",
	"user-library-modify",
	"user-library-read",
	"user-modify",
	"user-modify-playback-state",
	"user-modify-private",
	"user-personalized",
	"user-read-birthdate",
	"user-read-currently-playing",
	"user-read-email",
	"user-read-play-history",
	"user-read-playback-position",
	"user-read-playback-state",
	"user-read-private",
	"user-read-recently-played",
	"user-top-read",
}

func newInteractiveSession(ctx context.Context) (*Session, error) {
	devID := generateDeviceID()

	fmt.Println("Spotify: Starting OAuth2 authentication...")

	// We do our own OAuth2 flow so we can:
	// 1. Capture the access token for Web API calls
	// 2. Serve auto-close HTML in the callback
	// 3. Pass the token to go-librespot via SpotifyTokenCredentials

	// Start our callback server.
	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		return nil, fmt.Errorf("spotify: listen: %w", err)
	}
	callbackPort := lis.Addr().(*net.TCPAddr).Port

	oauthConf := &oauth2.Config{
		ClientID:    librespot.ClientIdHex,
		RedirectURL: fmt.Sprintf("http://127.0.0.1:%d/login", callbackPort),
		Scopes:      oauthScopes,
		Endpoint:    spotifyoauth2.Endpoint,
	}

	verifier := oauth2.GenerateVerifier()
	authURL := oauthConf.AuthCodeURL("", oauth2.S256ChallengeOption(verifier))

	// Serve the callback — return HTML that auto-closes the tab.
	codeCh := make(chan string, 1)
	go func() {
		_ = http.Serve(lis, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			code := r.URL.Query().Get("code")
			if code != "" {
				codeCh <- code
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head><title>cliamp</title></head>
<body style="font-family:system-ui;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#1a1a2e;color:#e0e0e0">
<div style="text-align:center">
<h2>✅ Authenticated!</h2>
<p>You can close this tab now.</p>
<script>setTimeout(function(){window.close()},1500)</script>
</div></body></html>`))
		}))
	}()

	// Show URL and open browser.
	fmt.Println()
	fmt.Printf("  Open this URL to authenticate with Spotify:\n\n")
	fmt.Printf("  %s\n\n", authURL)

	if err := openBrowser(authURL); err == nil {
		fmt.Println("  (Attempting to open in your browser...)")
	} else {
		fmt.Println("  (Could not open browser automatically.)")
	}
	fmt.Println("  Press Enter to retry opening the browser.")
	fmt.Println("  Waiting for authentication callback...")

	// Handle Enter for retry.
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			_ = openBrowser(authURL)
			fmt.Println("  (Retrying browser open...)")
		}
	}()

	// Wait for the auth code.
	code := <-codeCh
	_ = lis.Close()

	// Exchange code for token.
	token, err := oauthConf.Exchange(context.Background(), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("spotify: token exchange: %w", err)
	}

	username, _ := token.Extra("username").(string)
	accessToken := token.AccessToken

	fmt.Printf("\nSpotify: Got OAuth2 token, connecting session...\n")

	// Create go-librespot session using the OAuth2 token.
	sess, err := session.NewSessionFromOptions(ctx, &session.Options{
		Log:        &librespot.NullLogger{},
		DeviceType: devicespb.DeviceType_COMPUTER,
		DeviceId:   devID,
		Credentials: session.SpotifyTokenCredentials{
			Username: username,
			Token:    accessToken,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("spotify: session from token: %w", err)
	}

	fmt.Printf("Spotify: Authenticated as %s\n", sess.Username())

	// Persist stored credentials for future sessions.
	_ = saveCreds(&storedCreds{
		Username: sess.Username(),
		Data:     sess.StoredCredentials(),
		DeviceID: devID,
	})

	s := &Session{sess: sess, devID: devID, webAPIToken: accessToken}
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
func (s *Session) NewStream(ctx context.Context, spotID librespot.SpotifyId, bitrate int) (*librespotPlayer.Stream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.player.NewStream(ctx, http.DefaultClient, spotID, bitrate, 0)
}

// WebApi calls the Spotify Web API using the OAuth2 access token.
// This is the standard Web API token (not go-librespot's internal spclient token),
// which has proper rate limits for api.spotify.com endpoints.
func (s *Session) WebApi(ctx context.Context, method, path string, query url.Values) (*http.Response, error) {
	s.mu.Lock()
	token := s.webAPIToken
	s.mu.Unlock()

	// If no OAuth2 token available, fall back to spclient token.
	if token == "" {
		s.mu.Lock()
		var err error
		token, err = s.sess.Spclient().GetAccessToken(ctx, false)
		s.mu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("get access token: %w", err)
		}
	}

	u, _ := url.Parse("https://api.spotify.com")
	u = u.JoinPath(path)
	if query != nil {
		u.RawQuery = query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	return http.DefaultClient.Do(req)
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

// openBrowser tries to open a URL in the user's default browser.
func openBrowser(u string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "linux":
		return exec.Command("xdg-open", u).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	default:
		return fmt.Errorf("unsupported platform")
	}
}

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
	b := make([]byte, 20)
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
