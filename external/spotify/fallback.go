package spotify

import (
	"math/rand/v2"
)

// fallbackClientIDs is a pool of Spotify Developer app client IDs that are
// used when the user has not configured their own client_id.  A random entry
// is selected each session to spread rate-limit load across apps.
//
// These are public OAuth2 client IDs (no secrets) — the PKCE flow used by
// cliamp does not require a client secret, so embedding them is safe.
var fallbackClientIDs = []string{
	// Add your Spotify app client IDs here, one per line.
	// "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
	"9cff76da7237414892754bfe1c841d9f", // @lacymorrow
}

// FallbackClientID returns a random client ID from the built-in pool,
// or "" if the pool is empty.
func FallbackClientID() string {
	if len(fallbackClientIDs) == 0 {
		return ""
	}
	return fallbackClientIDs[rand.IntN(len(fallbackClientIDs))]
}
