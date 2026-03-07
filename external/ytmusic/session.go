package ytmusic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

// storedCreds holds persisted YouTube Music credentials for re-authentication.
type storedCreds struct {
	RefreshToken string `json:"refresh_token"`
}

// CallbackPort is the fixed port for the OAuth2 callback server.
// Must match the redirect URI registered in the Google Cloud console.
const CallbackPort = 19873

// Session manages a YouTube Data API v3 service for YouTube Music integration.
type Session struct {
	mu           sync.Mutex
	clientID     string
	clientSecret string
	service      *youtube.Service
	tokenSource  oauth2.TokenSource
}

// oauthScopes are the YouTube API scopes needed for cliamp.
var oauthScopes = []string{
	"https://www.googleapis.com/auth/youtube.readonly",
}

// googleOAuthConfig returns the OAuth2 config for the given client ID and secret.
// Google Desktop OAuth requires both a client_id and client_secret (unlike Spotify
// which supports PKCE-only public clients).
func googleOAuthConfig(clientID, clientSecret string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  fmt.Sprintf("http://127.0.0.1:%d/callback", CallbackPort),
		Scopes:       oauthScopes,
		Endpoint:     google.Endpoint,
	}
}

// NewSession creates a YouTube API session, using stored credentials if
// available, otherwise starting an interactive OAuth2 flow.
func NewSession(ctx context.Context, clientID, clientSecret string) (*Session, error) {
	creds, err := loadCreds()
	if err == nil && creds.RefreshToken != "" {
		s, err := newSessionFromStored(ctx, clientID, clientSecret, creds)
		if err == nil {
			return s, nil
		}
		// Stored credentials failed, fall through to interactive.
	}
	return newInteractiveSession(ctx, clientID, clientSecret)
}

// NewSessionSilent is like NewSession but only uses stored credentials.
// Returns an error if interactive auth is required.
func NewSessionSilent(ctx context.Context, clientID, clientSecret string) (*Session, error) {
	creds, err := loadCreds()
	if err != nil || creds.RefreshToken == "" {
		return nil, fmt.Errorf("no stored credentials")
	}
	return newSessionFromStored(ctx, clientID, clientSecret, creds)
}

// newSessionFromStored creates a session from stored credentials via silent refresh.
func newSessionFromStored(ctx context.Context, clientID, clientSecret string, creds *storedCreds) (*Session, error) {
	token, err := silentTokenRefresh(clientID, clientSecret, creds.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("ytmusic: silent refresh: %w", err)
	}

	conf := googleOAuthConfig(clientID, clientSecret)
	ts := conf.TokenSource(ctx, token)

	svc, err := youtube.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("ytmusic: create service: %w", err)
	}

	// Re-save credentials (refresh token may have been rotated).
	if token.RefreshToken != "" {
		if err := saveCreds(&storedCreds{RefreshToken: token.RefreshToken}); err != nil {
			fmt.Fprintf(os.Stderr, "ytmusic: failed to save credentials: %v\n", err)
		}
	}

	return &Session{
		clientID:     clientID,
		clientSecret: clientSecret,
		service:      svc,
		tokenSource:  ts,
	}, nil
}

// silentTokenRefresh uses a stored refresh token to get a new access token
// without opening a browser.
func silentTokenRefresh(clientID, clientSecret, refreshToken string) (*oauth2.Token, error) {
	conf := googleOAuthConfig(clientID, clientSecret)
	src := conf.TokenSource(context.Background(), &oauth2.Token{RefreshToken: refreshToken})
	return src.Token()
}

// newInteractiveSession performs an OAuth2 flow to authenticate.
func newInteractiveSession(ctx context.Context, clientID, clientSecret string) (*Session, error) {
	token, err := doOAuth(clientID, clientSecret)
	if err != nil {
		return nil, err
	}

	conf := googleOAuthConfig(clientID, clientSecret)
	ts := conf.TokenSource(ctx, token)

	svc, err := youtube.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("ytmusic: create service: %w", err)
	}

	// Persist refresh token for future sessions.
	if err := saveCreds(&storedCreds{RefreshToken: token.RefreshToken}); err != nil {
		fmt.Fprintf(os.Stderr, "ytmusic: failed to save credentials: %v\n", err)
	}

	return &Session{
		clientID:     clientID,
		clientSecret: clientSecret,
		service:      svc,
		tokenSource:  ts,
	}, nil
}

// doOAuth performs an OAuth2 flow: starts localhost server, opens browser,
// exchanges code for token.
func doOAuth(clientID, clientSecret string) (*oauth2.Token, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", CallbackPort))
	if err != nil {
		return nil, fmt.Errorf("ytmusic: listen on port %d (is another instance running?): %w", CallbackPort, err)
	}

	oauthConf := googleOAuthConfig(clientID, clientSecret)

	verifier := oauth2.GenerateVerifier()
	authURL := oauthConf.AuthCodeURL("", oauth2.S256ChallengeOption(verifier), oauth2.AccessTypeOffline)

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
<h2>Authenticated!</h2>
<p>You can close this tab now.</p>
<script>setTimeout(function(){window.close()},1500)</script>
</div></body></html>`))
		})); err != nil && !errors.Is(err, net.ErrClosed) {
			fmt.Fprintf(os.Stderr, "ytmusic: auth callback server error: %v\n", err)
		}
	}()

	_ = openBrowser(authURL)

	code := <-codeCh
	_ = lis.Close()

	token, err := oauthConf.Exchange(context.Background(), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("ytmusic: token exchange: %w", err)
	}

	fmt.Println("YouTube Music: authenticated.")
	return token, nil
}

// Reconnect tears down the current session, clears stored credentials, and
// re-authenticates interactively.
func (s *Session) Reconnect(ctx context.Context) error {
	s.mu.Lock()
	clientID := s.clientID
	clientSecret := s.clientSecret
	s.mu.Unlock()

	if err := deleteCreds(); err != nil {
		fmt.Fprintf(os.Stderr, "ytmusic: failed to clear stored credentials: %v\n", err)
	}

	newSess, err := NewSession(ctx, clientID, clientSecret)
	if err != nil {
		return fmt.Errorf("ytmusic: reconnect: %w", err)
	}

	s.mu.Lock()
	s.service = newSess.service
	s.tokenSource = newSess.tokenSource
	s.mu.Unlock()

	// Prevent newSess from being used separately.
	newSess.mu.Lock()
	newSess.service = nil
	newSess.mu.Unlock()

	fmt.Fprintf(os.Stderr, "ytmusic: re-authenticated successfully\n")
	return nil
}

// Service returns the YouTube API service, holding the lock briefly.
func (s *Session) Service() *youtube.Service {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.service
}

// Close is a no-op for YouTube Music sessions (no persistent connections).
func (s *Session) Close() {}

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
	return filepath.Join(dir, "ytmusic_credentials.json"), nil
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
