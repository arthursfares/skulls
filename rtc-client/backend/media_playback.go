package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hraban/opus"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// ----------------------------------------------------------------------
// media playback: /play fetches a YouTube URL's audio entirely on this
// client (yt-dlp resolves/streams it, ffmpeg decodes/resamples it to raw
// PCM) and feeds it to a synthetic peer.
// ----------------------------------------------------------------------

var (
	pipelineMutex  sync.Mutex
	pipelineCancel context.CancelFunc
	pipelineDone   chan struct{} 		// closed when feedPipeline exits - lets stopPipeline block until it's safe to start/teardown

	playbackBusy atomic.Bool 			// guards PlayCmd against a rapid double /play racing two spawns
)

// NOTE. PlayCmd resolves the video's title, spawns a synthetic identity if one
// isn't already active for this client, and starts the yt-dlp | ffmpeg
// pipeline feeding it. A second /play while one is already active does not
// respawn - it just replaces the audio feeding the existing synthetic.
func PlayCmd(url string) tea.Cmd {
	return func() tea.Msg {
		if !playbackBusy.CompareAndSwap(false, true) { return ErrorMsg{Reason: "a /play is already starting"} }
		defer playbackBusy.Store(false)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		title, err := resolveTitle(ctx, url)
		cancel()
		if err != nil { title = "media" }

		syntheticMutex.Lock()
		needsSpawn := activeSyntheticID == ""
		syntheticMutex.Unlock()

		if needsSpawn {
			id, existingPeers, err := requestSpawn(title)
			if err != nil { return ErrorMsg{Reason: "couldn't start playback: " + err.Error()} }
			track, err := newSyntheticTrack()
			if err != nil { despawnSynthetic(); return ErrorMsg{Reason: "couldn't create media track: " + err.Error()} }
			buf := Mixer.addPeer(id)
			syntheticMutex.Lock()
			activeSyntheticID, syntheticTrack, syntheticLocalBuf = id, track, buf
			syntheticMutex.Unlock()
			for _, p := range existingPeers {
				go callSyntheticPeer(id, p.ID)
			}
		} else {
			stopPipeline() // kill the previous yt-dlp/ffmpeg pair
		}

		if err := startPipeline(url); err != nil {
			if needsSpawn { despawnSynthetic() }
			return ErrorMsg{Reason: "playback error: " + err.Error()}
		}
		return PlaybackStartedMsg{Title: title}
	}
}

// NOTE. StopCmd kills the pipeline and tears down the synthetic, if this client
// has one active.
func StopCmd() tea.Cmd {
	return func() tea.Msg {
		syntheticMutex.Lock()
		active := activeSyntheticID != ""
		syntheticMutex.Unlock()
		if !active { return LogMsg{Text: "nothing is playing"} }
		stopPipeline()
		despawnSynthetic()
		return PlaybackStoppedMsg{}
	}
}

func resolveTitle(ctx context.Context, url string) (string, error) {
	out, err := exec.CommandContext(ctx, "yt-dlp", "--print", "title", "--no-playlist", "--skip-download", url).Output()
	if err != nil { return "", friendlyExecErr("yt-dlp", err) }
	title := strings.TrimSpace(string(out))
	if title == "" { return "", fmt.Errorf("empty title") }
	return title, nil
}

func friendlyExecErr(bin string, err error) error {
	if errors.Is(err, exec.ErrNotFound) { return fmt.Errorf("%s not found on PATH", bin) }
	return err
}

// NOTE. startPipeline launches yt-dlp piped into ffmpeg 
// (matching sampleRate/channels in audio.go) and a goroutine that frames and
// Opus-encodes the decoded PCM onto the shared synthetic track, while also
// feeding it directly into this client's own Mixer buffer so it's heard
// locally without an RTP round-trip.
func startPipeline(url string) error {
	pipelineMutex.Lock()
	defer pipelineMutex.Unlock()

	syntheticMutex.Lock()
	track, buf := syntheticTrack, syntheticLocalBuf
	syntheticMutex.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	ytdlp := exec.CommandContext(ctx, "yt-dlp", "-f", "bestaudio", "--no-playlist", "-o", "-", "--quiet", url)
	ffmpeg := exec.CommandContext(ctx, "ffmpeg", "-i", "pipe:0", "-f", "s16le", "-ar", strconv.Itoa(sampleRate), "-ac", strconv.Itoa(channels), "-loglevel", "error", "pipe:1")

	ytdlpErr := newTailWriter(4096)
	ffmpegErr := newTailWriter(4096)
	ytdlp.Stderr = ytdlpErr
	ffmpeg.Stderr = ffmpegErr

	ytdlpOut, err := ytdlp.StdoutPipe()
	if err != nil { cancel(); return err }
	ffmpeg.Stdin = ytdlpOut
	ffmpegOut, err := ffmpeg.StdoutPipe()
	if err != nil { cancel(); return err }

	if err := ytdlp.Start(); err != nil { cancel(); return friendlyExecErr("yt-dlp", err) }
	if err := ffmpeg.Start(); err != nil { cancel(); ytdlp.Process.Kill(); return friendlyExecErr("ffmpeg", err) }

	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppAudio)
	if err != nil { cancel(); ytdlp.Process.Kill(); ffmpeg.Process.Kill(); return err }

	done := make(chan struct{})
	pipelineCancel, pipelineDone = cancel, done
	go feedPipeline(ctx, ffmpegOut, enc, track, buf, ytdlp, ffmpeg, ytdlpErr, ffmpegErr, done)
	return nil
}

