package navidrome

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"cliamp/playlist"
)

type NavidromeClient struct {
	URL      string
	User     string
	Password string
}

type NavidromePlaylist struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"songCount"`
}

type NavidromeTrack struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
}

func (c *NavidromeClient) Name() string {
	return "Navidrome"
}

func (c *NavidromeClient) buildURL(endpoint string, params url.Values) string {
	salt := fmt.Sprintf("%d", time.Now().UnixNano())
	hash := md5.Sum([]byte(c.Password + salt))
	token := hex.EncodeToString(hash[:])

	if params == nil {
		params = url.Values{}
	}
	params.Set("u", c.User)
	params.Set("t", token)
	params.Set("s", salt)
	params.Set("v", "1.0.0")
	params.Set("c", "cliamp")
	params.Set("f", "json")

	return fmt.Sprintf("%s/rest/%s?%s", c.URL, endpoint, params.Encode())
}

func (c *NavidromeClient) Playlists() ([]playlist.PlaylistInfo, error) {
	resp, err := http.Get(c.buildURL("getPlaylists", nil))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		SubsonicResponse struct {
			Playlists struct {
				Playlist []struct {
					ID    string `json:"id"`
					Name  string `json:"name"`
					Count int    `json:"songCount"`
				} `json:"playlist"`
			} `json:"playlists"`
		} `json:"subsonic-response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var lists []playlist.PlaylistInfo
	for _, p := range result.SubsonicResponse.Playlists.Playlist {
		lists = append(lists, playlist.PlaylistInfo{
			ID:         p.ID,
			Name:       p.Name,
			TrackCount: p.Count,
		})
	}
	return lists, nil
}

func (c *NavidromeClient) Tracks(id string) ([]playlist.Track, error) {
	resp, err := http.Get(c.buildURL("getPlaylist", url.Values{"id": {id}}))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		SubsonicResponse struct {
			Playlist struct {
				Entry []struct {
					ID     string `json:"id"`
					Title  string `json:"title"`
					Artist string `json:"artist"`
				} `json:"entry"`
			} `json:"playlist"`
		} `json:"subsonic-response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var tracks []playlist.Track
	for _, t := range result.SubsonicResponse.Playlist.Entry {
		tracks = append(tracks, playlist.Track{
			Path:   c.streamURL(t.ID),
			Title:  t.Title,
			Artist: t.Artist,
		})
	}
	return tracks, nil
}

// StreamURL generates the authenticated streaming URL for a track ID.
func (c *NavidromeClient) streamURL(id string) string {
	return c.buildURL("stream", url.Values{"id": {id}, "format": {"mp3"}})
}
