package player

import (
	"fmt"
	"io"

	"github.com/gopxl/beep/v2"
)

// trackPipeline bundles a decoded track's resources.
type trackPipeline struct {
	decoder  beep.StreamSeekCloser // raw decoder (for Position/Duration/Seek)
	stream   beep.Streamer         // decoder + optional resample (fed to gapless)
	format   beep.Format
	seekable bool
	rc       io.ReadCloser // source file/HTTP body
}

// close releases the pipeline's resources.
func (tp *trackPipeline) close() {
	if tp.decoder != nil {
		tp.decoder.Close()
	}
	if tp.rc != nil {
		tp.rc.Close()
	}
}

// closePipelines closes one or more pipelines that are no longer in use.
func closePipelines(ps ...*trackPipeline) {
	for _, tp := range ps {
		if tp != nil {
			tp.close()
		}
	}
}

// buildPipeline opens and decodes a track, returning a ready-to-play pipeline.
func (p *Player) buildPipeline(path string) (*trackPipeline, error) {
	rc, err := openSource(path)
	if err != nil {
		return nil, err
	}

	decoder, format, err := decode(rc, path, p.sr)
	if err != nil {
		rc.Close()
		return nil, fmt.Errorf("decode: %w", err)
	}

	// HTTP streams decoded natively read from a non-seekable http.Response.Body.
	// FFmpeg-decoded streams are fully buffered in memory and therefore seekable.
	_, isPCM := decoder.(*pcmStreamer)
	seekable := !isURL(path) || isPCM

	var s beep.Streamer = decoder
	if format.SampleRate != p.sr {
		s = beep.Resample(4, format.SampleRate, p.sr, s)
	}

	return &trackPipeline{
		decoder:  decoder,
		stream:   s,
		format:   format,
		seekable: seekable,
		rc:       rc,
	}, nil
}
