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

// Tracks resolves CLI arguments into playlist tracks.
// Each argument may be a file path, directory, glob pattern, HTTP stream URL,
// M3U playlist URL, or RSS podcast feed URL.
func Tracks(args []string) ([]playlist.Track, error) {
	var files []string
	var feedTracks []playlist.Track

	for _, arg := range args {
		if playlist.IsURL(arg) {
			switch {
			case playlist.IsFeed(arg):
				tracks, err := resolveFeed(arg)
				if err != nil {
					return nil, fmt.Errorf("resolving feed %s: %w", arg, err)
				}
				feedTracks = append(feedTracks, tracks...)
			case playlist.IsM3U(arg):
				streams, err := resolveM3U(arg)
				if err != nil {
					return nil, fmt.Errorf("resolving m3u %s: %w", arg, err)
				}
				files = append(files, streams...)
			default:
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
				return nil, fmt.Errorf("scanning %s: %w", path, err)
			}
			files = append(files, resolved...)
		}
	}

	var tracks []playlist.Track
	for _, f := range files {
		tracks = append(tracks, playlist.TrackFromPath(f))
	}
	tracks = append(tracks, feedTracks...)
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
