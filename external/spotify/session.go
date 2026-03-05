package spotify

import (
	"context"
	"crypto/rand"
	"errors"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	Username     string `json:"username"`
	Data         []byte `json:"data"`
	DeviceID     string `json:"device_id"`
	RefreshToken string `json:"refresh_token,omitempty"` // OAuth2 refresh token for silent re-auth
}

// CallbackPort is the fixed port for the OAuth2 callback server.
// Must match the redirect URI registered in the Spotify Developer app.
const CallbackPort = 19872

// Session manages a go-librespot session and player for Spotify integration.
type Session struct {
	mu          sync.Mutex
	sess        *session.Session
	player      *librespotPlayer.Player
	devID       string
	clientID    string         // Spotify Developer app client ID
	tokenSource oauth2.TokenSource // auto-refreshing OAuth2 token source
}

// NewSession creates a go-librespot session, using stored credentials if
// available, otherwise starting an interactive OAuth2 flow.
// clientID is the Spotify Developer app client ID for Web API access.
func NewSession(ctx context.Context, clientID string) (*Session, error) {
	creds, err := loadCreds()
	if err == nil && creds.Username != "" && len(creds.Data) > 0 {
		s, err := newSessionFromStored(ctx, clientID, creds)
		if err == nil {
			return s, nil
		}
		// Stored credentials failed (expired/revoked), fall through to interactive.
		fmt.Fprintf(os.Stderr, "spotify: stored credentials failed, re-authenticating: %v\n", err)
	}
	return newInteractiveSession(ctx, clientID)
}

func newSessionFromStored(ctx context.Context, clientID string, creds *storedCreds) (*Session, error) {
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

	// For stored credentials, we need a fresh Web API token via OAuth2.
	// The spclient's login5 token is NOT suitable for Web API calls.
	// Try silent refresh first (no browser), fall back to interactive.
	var oauthToken *oauth2.Token
	if creds.RefreshToken != "" {
		token, err := silentTokenRefresh(clientID, creds.RefreshToken)
		if err == nil {
			oauthToken = token
			fmt.Fprintf(os.Stderr, "spotify: Web API token refreshed silently\n")
		} else {
			fmt.Fprintf(os.Stderr, "spotify: silent refresh failed, launching browser: %v\n", err)
		}
	}
	if oauthToken == nil {
		token, err := doWebAPIAuth(clientID)
		if err != nil {
			sess.Close()
			return nil, fmt.Errorf("stored session needs fresh Web API token: %w", err)
		}
		oauthToken = token
	}

	// Create an auto-refreshing token source — handles expiry transparently.
	conf := spotifyOAuthConfig(clientID)
	ts := conf.TokenSource(context.Background(), oauthToken)

	s := &Session{sess: sess, devID: devID, clientID: clientID, tokenSource: ts}

	// Re-save credentials (including refresh token for next launch).
	if err := saveCreds(&storedCreds{
		Username:     sess.Username(),
		Data:         sess.StoredCredentials(),
		DeviceID:     devID,
		RefreshToken: oauthToken.RefreshToken,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "spotify: failed to save credentials: %v\n", err)
	}

	if err := s.initPlayer(); err != nil {
		sess.Close()
		return nil, err
	}
	return s, nil
}

// oauthScopes are the Spotify Web API scopes needed for cliamp.
// See: https://developer.spotify.com/documentation/web-api/concepts/scopes
//
// NOTE: The following internal Spotify scopes are NOT available to third-party
// apps and cause "Illegal scope" errors:
//   app-remote-control, playlist-modify, playlist-read, user-modify,
//   user-modify-private, user-personalized, user-read-birthdate
var oauthScopes = []string{
	// Playlist browsing
	"playlist-read-collaborative",
	"playlist-read-private",
	// Playlist modification (save queue, create playlists)
	"playlist-modify-public",
	"playlist-modify-private",
	// Streaming audio
	"streaming",
	// Library (liked songs, saved albums)
	"user-library-read",
	"user-library-modify",
	// User profile
	"user-read-private",
	"user-read-email",
	// Playback state (current track, queue)
	"user-read-playback-state",
	"user-modify-playback-state",
	"user-read-currently-playing",
	// Recently played / top tracks
	"user-read-recently-played",
	"user-top-read",
	// Following (artists, users)
	"user-follow-read",
	"user-follow-modify",
}

