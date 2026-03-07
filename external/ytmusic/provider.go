package ytmusic

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cliamp/playlist"
)

// YouTubeMusicProvider implements playlist.Provider using the YouTube Data API v3
// for playlist/track metadata. Audio playback is handled by the existing yt-dlp pipeline.
type YouTubeMusicProvider struct {
	session      *Session
	clientID     string
	clientSecret string
	hasCookies   bool // true when cookies_from is configured (can play private/uploaded tracks)
	mu           sync.Mutex
	trackCache   map[string][]playlist.Track // playlist ID -> cached tracks
}

// New creates a YouTubeMusicProvider. If session is nil, authentication is
// deferred until the user first selects the YouTube Music provider.
// Set hasCookies to true when cookies_from is configured (enables uploaded/private tracks).
func New(session *Session, clientID, clientSecret string, hasCookies bool) *YouTubeMusicProvider {
	return &YouTubeMusicProvider{
		session:      session,
		clientID:     clientID,
		clientSecret: clientSecret,
		hasCookies:   hasCookies,
		trackCache:   make(map[string][]playlist.Track),
	}
}

// ensureSession tries to create a session using stored credentials only
// (no browser). Returns playlist.ErrNeedsAuth if interactive sign-in is needed.
func (p *YouTubeMusicProvider) ensureSession() error {
	p.mu.Lock()
	if p.session != nil {
		p.mu.Unlock()
		return nil
	}
	clientID := p.clientID
	p.mu.Unlock()

	clientSecret := p.clientSecret
	if clientID == "" {
		return fmt.Errorf("ytmusic: no client ID available")
	}
	fmt.Fprintf(os.Stderr, "ytmusic: attempting silent auth...\n")
	sess, err := NewSessionSilent(context.Background(), clientID, clientSecret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ytmusic: silent auth failed: %v\n", err)
		return playlist.ErrNeedsAuth
	}
	fmt.Fprintf(os.Stderr, "ytmusic: silent auth succeeded\n")
	p.mu.Lock()
	p.session = sess
	p.mu.Unlock()
	return nil
}

// Authenticate runs the interactive sign-in flow (opens browser, waits for callback).
func (p *YouTubeMusicProvider) Authenticate() error {
	p.mu.Lock()
	if p.session != nil {
		p.mu.Unlock()
		return nil
	}
	clientID := p.clientID
	clientSecret := p.clientSecret
	p.mu.Unlock()

	if clientID == "" {
		return fmt.Errorf("ytmusic: no client ID available")
	}
	sess, err := NewSession(context.Background(), clientID, clientSecret)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.session = sess
	p.mu.Unlock()
	return nil
}

// Close releases the session if one was created.
func (p *YouTubeMusicProvider) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.session != nil {
		p.session.Close()
		p.session = nil
	}
}

func (p *YouTubeMusicProvider) Name() string { return "YouTube Music" }

