package ytmusic

import (
	"math/rand/v2"
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

var fallbackCredentials = []oauthCreds{
	{
		// ClientID:     "REMOVED_CLIENT_ID",
		// ClientSecret: "REMOVED_CLIENT_SECRET",
	},
}

// FallbackCredentials returns a random credential pair from the built-in pool,
// or empty strings if the pool is empty.
func FallbackCredentials() (clientID, clientSecret string) {
	if len(fallbackCredentials) == 0 {
		return "", ""
	}
	c := fallbackCredentials[rand.IntN(len(fallbackCredentials))]
	return c.ClientID, c.ClientSecret
}