// spotifyOAuthConfig returns the OAuth2 config for the given client ID.
func spotifyOAuthConfig(clientID string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:    clientID,
		RedirectURL: fmt.Sprintf("http://127.0.0.1:%d/login", CallbackPort),
		Scopes:      oauthScopes,
		Endpoint:    spotifyoauth2.Endpoint,
	}
}

// silentTokenRefresh uses a stored refresh token to get a new access token
// without opening a browser.
func silentTokenRefresh(clientID, refreshToken string) (*oauth2.Token, error) {
	conf := spotifyOAuthConfig(clientID)
	src := conf.TokenSource(context.Background(), &oauth2.Token{RefreshToken: refreshToken})
	return src.Token()
}

// doWebAPIAuth performs an OAuth2 PKCE flow to get a fresh Web API access token.
// Opens a browser for user consent, returns the full token (including refresh token).
func doWebAPIAuth(clientID string) (*oauth2.Token, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", CallbackPort))
	if err != nil {
		return nil, fmt.Errorf("listen on port %d: %w", CallbackPort, err)
	}

	oauthConf := spotifyOAuthConfig(clientID)

	verifier := oauth2.GenerateVerifier()
	authURL := oauthConf.AuthCodeURL("", oauth2.S256ChallengeOption(verifier))

	codeCh := make(chan string, 1)
	go func() {
		if err := http.Serve(lis, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			code := r.URL.Query().Get("code")
			if code != "" {
				codeCh <- code
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>cliamp</title></head>
<body style="font-family:system-ui;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#1a1a2e;color:#e0e0e0">
<div style="text-align:center">
<h2>✅ Authenticated!</h2>
<p>You can close this tab now.</p>
<script>setTimeout(function(){window.close()},1500)</script>
</div></body></html>`))
		})); err != nil && !errors.Is(err, net.ErrClosed) {
			fmt.Fprintf(os.Stderr, "spotify: auth callback server error: %v\n", err)
		}
	}()

	fmt.Println("\nSpotify: Refreshing Web API token...")
	fmt.Printf("  %s\n", authURL)
	_ = openBrowser(authURL)
	fmt.Println("  Waiting for authentication callback...")

	code := <-codeCh
	_ = lis.Close()

	token, err := oauthConf.Exchange(context.Background(), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}

	fmt.Println("Spotify: Web API token refreshed.")
	return token, nil
}

func newInteractiveSession(ctx context.Context, clientID string) (*Session, error) {
	devID := generateDeviceID()

	fmt.Println("Spotify: Starting OAuth2 authentication...")

	// We do our own OAuth2 flow so we can:
	// 1. Capture the access token for Web API calls
	// 2. Serve auto-close HTML in the callback
	// 3. Pass the token to go-librespot via SpotifyTokenCredentials

	// Start our callback server on a fixed port (must match Spotify Developer app redirect URI).
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", CallbackPort))
	if err != nil {
		return nil, fmt.Errorf("spotify: listen on port %d (is another instance running?): %w", CallbackPort, err)
	}

	oauthConf := &oauth2.Config{
		ClientID:    clientID,
		RedirectURL: fmt.Sprintf("http://127.0.0.1:%d/login", CallbackPort),
		Scopes:      oauthScopes,
		Endpoint:    spotifyoauth2.Endpoint,
	}

	verifier := oauth2.GenerateVerifier()
	authURL := oauthConf.AuthCodeURL("", oauth2.S256ChallengeOption(verifier))

	// Serve the callback — return HTML that auto-closes the tab.
	codeCh := make(chan string, 1)
	go func() {
		if err := http.Serve(lis, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			code := r.URL.Query().Get("code")
			if code != "" {
				codeCh <- code
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>cliamp</title></head>
<body style="font-family:system-ui;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#1a1a2e;color:#e0e0e0">
<div style="text-align:center">
<h2>✅ Authenticated!</h2>
<p>You can close this tab now.</p>
<script>setTimeout(function(){window.close()},1500)</script>
</div></body></html>`))
		})); err != nil && !errors.Is(err, net.ErrClosed) {
			fmt.Fprintf(os.Stderr, "spotify: auth callback server error: %v\n", err)
		}
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

	// Handle Enter for retry. We read in a goroutine but DON'T use
	// bufio.Scanner — it holds an internal buffer that steals bytes from
	// stdin after auth completes, breaking Bubbletea's raw terminal input.
	// The goroutine will block on Read after auth, but that's fine — it
	// will exit when the process exits or stdin is closed.
	authDone := make(chan struct{})
	go func() {
		buf := make([]byte, 1)
		for {
			select {
			case <-authDone:
				return
			default:
			}
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			if n > 0 && buf[0] == '\n' {
				select {
				case <-authDone:
					return
				default:
					_ = openBrowser(authURL)
					fmt.Println("  (Retrying browser open...)")
				}
			}
		}
	}()

	// Wait for the auth code.
	code := <-codeCh
	close(authDone) // stop the retry goroutine
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

	// Persist stored credentials + refresh token for future sessions.
	if err := saveCreds(&storedCreds{
		Username:     sess.Username(),
		Data:         sess.StoredCredentials(),
		DeviceID:     devID,
		RefreshToken: token.RefreshToken,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "spotify: failed to save credentials: %v\n", err)
	}

	// Create an auto-refreshing token source for Web API calls.
	conf := spotifyOAuthConfig(clientID)
	ts := conf.TokenSource(context.Background(), token)

	s := &Session{sess: sess, devID: devID, clientID: clientID, tokenSource: ts}
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
	// Fetch user's country for media restriction checks.
	countryCode := "US" // fallback
	if resp, err := s.WebApi(context.Background(), "GET", "/v1/me", nil); err != nil {
		fmt.Fprintf(os.Stderr, "spotify: failed to get user profile for country: %v\n", err)
	} else {
		defer resp.Body.Close()
		var me struct {
			Country string `json:"country"`
		}
		if data, err := io.ReadAll(resp.Body); err != nil {
			fmt.Fprintf(os.Stderr, "spotify: failed to read user profile: %v\n", err)
		} else if err := json.Unmarshal(data, &me); err != nil {
			fmt.Fprintf(os.Stderr, "spotify: failed to parse user profile: %v\n", err)
		} else if me.Country != "" {
			countryCode = me.Country
		}
	}
	p, err := librespotPlayer.NewPlayer(&librespotPlayer.Options{
		Spclient:             s.sess.Spclient(),
		AudioKey:             s.sess.AudioKey(),
		Events:               s.sess.Events(),
		Log:                  &librespot.NullLogger{},
		CountryCode:          &countryCode,
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
	ts := s.tokenSource
	s.mu.Unlock()

	var token string
	if ts != nil {
		tok, err := ts.Token()
		if err != nil {
			return nil, fmt.Errorf("refresh access token: %w", err)
		}
		token = tok.AccessToken
	} else {
		// Fall back to spclient token if no OAuth2 token source.
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

// Reconnect tears down the current session, clears stored credentials, and
// re-authenticates interactively. This is called automatically when playback
// encounters an auth-related error (e.g. AES key retrieval failure) so the
// user doesn't get stuck in an error loop.
//
// The new session is established before tearing down the old one to avoid a
// window where s.sess/s.player are nil (which would crash concurrent callers
// like NewStream or WebApi).
func (s *Session) Reconnect(ctx context.Context) error {
	// Capture clientID without holding the lock during the (potentially long)
	// interactive OAuth2 flow.
	s.mu.Lock()
	clientID := s.clientID
	s.mu.Unlock()

	// Clear stored credentials so we don't reuse stale ones.
	if err := deleteCreds(); err != nil {
		fmt.Fprintf(os.Stderr, "spotify: failed to clear stored credentials: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "spotify: session expired, re-authenticating...\n")

	// Create the new session outside the lock — this may open a browser and
	// block for user interaction.
	newSess, err := NewSession(ctx, clientID)
	if err != nil {
		return fmt.Errorf("spotify: reconnect: %w", err)
	}

	// Now acquire the lock and atomically swap internals.
	s.mu.Lock()
	oldPlayer := s.player
	oldSess := s.sess
	s.sess = newSess.sess
	s.player = newSess.player
	s.devID = newSess.devID
	s.tokenSource = newSess.tokenSource
	s.mu.Unlock()

	// Tear down old session/player after the swap so there's no nil window.
	if oldPlayer != nil {
		oldPlayer.Close()
	}
	if oldSess != nil {
		oldSess.Close()
	}

	// Prevent newSess.Close() from tearing down the resources we just adopted.
	newSess.mu.Lock()
	newSess.sess = nil
	newSess.player = nil
	newSess.mu.Unlock()

	fmt.Fprintf(os.Stderr, "spotify: re-authenticated successfully\n")
	return nil
}

// deleteCreds removes the stored credentials file.
func deleteCreds() error {
	path, err := credsPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
