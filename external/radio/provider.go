// Package radio implements a playlist.Provider for internet radio stations.
// It includes a built-in cliamp radio stream and reads user-defined stations
// from ~/.config/cliamp/radios.toml.
package radio

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"cliamp/playlist"
)

const builtinName = "cliamp radio"
const builtinURL = "https://radio.cliamp.stream/lofi/stream.pls"

// Provider serves radio stations as single-track playlists.
type Provider struct {
	stations []station
}

type station struct {
	name string
	url  string
}

// New creates a Provider with the built-in station plus any user-defined
// stations from ~/.config/cliamp/radios.toml.
func New() *Provider {
	p := &Provider{
		stations: []station{{name: builtinName, url: builtinURL}},
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	path := filepath.Join(home, ".config", "cliamp", "radios.toml")
	if extra, err := loadStations(path); err == nil {
		p.stations = append(p.stations, extra...)
	}
	return p
}

func (p *Provider) Name() string { return "Radio" }

func (p *Provider) Playlists() ([]playlist.PlaylistInfo, error) {
	var out []playlist.PlaylistInfo
	for i, s := range p.stations {
		out = append(out, playlist.PlaylistInfo{
			ID:         strconv.Itoa(i),
			Name:       s.name,
			TrackCount: 1,
		})
	}
	return out, nil
}

func (p *Provider) Tracks(id string) ([]playlist.Track, error) {
	idx, err := strconv.Atoi(id)
	if err != nil || idx < 0 || idx >= len(p.stations) {
		return nil, errors.New("invalid station ID")
	}
	s := p.stations[idx]
	return []playlist.Track{{
		Path:   s.url,
		Title:  s.name,
		Stream: true,
	}}, nil
}

// loadStations parses a TOML file with [[station]] sections.
func loadStations(path string) ([]station, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var stations []station
	var current *station

	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "[[station]]" {
			if current != nil && current.name != "" && current.url != "" {
				stations = append(stations, *current)
			}
			current = &station{}
			continue
		}
		if current == nil {
			continue
		}

		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = unquote(val)

		switch key {
		case "name":
			current.name = val
		case "url":
			current.url = val
		}
	}
	if current != nil && current.name != "" && current.url != "" {
		stations = append(stations, *current)
	}
	return stations, nil
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
		return s[1 : len(s)-1]
	}
	return s
}
