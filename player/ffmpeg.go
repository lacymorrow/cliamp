package player

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gopxl/beep/v2"
)

// pcmFrameSize is the byte size of one stereo s16le sample frame (2 channels * 2 bytes).
const pcmFrameSize = 4

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
		"-ar", strconv.Itoa(int(sr)),
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
	totalFrames := len(p.data) / pcmFrameSize

	if p.pos >= totalFrames {
		return 0, false
	}

	n := 0
	for i := range samples {
		if p.pos >= totalFrames {
			break
		}
		off := p.pos * pcmFrameSize
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
	return len(p.data) / pcmFrameSize
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

// decodeFFmpegStream starts ffmpeg as a subprocess and streams PCM data
// incrementally from its stdout pipe. Unlike decodeFFmpeg, this does not
// wait for the entire input to be read — suitable for live/infinite streams.
func decodeFFmpegStream(path string, sr beep.SampleRate) (*ffmpegPipeStreamer, beep.Format, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		ext := filepath.Ext(path)
		return nil, beep.Format{}, fmt.Errorf("ffmpeg is required to play %s files — install it with your package manager", ext)
	}

	cmd := exec.Command("ffmpeg",
		"-i", path,
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"-ar", strconv.Itoa(int(sr)),
		"-ac", "2",
		"-loglevel", "error",
		"pipe:1",
	)

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, beep.Format{}, fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, beep.Format{}, fmt.Errorf("ffmpeg start: %w", err)
	}

	format := beep.Format{
		SampleRate:  sr,
		NumChannels: 2,
		Precision:   2,
	}

	return &ffmpegPipeStreamer{cmd: cmd, reader: bufio.NewReaderSize(pipe, 64*1024), pipe: pipe}, format, nil
}

// ffmpegPipeStreamer reads PCM data incrementally from a running ffmpeg process.
type ffmpegPipeStreamer struct {
	cmd    *exec.Cmd
	reader *bufio.Reader
	pipe   io.ReadCloser
	buf    [pcmFrameSize]byte
	err    error
}

func (f *ffmpegPipeStreamer) Stream(samples [][2]float64) (int, bool) {
	n := 0
	for i := range samples {
		_, err := io.ReadFull(f.reader, f.buf[:])
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				f.err = err
			}
			break
		}
		left := int16(binary.LittleEndian.Uint16(f.buf[0:2]))
		right := int16(binary.LittleEndian.Uint16(f.buf[2:4]))
		samples[i][0] = float64(left) / 32768
		samples[i][1] = float64(right) / 32768
		n++
	}
	return n, n > 0
}

func (f *ffmpegPipeStreamer) Err() error { return f.err }

func (f *ffmpegPipeStreamer) Len() int { return 0 }

func (f *ffmpegPipeStreamer) Position() int { return 0 }

func (f *ffmpegPipeStreamer) Seek(int) error { return nil }

func (f *ffmpegPipeStreamer) Close() error {
	f.pipe.Close()
	if f.cmd.Process != nil {
		f.cmd.Process.Kill()
	}
	f.cmd.Wait() // ignore error — process was intentionally killed
	return nil
}

// decodeFFmpegLocal starts ffmpeg as a streaming pipe for local files, giving
// instant playback start instead of buffering the entire file to memory.
// Seeking is supported by killing and restarting ffmpeg with a -ss offset.
// Duration is probed via ffprobe so the seek bar works.
func decodeFFmpegLocal(path string, sr beep.SampleRate) (*localFFmpegStreamer, beep.Format, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		ext := filepath.Ext(path)
		return nil, beep.Format{}, fmt.Errorf("ffmpeg is required to play %s files — install it with your package manager", ext)
	}

	total := probeFrames(path, sr)

	s := &localFFmpegStreamer{path: path, sr: sr, total: total}
	if err := s.start(0); err != nil {
		return nil, beep.Format{}, err
	}

	format := beep.Format{
		SampleRate:  sr,
		NumChannels: 2,
		Precision:   2,
	}
	return s, format, nil
}

// localFFmpegStreamer streams PCM from a running ffmpeg subprocess for local
// files. Unlike pcmStreamer it does not buffer the entire file — playback
// starts as soon as ffmpeg begins producing output. Seeking kills the current
// process and restarts with -ss (demuxer-level fast seek).
type localFFmpegStreamer struct {
	path   string
	sr     beep.SampleRate
	cmd    *exec.Cmd
	reader *bufio.Reader
	pipe   io.ReadCloser
	buf    [pcmFrameSize]byte
	err    error
	pos    int // current sample frame
	total  int // total frames (0 if unknown)
}

// start launches ffmpeg, optionally seeking to seekPos sample frames.
func (s *localFFmpegStreamer) start(seekPos int) error {
	var args []string
	if seekPos > 0 {
		secs := float64(seekPos) / float64(s.sr)
		args = append(args, "-ss", strconv.FormatFloat(secs, 'f', 3, 64))
	}
	args = append(args,
		"-i", s.path,
		"-f", "s16le",
		"-acodec", "pcm_s16le",
		"-ar", strconv.Itoa(int(s.sr)),
		"-ac", "2",
		"-loglevel", "error",
		"pipe:1",
	)

	cmd := exec.Command("ffmpeg", args...)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("ffmpeg pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg start: %w", err)
	}

	s.cmd = cmd
	s.pipe = pipe
	s.reader = bufio.NewReaderSize(pipe, 64*1024)
	s.pos = seekPos
	s.err = nil
	return nil
}

// stop kills the running ffmpeg process and cleans up.
func (s *localFFmpegStreamer) stop() {
	if s.pipe != nil {
		s.pipe.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
		s.cmd.Wait()
	}
}

func (s *localFFmpegStreamer) Stream(samples [][2]float64) (int, bool) {
	n := 0
	for i := range samples {
		_, err := io.ReadFull(s.reader, s.buf[:])
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				s.err = err
			}
			break
		}
		left := int16(binary.LittleEndian.Uint16(s.buf[0:2]))
		right := int16(binary.LittleEndian.Uint16(s.buf[2:4]))
		samples[i][0] = float64(left) / 32768
		samples[i][1] = float64(right) / 32768
		s.pos++
		n++
	}
	return n, n > 0
}

func (s *localFFmpegStreamer) Err() error    { return s.err }
func (s *localFFmpegStreamer) Len() int      { return s.total }
func (s *localFFmpegStreamer) Position() int { return s.pos }

func (s *localFFmpegStreamer) Seek(pos int) error {
	if pos < 0 {
		pos = 0
	}
	if s.total > 0 && pos > s.total {
		pos = s.total
	}
	s.stop()
	return s.start(pos)
}

func (s *localFFmpegStreamer) Close() error {
	s.stop()
	return nil
}

// probeFrames uses ffprobe to quickly read file duration from metadata and
// converts it to sample frames. This only reads the container header, so it
// returns almost instantly even for very large files.
func probeFrames(path string, sr beep.SampleRate) int {
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
	if err != nil {
		return 0
	}
	secs, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0
	}
	return int(secs * float64(sr))
}