// Playlists returns the authenticated user's YouTube Music playlists.
func (p *YouTubeMusicProvider) Playlists() ([]playlist.PlaylistInfo, error) {
	if err := p.ensureSession(); err != nil {
		return nil, err
	}

	svc := p.session.Service()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var all []playlist.PlaylistInfo

	// Add "Liked Music" entry (YouTube's special LL playlist).
	// Fetch its actual item count via a direct playlist lookup.
	likedCount := 0
	if llResp, err := svc.Playlists.List([]string{"contentDetails"}).
		Id("LL").
		Context(ctx).
		Do(); err == nil && len(llResp.Items) > 0 {
		likedCount = int(llResp.Items[0].ContentDetails.ItemCount)
	}
	all = append(all, playlist.PlaylistInfo{
		ID:         "LL",
		Name:       "Liked Videos",
		TrackCount: likedCount,
	})

	fmt.Fprintf(os.Stderr, "ytmusic: fetching playlists...\n")
	pageToken := ""
	for {
		call := svc.Playlists.List([]string{"snippet", "contentDetails"}).
			Mine(true).
			MaxResults(50).
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("ytmusic: list playlists: %w", err)
		}

		fmt.Fprintf(os.Stderr, "ytmusic: got %d playlists (page)\n", len(resp.Items))

		for _, item := range resp.Items {
			all = append(all, playlist.PlaylistInfo{
				ID:         item.Id,
				Name:       item.Snippet.Title,
				TrackCount: int(item.ContentDetails.ItemCount),
			})
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	fmt.Fprintf(os.Stderr, "ytmusic: total %d playlists\n", len(all))
	return all, nil
}

// Tracks returns all tracks for the given YouTube playlist ID.
// Track.Path is set to a YouTube Music URL for the player to resolve via yt-dlp.
// Results are cached by playlist ID for the session lifetime.
func (p *YouTubeMusicProvider) Tracks(playlistID string) ([]playlist.Track, error) {
	if err := p.ensureSession(); err != nil {
		return nil, err
	}

	// Check cache.
	p.mu.Lock()
	if cached, ok := p.trackCache[playlistID]; ok {
		p.mu.Unlock()
		return cached, nil
	}
	p.mu.Unlock()

	svc := p.session.Service()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Collect all playlist items.
	type itemInfo struct {
		videoID string
		title   string
		channel string
	}
	var items []itemInfo

	pageToken := ""
	for {
		call := svc.PlaylistItems.List([]string{"snippet", "contentDetails"}).
			PlaylistId(playlistID).
			MaxResults(50).
			Context(ctx)
		if pageToken != "" {
			call = call.PageToken(pageToken)
		}

		resp, err := call.Do()
		if err != nil {
			return nil, fmt.Errorf("ytmusic: list playlist items: %w", err)
		}

		for _, item := range resp.Items {
			vid := item.ContentDetails.VideoId
			if vid == "" {
				continue // skip deleted/private videos
			}
			title := item.Snippet.Title
			if title == "Private video" || title == "Deleted video" {
				continue
			}
			channel := item.Snippet.VideoOwnerChannelTitle
			// Skip uploaded/private tracks when cookies aren't configured (they can't be played).
			if !p.hasCookies && (channel == "Music Library Uploads" || channel == "") {
				continue
			}
			items = append(items, itemInfo{
				videoID: vid,
				title:   title,
				channel: channel,
			})
		}

		if resp.NextPageToken == "" {
			break
		}
		pageToken = resp.NextPageToken
	}

	// Batch-fetch durations via videos.list (up to 50 per request).
	durations := make(map[string]int) // videoID -> seconds
	for i := 0; i < len(items); i += 50 {
		end := i + 50
		if end > len(items) {
			end = len(items)
		}
		var ids []string
		for _, it := range items[i:end] {
			ids = append(ids, it.videoID)
		}

		vResp, err := svc.Videos.List([]string{"contentDetails"}).
			Id(ids...).
			Context(ctx).
			Do()
		if err != nil {
			return nil, fmt.Errorf("ytmusic: fetch video details: %w", err)
		}
		for _, v := range vResp.Items {
			durations[v.Id] = parseISO8601Duration(v.ContentDetails.Duration)
		}
	}

	// Build track list.
	var tracks []playlist.Track
	for _, it := range items {
		tracks = append(tracks, playlist.Track{
			Path:         "https://music.youtube.com/watch?v=" + it.videoID,
			Title:        it.title,
			Artist:       cleanChannelName(it.channel),
			Stream:       false,
			DurationSecs: durations[it.videoID],
		})
	}

	// Cache the fetched tracks.
	p.mu.Lock()
	p.trackCache[playlistID] = tracks
	p.mu.Unlock()

	return tracks, nil
}

// iso8601Re matches ISO 8601 duration components (e.g. PT4M13S, PT1H2M3S).
var iso8601Re = regexp.MustCompile(`P(?:(\d+)D)?T?(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?`)

// parseISO8601Duration parses an ISO 8601 duration string (e.g. "PT4M13S") to seconds.
func parseISO8601Duration(d string) int {
	m := iso8601Re.FindStringSubmatch(d)
	if m == nil {
		return 0
	}
	var total int
	if m[1] != "" {
		v, _ := strconv.Atoi(m[1])
		total += v * 86400
	}
	if m[2] != "" {
		v, _ := strconv.Atoi(m[2])
		total += v * 3600
	}
	if m[3] != "" {
		v, _ := strconv.Atoi(m[3])
		total += v * 60
	}
	if m[4] != "" {
		v, _ := strconv.Atoi(m[4])
		total += v
	}
	return total
}

// cleanChannelName strips common suffixes like " - Topic" from YouTube Music
// auto-generated channel names to produce cleaner artist names.
func cleanChannelName(name string) string {
	name = strings.TrimSuffix(name, " - Topic")
	return name
}
