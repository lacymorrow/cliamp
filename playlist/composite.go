package playlist

import (
	"fmt"
	"strconv"
	"strings"
)

// CompositeProvider merges multiple Provider implementations into one,
// prefixing playlist IDs with a provider index for disambiguation.
// When only one provider is present, it delegates directly without prefixing.
type CompositeProvider struct {
	providers []Provider
}

// NewComposite creates a CompositeProvider from the given providers.
// Nil providers are filtered out. Returns nil if no providers remain.
func NewComposite(providers ...Provider) *CompositeProvider {
	var valid []Provider
	for _, p := range providers {
		if p != nil {
			valid = append(valid, p)
		}
	}
	if len(valid) == 0 {
		return nil
	}
	return &CompositeProvider{providers: valid}
}

func (c *CompositeProvider) Name() string {
	if len(c.providers) == 1 {
		return c.providers[0].Name()
	}
	return "Playlists"
}

// Playlists merges lists from all providers, prefixing IDs when multiple
// providers are present.
func (c *CompositeProvider) Playlists() ([]PlaylistInfo, error) {
	var all []PlaylistInfo
	for i, p := range c.providers {
		lists, err := p.Playlists()
		if err != nil {
			return nil, fmt.Errorf("provider %s: playlists: %w", p.Name(), err)
		}
		for _, l := range lists {
			if len(c.providers) > 1 {
				l.ID = fmt.Sprintf("%d:%s", i, l.ID)
				l.Name = fmt.Sprintf("[%s] %s", p.Name(), l.Name)
			}
			all = append(all, l)
		}
	}
	return all, nil
}

// Tracks parses the provider prefix from the ID and dispatches to the
// correct provider.
func (c *CompositeProvider) Tracks(id string) ([]Track, error) {
	if len(c.providers) == 1 {
		tracks, err := c.providers[0].Tracks(id)
		if err != nil {
			return nil, fmt.Errorf("provider %s: tracks: %w", c.providers[0].Name(), err)
		}
		return tracks, nil
	}

	idx, realID, ok := strings.Cut(id, ":")
	if !ok {
		return nil, fmt.Errorf("invalid composite ID: %s", id)
	}
	i, err := strconv.Atoi(idx)
	if err != nil || i < 0 || i >= len(c.providers) {
		return nil, fmt.Errorf("invalid provider index in ID: %s", id)
	}
	tracks, err := c.providers[i].Tracks(realID)
	if err != nil {
		return nil, fmt.Errorf("provider %s: tracks: %w", c.providers[i].Name(), err)
	}
	return tracks, nil
}
