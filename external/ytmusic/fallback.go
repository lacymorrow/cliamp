package ytmusic

import (
	"math/rand/v2"
	"os"
	"strings"
)

// fallbackCredentials is a pool of Google Cloud OAuth2 Desktop app credentials
// used when the user has not configured their own client_id/client_secret.
// A random entry is selected each session to spread quota load across projects.
//
// These are Desktop-type OAuth2 credentials. Google allows embedding them in
// open-source desktop apps — the user still authenticates via their own Google
// account, so the credentials alone grant no access.
type oauthCreds struct {
	ClientID     string
	ClientSecret string
}

// builtinCredentials is the compiled-in pool. Populate via -ldflags or add
// entries directly for private builds. For public repos, use the
// CLIAMP_YT_CREDENTIALS environment variable instead to avoid push-protection.
var builtinCredentials []oauthCreds

// FallbackCredentials returns a random credential pair from the built-in pool
// or the CLIAMP_YT_CREDENTIALS environment variable (format: "clientID:clientSecret").
// Returns empty strings if neither source has credentials.
func FallbackCredentials() (clientID, clientSecret string) {
	// Check environment variable first.
	if env := os.Getenv("CLIAMP_YT_CREDENTIALS"); env != "" {
		if id, secret, ok := strings.Cut(env, ":"); ok && id != "" && secret != "" {
			return id, secret
		}
	}

	if len(builtinCredentials) == 0 {
		return "", ""
	}
	c := builtinCredentials[rand.IntN(len(builtinCredentials))]
	return c.ClientID, c.ClientSecret
}
