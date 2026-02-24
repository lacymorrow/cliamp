// Package playlist manages an ordered track list with shuffle and repeat support.
package playlist

import (
	"math/rand"
	"path/filepath"
	"slices"
	"strings"
)

// RepeatMode controls playlist repeat behavior.
type RepeatMode int

const (
	RepeatOff RepeatMode = iota
	RepeatAll
	RepeatOne
)

func (r RepeatMode) String() string {
	switch r {
	case RepeatAll:
		return "All"
	case RepeatOne:
		return "One"
	default:
		return "Off"
	}
}

// Track represents a single audio file.
type Track struct {
	Path   string
	Title  string
	Artist string
}

// TrackFromPath creates a Track by parsing the filename.
// Supports "Artist - Title" format, otherwise uses the filename as title.
func TrackFromPath(path string) Track {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	parts := strings.SplitN(name, " - ", 2)
	if len(parts) == 2 {
		return Track{Path: path, Artist: strings.TrimSpace(parts[0]), Title: strings.TrimSpace(parts[1])}
	}
	return Track{Path: path, Title: name}
}

// DisplayName returns a formatted display string for the track.
func (t Track) DisplayName() string {
	if t.Artist != "" {
		return t.Artist + " - " + t.Title
	}
	return t.Title
}

// Playlist manages an ordered list of tracks with shuffle and repeat support.
type Playlist struct {
	tracks    []Track
	order     []int // indices into tracks, shuffled or sequential
	pos       int   // current position in order
	shuffle   bool
	repeat    RepeatMode
	queue     []int // track indices queued to play next
	queuedIdx int   // track index currently playing from queue, -1 if none
}

// New creates an empty Playlist.
func New() *Playlist {
	return &Playlist{queuedIdx: -1}
}

// Add appends tracks to the playlist.
func (p *Playlist) Add(tracks ...Track) {
	start := len(p.tracks)
	p.tracks = append(p.tracks, tracks...)
	for i := start; i < len(p.tracks); i++ {
		p.order = append(p.order, i)
	}
}

// Len returns the number of tracks.
func (p *Playlist) Len() int { return len(p.tracks) }

// Current returns the currently selected track and its index.
func (p *Playlist) Current() (Track, int) {
	if len(p.tracks) == 0 {
		return Track{}, -1
	}
	if p.queuedIdx >= 0 {
		return p.tracks[p.queuedIdx], p.queuedIdx
	}
	idx := p.order[p.pos]
	return p.tracks[idx], idx
}

// Index returns the track index of the current position.
func (p *Playlist) Index() int {
	if len(p.order) == 0 {
		return -1
	}
	if p.queuedIdx >= 0 {
		return p.queuedIdx
	}
	return p.order[p.pos]
}

// Next advances to the next track. Returns false if at end with repeat off.
// Queued tracks are played first before resuming normal order.
func (p *Playlist) Next() (Track, bool) {
	if len(p.tracks) == 0 {
		return Track{}, false
	}
	// Play from queue first
	if len(p.queue) > 0 {
		idx := p.queue[0]
		p.queue = p.queue[1:]
		p.queuedIdx = idx
		return p.tracks[idx], true
	}
	p.queuedIdx = -1
	if p.repeat == RepeatOne {
		return p.tracks[p.order[p.pos]], true
	}
	if p.pos+1 < len(p.order) {
		p.pos++
		return p.tracks[p.order[p.pos]], true
	}
	if p.repeat == RepeatAll {
		p.pos = 0
		if p.shuffle {
			p.doShuffle()
		}
		return p.tracks[p.order[p.pos]], true
	}
	return Track{}, false
}

// Prev moves to the previous track. Wraps around with RepeatAll.
func (p *Playlist) Prev() (Track, bool) {
	p.queuedIdx = -1
	if len(p.tracks) == 0 {
		return Track{}, false
	}
	if p.pos > 0 {
		p.pos--
		return p.tracks[p.order[p.pos]], true
	}
	if p.repeat == RepeatAll {
		p.pos = len(p.order) - 1
		return p.tracks[p.order[p.pos]], true
	}
	return p.tracks[p.order[p.pos]], true
}

// SetIndex sets the current position to the given track index.
func (p *Playlist) SetIndex(i int) {
	p.queuedIdx = -1
	for pos, idx := range p.order {
		if idx == i {
			p.pos = pos
			return
		}
	}
}

// Queue adds a track to the play-next queue by its index.
func (p *Playlist) Queue(trackIdx int) {
	if trackIdx >= 0 && trackIdx < len(p.tracks) {
		p.queue = append(p.queue, trackIdx)
	}
}

// Dequeue removes a track from the queue. Returns true if it was found.
func (p *Playlist) Dequeue(trackIdx int) bool {
	for i, idx := range p.queue {
		if idx == trackIdx {
			p.queue = slices.Delete(p.queue, i, i+1)
			return true
		}
	}
	return false
}

// QueuePosition returns the 1-based position of a track in the queue,
// or 0 if the track is not queued.
func (p *Playlist) QueuePosition(trackIdx int) int {
	for i, idx := range p.queue {
		if idx == trackIdx {
			return i + 1
		}
	}
	return 0
}

// QueueLen returns the number of tracks in the queue.
func (p *Playlist) QueueLen() int { return len(p.queue) }

// Tracks returns all tracks in the playlist.
func (p *Playlist) Tracks() []Track { return p.tracks }

// ToggleShuffle enables or disables shuffle mode.
// Uses Fisher-Yates shuffle, preserving the current track at position 0.
func (p *Playlist) ToggleShuffle() {
	p.shuffle = !p.shuffle
	if p.shuffle {
		p.doShuffle()
		return
	}
	cur := p.order[p.pos]
	p.order = make([]int, len(p.tracks))
	for i := range p.order {
		p.order[i] = i
	}
	p.pos = cur
}

func (p *Playlist) doShuffle() {
	cur := p.order[p.pos]
	others := make([]int, 0, len(p.tracks)-1)
	for i := range len(p.tracks) {
		if i != cur {
			others = append(others, i)
		}
	}
	for i := len(others) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		others[i], others[j] = others[j], others[i]
	}
	p.order = make([]int, 0, len(p.tracks))
	p.order = append(p.order, cur)
	p.order = append(p.order, others...)
	p.pos = 0
}

// CycleRepeat cycles through Off -> All -> One.
func (p *Playlist) CycleRepeat() {
	p.repeat = (p.repeat + 1) % 3
}

// Shuffled returns whether shuffle is enabled.
func (p *Playlist) Shuffled() bool { return p.shuffle }

// Repeat returns the current repeat mode.
func (p *Playlist) Repeat() RepeatMode { return p.repeat }