// NOTE. feedPipeline paces itself to real time (a ticker firing once per
// frameDuration) rather than draining ffmpeg's stdout as fast as it'll
// give it up - ffmpeg decodes far faster than playback speed, so without
// this the whole track gets crunched through the (200ms-capped) jitter
// buffer in a couple of seconds, most of it discarded as overflow, and then
// goes silent once ffmpeg hits EOF.
func feedPipeline(ctx context.Context, stdout io.ReadCloser, enc *opus.Encoder, track *webrtc.TrackLocalStaticSample,
	buf *peerAudioBuffer, ytdlp *exec.Cmd, ffmpeg *exec.Cmd, ytdlpErr, ffmpegErr *tailWriter, done chan struct{}) {
	defer close(done)

	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	raw := make([]byte, frameSizeSamples*2) // s16le mono, 20ms
	sawData := false
	for {
		if _, err := io.ReadFull(stdout, raw); err != nil { break } // EOF (track ended) or pipe killed by stopPipeline's cancel()
		sawData = true
		<-ticker.C // hold to real time
		samples := bytesToInt16(raw)
		if buf != nil { buf.push(samples) }
		if track == nil { continue }
		out := make([]byte, maxOpusFrameSize)
		n, err := enc.Encode(samples, out)
		if err != nil { program.Send(ErrorMsg{Reason: "opus encode error: " + err.Error()}); continue }
		if err := track.WriteSample(media.Sample{Data: out[:n], Duration: frameDuration}); err != nil { program.Send(ErrorMsg{Reason: "media track write error: " + err.Error()}) }
	}

	ffmpegWaitErr := ffmpeg.Wait()
	ytdlpWaitErr := ytdlp.Wait()

	if ctx.Err() != nil { return }

	switch {
	case !sawData:
		reason := "playback failed before any audio arrived"
		if tail := lastLine(ytdlpErr.String()); tail != "" {
			reason += " - yt-dlp: " + tail
		} else if tail := lastLine(ffmpegErr.String()); tail != "" {
			reason += " - ffmpeg: " + tail
		} else if ytdlpWaitErr != nil {
			reason += " - " + ytdlpWaitErr.Error()
		}
		program.Send(ErrorMsg{Reason: reason})
		despawnSynthetic()
	case ytdlpWaitErr != nil || ffmpegWaitErr != nil:
		reason := "playback stopped early"
		if tail := lastLine(ffmpegErr.String()); tail != "" { reason += " - ffmpeg: " + tail }
		program.Send(ErrorMsg{Reason: reason})
		despawnSynthetic()
	default:
		program.Send(PlaybackStoppedMsg{})
		despawnSynthetic()
	}
}


func newTailWriter(n int) *tailWriter { return &tailWriter{n: n} }

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.n { w.buf = w.buf[len(w.buf)-w.n:] }
	return len(p), nil
}

func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" { return line }
	}
	return ""
}

// NOTE. stopPipeline kills the current yt-dlp/ffmpeg pair, if any, and blocks
// until feedPipeline has actually exited, so callers can safely start a new
// pipeline or tear down the synthetic right after it returns.
func stopPipeline() {
	pipelineMutex.Lock()
	cancel, done := pipelineCancel, pipelineDone
	pipelineCancel, pipelineDone = nil, nil
	pipelineMutex.Unlock()
	if cancel == nil { return }
	cancel() // kills both processes and closes their pipes, unblocking io.ReadFull in feedPipeline
	<-done
}
