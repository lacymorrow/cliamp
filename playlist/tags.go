package playlist

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
)

// ReadTags reads embedded metadata (ID3v2, Vorbis comments, MP4 atoms) from
// a local audio file and returns a Track. Falls back to filename parsing if
// tag reading fails or the tags contain no title.
func ReadTags(path string) Track {
	f, err := os.Open(path)
	if err != nil {
		return trackFromFilename(path)
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil || m == nil || strings.TrimSpace(m.Title()) == "" {
		return trackFromFilename(path)
	}

	t := Track{
		Path:   path,
		Title:  strings.TrimSpace(m.Title()),
		Artist: strings.TrimSpace(m.Artist()),
		Album:  strings.TrimSpace(m.Album()),
		Genre:  strings.TrimSpace(m.Genre()),
		Year:   m.Year(),
	}
	trackNum, _ := m.Track()
	t.TrackNumber = trackNum
	return t
}

// trackFromFilename creates a Track by parsing "Artist - Title" from the
// filename, or using the bare filename as the title.
func trackFromFilename(path string) Track {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	parts := strings.SplitN(name, " - ", 2)
	if len(parts) == 2 {
		return Track{Path: path, Artist: strings.TrimSpace(parts[0]), Title: strings.TrimSpace(parts[1])}
	}
	return Track{Path: path, Title: name}
}
