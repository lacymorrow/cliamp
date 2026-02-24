package player

import (
	"encoding/binary"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/gopxl/beep/v2"
)

// decodeFFmpeg uses ffmpeg to decode any audio file into raw PCM,
// returning a seekable beep.StreamSeekCloser.
func decodeFFmpeg(path string, sr beep.SampleRate) (beep.StreamSeekCloser, beep.Format, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		ext := filepath.Ext(path)
		return nil, beep.Format{}, fmt.Errorf("ffmpeg is required to play %s files — install it with your package manager", ext)
	}

	cmd := exec.Command("ffmpeg",
		"-i", path,
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"-ar", fmt.Sprintf("%d", sr),
		"-ac", "2",
		"-loglevel", "error",
		"pipe:1",
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, beep.Format{}, fmt.Errorf("ffmpeg decode: %w", err)
	}

	format := beep.Format{
		SampleRate:  sr,
		NumChannels: 2,
		Precision:   2,
	}

	return &pcmStreamer{data: out}, format, nil
}

// pcmStreamer wraps raw s16le stereo PCM data as a beep.StreamSeekCloser.
type pcmStreamer struct {
	data []byte
	pos  int // current sample frame index
}

func (p *pcmStreamer) Stream(samples [][2]float64) (int, bool) {
	frameSize := 4 // 2 bytes * 2 channels
	totalFrames := len(p.data) / frameSize

	if p.pos >= totalFrames {
		return 0, false
	}

	n := 0
	for i := range samples {
		if p.pos >= totalFrames {
			break
		}
		off := p.pos * frameSize
		left := int16(binary.LittleEndian.Uint16(p.data[off : off+2]))
		right := int16(binary.LittleEndian.Uint16(p.data[off+2 : off+4]))
		samples[i][0] = float64(left) / 32768
		samples[i][1] = float64(right) / 32768
		p.pos++
		n++
	}
	return n, true
}

func (p *pcmStreamer) Err() error { return nil }

func (p *pcmStreamer) Len() int {
	return len(p.data) / 4
}

func (p *pcmStreamer) Position() int {
	return p.pos
}

func (p *pcmStreamer) Seek(pos int) error {
	if pos < 0 || pos > p.Len() {
		return fmt.Errorf("seek position %d out of range [0, %d]", pos, p.Len())
	}
	p.pos = pos
	return nil
}

func (p *pcmStreamer) Close() error {
	p.data = nil
	return nil
}
