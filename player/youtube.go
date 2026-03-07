package player

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/kkdai/youtube/v2"
)

// youtubeStreamer streams PCM audio from a native YouTube download piped through
// ffmpeg. Unlike ytdlPipeStreamer it does not shell out to yt-dlp — the audio
// stream is obtained directly via the kkdai/youtube library.
type youtubeStreamer struct {
	ffmpegCmd *exec.Cmd
	pipe      io.ReadCloser // ffmpeg stdout (PCM output)
	reader    *bufio.Reader // buffered reader over pipe
	ytBody    io.ReadCloser // YouTube HTTP stream body
	buf       [pcmFrameSize32]byte
	f32       bool // true = f32le, false = s16le
	err       error
}

func (y *youtubeStreamer) Stream(samples [][2]float64) (int, bool) {
	return streamFromReader(y.reader, samples, y.buf[:], y.f32, &y.err)
}

func (y *youtubeStreamer) Err() error     { return y.err }
func (y *youtubeStreamer) Len() int       { return 0 }
func (y *youtubeStreamer) Position() int  { return 0 }
func (y *youtubeStreamer) Seek(int) error { return nil }

func (y *youtubeStreamer) Close() error {
	if y.ffmpegCmd.Process != nil {
		y.ffmpegCmd.Process.Kill()
	}
	y.pipe.Close()
	y.ffmpegCmd.Wait()
	// Close the YouTube HTTP body to stop the background download.
	if y.ytBody != nil {
		y.ytBody.Close()
	}
	return nil
}

// decodeYouTubePipe fetches a YouTube audio stream via the kkdai/youtube library
// and pipes it through ffmpeg for PCM conversion. Returns a streamer compatible
// with the existing pipe-based pattern.
func decodeYouTubePipe(pageURL string, sr beep.SampleRate, bitDepth int) (*youtubeStreamer, beep.Format, time.Duration, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, beep.Format{}, 0, fmt.Errorf("ffmpeg is required to play YouTube audio — install it with your package manager")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client := youtube.Client{}
	video, err := client.GetVideoContext(ctx, pageURL)
	if err != nil {
		return nil, beep.Format{}, 0, fmt.Errorf("youtube metadata: %w", err)
	}

	// Select the best audio-only format. Prefer audio/mp4 (m4a/AAC), fall
	// back to audio/webm (opus), then any format with audio channels.
	format := pickAudioFormat(video.Formats)
	if format == nil {
		return nil, beep.Format{}, 0, fmt.Errorf("no audio format available for %s", pageURL)
	}

	stream, _, err := client.GetStreamContext(context.Background(), video, format)
	if err != nil {
		return nil, beep.Format{}, 0, fmt.Errorf("youtube stream: %w", err)
	}

	// Pipe the YouTube stream through ffmpeg for PCM conversion.
	pcmFmt, codec, precision := ffmpegPCMArgs(bitDepth)
	ffmpegCmd := exec.Command("ffmpeg",
		"-i", "pipe:0",
		"-f", pcmFmt,
		"-acodec", codec,
		"-ar", strconv.Itoa(int(sr)),
		"-ac", "2",
		"-loglevel", "error",
		"pipe:1",
	)
	ffmpegCmd.Stdin = stream
	ffmpegPipe, err := ffmpegCmd.StdoutPipe()
	if err != nil {
		stream.Close()
		return nil, beep.Format{}, 0, fmt.Errorf("ffmpeg stdout pipe: %w", err)
	}
	if err := ffmpegCmd.Start(); err != nil {
		stream.Close()
		return nil, beep.Format{}, 0, fmt.Errorf("ffmpeg start: %w", err)
	}

	beepFormat := beep.Format{
		SampleRate:  sr,
		NumChannels: 2,
		Precision:   precision,
	}

	return &youtubeStreamer{
		ffmpegCmd: ffmpegCmd,
		pipe:      ffmpegPipe,
		reader:    bufio.NewReaderSize(ffmpegPipe, 64*1024),
		ytBody:    stream,
		f32:       bitDepth == 32,
	}, beepFormat, video.Duration, nil
}

// pickAudioFormat selects the best audio-only format from the list.
// Prefers audio/mp4 (AAC), then audio/webm (opus), then any format with audio.
func pickAudioFormat(formats youtube.FormatList) *youtube.Format {
	// Try audio/mp4 first (best ffmpeg compatibility).
	if list := formats.Type("audio/mp4"); len(list) > 0 {
		list.Sort()
		return &list[0]
	}
	// Fall back to audio/webm (opus).
	if list := formats.Type("audio/webm"); len(list) > 0 {
		list.Sort()
		return &list[0]
	}
	// Last resort: any format with audio channels.
	if list := formats.WithAudioChannels(); len(list) > 0 {
		list.Sort()
		return &list[0]
	}
	return nil
}

// buildYouTubePipeline creates a non-seekable trackPipeline for a YouTube URL
// using native streaming (no yt-dlp dependency).
func (p *Player) buildYouTubePipeline(pageURL string) (*trackPipeline, error) {
	p.streamTitle.Store("")

	decoder, format, duration, err := decodeYouTubePipe(pageURL, p.sr, p.bitDepth)
	if err != nil {
		return nil, err
	}

	return &trackPipeline{
		decoder:       decoder,
		stream:        decoder,
		format:        format,
		seekable:      false,
		path:          pageURL,
		knownDuration: duration,
	}, nil
}
