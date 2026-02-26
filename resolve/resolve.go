// Package resolve converts CLI arguments (files, directories, globs, URLs,
// M3U playlists, and RSS feeds) into a flat list of playlist tracks.
package resolve

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"cliamp/player"
	"cliamp/playlist"
)

// Result holds the output of Args: instantly-resolved tracks and
// remote URLs (feeds, M3U) that need async HTTP fetching.
type Result struct {
	Tracks  []playlist.Track // local files, dirs, plain stream URLs
	Pending []string         // feed/M3U URLs to resolve asynchronously
}

// Args separates CLI arguments into immediately-resolved local tracks
// and pending remote URLs (feeds, M3U) that require HTTP fetching.
func Args(args []string) (Result, error) {
	var r Result
	var files []string

	for _, arg := range args {
		if playlist.IsURL(arg) {
			if playlist.IsFeed(arg) || playlist.IsM3U(arg) {
				r.Pending = append(r.Pending, arg)
			} else {
				files = append(files, arg)
			}
			continue
		}
		matches, err := filepath.Glob(arg)
		if err != nil || len(matches) == 0 {
			matches = []string{arg}
		}
		for _, path := range matches {
			resolved, err := collectAudioFiles(path)
			if err != nil {
				return r, fmt.Errorf("scanning %s: %w", path, err)
			}
			files = append(files, resolved...)
		}
	}

	for _, f := range files {
		r.Tracks = append(r.Tracks, playlist.TrackFromPath(f))
	}
	return r, nil
}

// Remote fetches feed and M3U URLs and returns the resolved tracks.
func Remote(urls []string) ([]playlist.Track, error) {
	var tracks []playlist.Track
	for _, u := range urls {
		switch {
		case playlist.IsFeed(u):
			t, err := resolveFeed(u)
			if err != nil {
				return nil, fmt.Errorf("resolving feed %s: %w", u, err)
			}
			tracks = append(tracks, t...)
		case playlist.IsM3U(u):
			streams, err := resolveM3U(u)
			if err != nil {
				return nil, fmt.Errorf("resolving m3u %s: %w", u, err)
			}
			for _, s := range streams {
				tracks = append(tracks, playlist.TrackFromPath(s))
			}
		}
	}
	return tracks, nil
}

// collectAudioFiles returns audio file paths for the given argument.
// If path is a directory, it walks it recursively collecting supported files.
// If path is a file with a supported extension, it returns it directly.
func collectAudioFiles(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() {
		if player.SupportedExts[strings.ToLower(filepath.Ext(path))] {
			return []string{path}, nil
		}
		return nil, nil
	}

	var files []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && player.SupportedExts[strings.ToLower(filepath.Ext(p))] {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.Sort(files)
	return files, nil
}

// resolveFeed fetches a podcast RSS feed and returns tracks with metadata.
func resolveFeed(feedURL string) ([]playlist.Track, error) {
	resp, err := http.Get(feedURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rss struct {
		Channel struct {
			Title string `xml:"title"`
			Items []struct {
				Title     string `xml:"title"`
				Enclosure struct {
					URL  string `xml:"url,attr"`
					Type string `xml:"type,attr"`
				} `xml:"enclosure"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
		return nil, fmt.Errorf("parsing feed: %w", err)
	}

	var tracks []playlist.Track
	for _, item := range rss.Channel.Items {
		if item.Enclosure.URL == "" {
			continue
		}
		tracks = append(tracks, playlist.Track{
			Path:   item.Enclosure.URL,
			Title:  item.Title,
			Artist: rss.Channel.Title,
			Stream: true,
		})
	}
	return tracks, nil
}

// resolveM3U fetches an M3U playlist URL and returns the stream URLs it contains.
func resolveM3U(m3uURL string) ([]string, error) {
	resp, err := http.Get(m3uURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var urls []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	return urls, scanner.Err()
}
