package backend

import (
	"encoding/binary"
	"math"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/gen2brain/malgo"
	"github.com/hraban/opus"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// --- audio constants ---
// 48kHz mono, 20ms frames (960 samples/frame)
const (
	sampleRate       = 48000
	channels         = 1
	frameSizeSamples = sampleRate / 50 // 960 samples = 20ms
	frameDuration    = 20 * time.Millisecond
	maxOpusFrameSize = 4000 // bytes, generous upper bound for an encoded 20ms frame
)

var (
	// audio plumbing, all set up once in initAudio()
	audioCtx        *malgo.AllocatedContext
	captureDevice   *malgo.Device
	playbackDevice  *malgo.Device
	opusEncoder     *opus.Encoder
	localAudioTrack *webrtc.TrackLocalStaticSample

	// Mixer holds every currently-connected peer's jitter buffer and is what
	// the call screen's volume menu (components.RenderVolumeMenuBox) reads
	// gain from.
	Mixer = newAudioMixer()

	// only ever touched from the capture callback goroutine
	captureAccum []int16

	// noise cleanup, applied to the mic signal before it's framed/encoded.
	// Both are only ever touched from the capture callback goroutine.
	hpFilter	= newHighPassFilter(100) // cuts rumble/hum below ~100Hz
	gate		= newNoiseGate()

	// local voice activity detection, for the "you" row's speaking
	// indicator in the peers box - see onCaptureData.
	localVAD         = newVoiceActivityDetector()
	lastSelfSpeaking bool // last speaking state actually reported to the UI (see onCaptureData)
)

// ----------------------------------------------------------------------
// noise cleanup: one-pole high-pass filter + adaptive noise gate.
// Both run in-place on raw int16 PCM, right after capture and before
// framing/encoding.
// ----------------------------------------------------------------------

func clampFloat(v float64, lo float64, hi float64) float64 {
	if v < lo { return lo }
	if v > hi { return hi }
	return v
}

func newHighPassFilter(cutoffHz float64) *highPassFilter {
	rc := 1.0 / (2 * math.Pi * cutoffHz)
	dt := 1.0 / float64(sampleRate)
	return &highPassFilter{ alpha: rc / (rc + dt) }
}

func (f *highPassFilter) process(samples []int16) {
	for i, s := range samples {
		in := float64(s)
		out := f.alpha * (f.prevOut + in - f.prevIn)
		f.prevIn = in
		f.prevOut = out
		samples[i] = int16(clampFloat(out, -32768, 32767))
	}
}

func newNoiseGate() *noiseGate {
	return &noiseGate{
		gain: 		1,
		threshold: 	350, 	// ~ -39dBFS; raise if your room/mic is noisy, lower if it's clipping quiet speech
		floor: 		0.05, 	// -26dB floor instead of hard silence
		attack: 	0.85,
		release: 	0.995,
	}
}

func (g *noiseGate) process(samples []int16) {
	for i, s := range samples {
		abs := math.Abs(float64(s))
		if abs > g.envelope {
			g.envelope = g.attack*g.envelope + (1-g.attack)*abs
		} else {
			g.envelope = g.release*g.envelope + (1-g.release)*abs
		}
		target := 1.0
		if g.envelope < g.threshold { target = g.floor }
		g.gain = 0.99*g.gain + 0.01*target // smooth the gain itself too - avoid zipper noise
		samples[i] = int16(clampFloat(float64(s)*g.gain, -32768, 32767))
	}
}

// ----------------------------------------------------------------------
// audio: capture -> opus encode -> shared local track -> every peer connection
// remote tracks -> opus decode -> per-peer jitter buffer -> mixer -> playback
// ----------------------------------------------------------------------

const maxBufferedSamples = frameSizeSamples * 10 // ~200ms of slack per peer

// peer volume: linear gain multiplier applied per-peer in mixInto, adjusted
// from the call screen's volume menu (left/right). 1.0 is unity (the level
// the peer's audio arrives at); 0 mutes them locally without affecting what
// they send to anyone else.
const (
	PeerVolumeDefault = 1.0
	PeerVolumeMax      = 4.0 // +12dB above unity
	PeerVolumeStep     = 3.0 // dB per keypress, not a linear amount - see AdjustPeerGain
)

// peerVolumeMuteFloor is the linear gain a single "volume up" press lands on from fully muted
const peerVolumeMuteFloor = 0.05 // ~ -26dBFS: quiet but clearly audible

func dbToLinearGain(db float64) float64 { return math.Pow(10, db/20) }

func linearGainToDB(g float64) float64 {
	if g <= 0 { return math.Inf(-1) }
	return 20 * math.Log10(g)
}

func (b *peerAudioBuffer) setGain(g float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gain = g
}

func (b *peerAudioBuffer) getGain() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gain
}

// ----------------------------------------------------------------------
// peer audio normalization: a per-peer automatic-gain-control style
// compressor that pulls every peer's envelope toward a shared target peak
// (CompressorTargetDB), so a quiet talker gets boosted and a loud talker
// gets pulled down to roughly the same loudness before the mix. 
// Runs upstream of the peer's manual volume-menu gain above,
// which still works as a final per-peer trim on top of the normalized
// signal. A separate limiter (below) catches the final mixed output in
// case several normalized peers talking at once would otherwise clip.
// ----------------------------------------------------------------------

const (
	CompressorTargetDBDefault = -18.0 	// dBFS
	CompressorTargetMinDB     = -40.0
	CompressorTargetMaxDB     = 0.0
	CompressorTargetStep      = 3.0 	// dB per keypress in the audio settings view

	compressorMinGain       = 0.25  	// -12dB floor, so near-silence/room noise doesn't get boosted into audible hiss
	compressorMaxGain       = 4.0   	// +12dB ceiling, mirrors the manual peer-gain ceiling
	compressorAttack        = 0.6   	// envelope smoothing when the signal is rising (react fast, avoid overshoot)
	compressorRelease       = 0.995 	// envelope smoothing when falling (release slow, avoid pumping between words)
	compressorGainSmoothing = 0.98  	// extra smoothing on the gain itself, avoids zipper noise between frames
)

// compressorTargetDB is session-only, like selectedCapture/selectedPlayback
// above - it takes effect on the very next mixed frame for every peer.
var (
	compressorMu       sync.Mutex
	compressorTargetDB = CompressorTargetDBDefault
)

// NOTE. CompressorTargetDB returns the shared peer-normalization target, in dBFS.
func CompressorTargetDB() float64 {
	compressorMu.Lock()
	defer compressorMu.Unlock()
	return compressorTargetDB
}

// NOTE. AdjustCompressorTargetDB nudges the shared normalization target by
// deltaDB, clamped to [CompressorTargetMinDB, CompressorTargetMaxDB], and
// returns the resulting value.
func AdjustCompressorTargetDB(deltaDB float64) float64 {
	compressorMu.Lock()
	defer compressorMu.Unlock()
	compressorTargetDB = clampFloat(compressorTargetDB+deltaDB, CompressorTargetMinDB, CompressorTargetMaxDB)
	return compressorTargetDB
}

func dbfsToLinearAmplitude(db float64) float64 { return 32767 * dbToLinearGain(db) }

// NOTE. process pulls samples toward targetLinear: the envelope follower tracks
// how loud the peer actually is right now, and gain is whatever multiplier
// currently closes the gap - clamped so near-silence doesn't get boosted
// into hiss and a already-loud peer doesn't get pushed past a sane ceiling.
func (c *compressor) process(samples []int16, targetLinear float64) {
	if c.gain == 0 { c.gain = 1 } // first run, before any envelope has built up
	for i, s := range samples {
		abs := math.Abs(float64(s))
		if abs > c.envelope {
			c.envelope = compressorAttack*c.envelope + (1-compressorAttack)*abs
		} else {
			c.envelope = compressorRelease*c.envelope + (1-compressorRelease)*abs
		}
		target := 1.0
		if c.envelope > 1 { 
			target = clampFloat(targetLinear/c.envelope, compressorMinGain, compressorMaxGain)
		}
		c.gain = compressorGainSmoothing*c.gain + (1-compressorGainSmoothing)*target
		samples[i] = int16(clampFloat(float64(s)*c.gain, -32768, 32767))
	}
}

// masterLimiter is a lightweight peak limiter on the final mixed output,
// independent of the per-peer compressor above - it exists purely to catch
// the case where several already-normalized peers talk over each other and
// their sum would otherwise clip, not to shape any one peer's tone.
var masterLimiter = &limiterState{gain: 1}

const (
	limiterCeiling = 30000.0 // just under int16 max, leaves a little headroom before hard clipping
	limiterAttack  = 0.3     // react fast to a peak so it never audibly clips
	limiterRelease = 0.9995  // release slowly so gain reduction doesn't pump audibly
)

// NOTE. process writes in (the raw summed mix, which can exceed int16 range) into
// out as clamped int16 samples, pulling the whole mix down whenever the
// envelope threatens to exceed limiterCeiling and easing back up once it's
// clear.
func (l *limiterState) process(in []float64, out []int16) {
	for i, s := range in {
		abs := math.Abs(s)
		if abs > l.envelope {
			l.envelope = limiterAttack*l.envelope + (1-limiterAttack)*abs
		} else {
			l.envelope = limiterRelease*l.envelope + (1-limiterRelease)*abs
		}
		target := 1.0
		if l.envelope > limiterCeiling { target = limiterCeiling / l.envelope }
		if target < l.gain {
			l.gain = target // clamp down immediately
		} else {
			l.gain = 0.999*l.gain + 0.001*target // ease back up slowly
		}
		out[i] = int16(clampFloat(s*l.gain, -32768, 32767))
	}
}

// ----------------------------------------------------------------------

func (b *peerAudioBuffer) push(samples []int16) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.samples = append(b.samples, samples...)
	if len(b.samples) > maxBufferedSamples {
		b.samples = b.samples[len(b.samples)-maxBufferedSamples:]
	}
}

// NOTE. pull returns exactly n samples, zero-padding with silence if the peer
// hasn't sent enough audio yet, after running the actually-available
// portion through this peer's normalizing compressor.
func (b *peerAudioBuffer) pull(n int, targetLinear float64) []int16 {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]int16, n)
	if len(b.samples) == 0 { return out }
	take := len(b.samples)
	if take > n { take = n }
	copy(out, b.samples[:take])
	b.samples = b.samples[take:]
	b.comp.process(out[:take], targetLinear)
	return out
}

func newAudioMixer() *AudioMixer {
	return &AudioMixer{buffers: make(map[string]*peerAudioBuffer)}
}

func (mx *AudioMixer) addPeer(id string) *peerAudioBuffer {
	mx.mu.Lock()
	defer mx.mu.Unlock()
	buf := &peerAudioBuffer{gain: PeerVolumeDefault}
	mx.buffers[id] = buf
	return buf
}

func (mx *AudioMixer) removePeer(id string) {
	mx.mu.Lock()
	defer mx.mu.Unlock()
	delete(mx.buffers, id)
}

// NOTE. PeerGain returns id's current volume, or PeerVolumeDefault if id isn't a live peer
func (mx *AudioMixer) PeerGain(id string) float64 {
	mx.mu.Lock()
	buf, ok := mx.buffers[id]
	mx.mu.Unlock()
	if !ok { return PeerVolumeDefault }
	return buf.getGain()
}

// NOTE. AdjustPeerGain nudges id's volume by deltaDB decibels, clamped to
// [0, PeerVolumeMax], and returns the resulting linear gain.
func (mx *AudioMixer) AdjustPeerGain(id string, deltaDB float64) float64 {
	mx.mu.Lock()
	buf, ok := mx.buffers[id]
	mx.mu.Unlock()
	if !ok { return PeerVolumeDefault }

	current := buf.getGain()
	var next float64
	switch {
	case current <= 0 && deltaDB > 0:
		next = peerVolumeMuteFloor
	case current <= 0:
		next = 0
	default:
		next = dbToLinearGain(linearGainToDB(current) + deltaDB)
	}
	if deltaDB < 0 && next < peerVolumeMuteFloor { next = 0 }
	next = clampFloat(next, 0, PeerVolumeMax)

	buf.setGain(next)
	return next
}

// NOTE. mixInto sums every peer's next len(out) samples (each already pulled
// through its own normalizing compressor) into out, then runs the sum
// through masterLimiter instead of hard-clipping.
func (mx *AudioMixer) mixInto(out []int16) {
	mx.mu.Lock()
	bufs := make([]*peerAudioBuffer, 0, len(mx.buffers))
	for _, b := range mx.buffers { bufs = append(bufs, b) }
	mx.mu.Unlock()

	if len(bufs) == 0 {
		for i := range out { out[i] = 0 }
		return
	}

	targetLinear := dbfsToLinearAmplitude(CompressorTargetDB())
	acc := make([]float64, len(out))
	for _, b := range bufs {
		chunk := b.pull(len(out), targetLinear)
		gain := b.getGain()
		for i, s := range chunk { acc[i] += float64(s) * gain }
	}
	masterLimiter.process(acc, out)
}

func bytesToInt16(b []byte) []int16 {
	n := len(b) / 2
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		out[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return out
}

func int16ToBytes(samples []int16, out []byte) {
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
	}
}

func encodeAndSend(frame []int16) {
	if opusEncoder == nil || localAudioTrack == nil { return }
	buf := make([]byte, maxOpusFrameSize)
	n, err := opusEncoder.Encode(frame, buf)
	if err != nil { program.Send(ErrorMsg{Reason: "opus encode error: " + err.Error()}); return }
	if err := localAudioTrack.WriteSample(media.Sample{Data: buf[:n], Duration: frameDuration}); err != nil {
		program.Send(ErrorMsg{Reason: "track write error: " + err.Error()})
	}
}

func onCaptureData(pInput []byte, framecount uint32) {
	samples := bytesToInt16(pInput)
	hpFilter.process(samples)
	gate.process(samples)

	// speaking-but-muted still updates the VAD's envelope (so it reacts
	// instantly once unmuted) but never gets reported as "speaking" - the
	// indicator means "audio is actually going out".
	speaking, _ := localVAD.process(samples)
	effectiveSpeaking := speaking && !muted.Load()
	if effectiveSpeaking != lastSelfSpeaking {
		lastSelfSpeaking = effectiveSpeaking
		program.Send(SelfSpeakingMsg{Speaking: effectiveSpeaking})
	}

	captureAccum = append(captureAccum, samples...)
	for len(captureAccum) >= frameSizeSamples {
		frame := make([]int16, frameSizeSamples)
		copy(frame, captureAccum[:frameSizeSamples])
		captureAccum = captureAccum[frameSizeSamples:]
		if !muted.Load() { encodeAndSend(frame) }
	}
}

func onPlaybackData(pOutput []byte, framecount uint32) {
	out := make([]int16, int(framecount)*channels)
	Mixer.mixInto(out)
	int16ToBytes(out, pOutput)
}

// ----------------------------------------------------------------------
// device selection: lets the dashboard's audio settings screen pick a
// specific input/output device.
// ----------------------------------------------------------------------

var (
	// nil means "let malgo pick its system default"
	selectedCapture		*AudioDeviceOption
	selectedPlayback	*AudioDeviceOption
)

func SelectAudioDevice(kind AudioDeviceKind, option AudioDeviceOption) {
	switch kind {
	case AudioDeviceInput:
		selectedCapture = &option
	case AudioDeviceOutput:
		selectedPlayback = &option
	}
}

func IsSelectedAudioDevice(kind AudioDeviceKind, id malgo.DeviceID) bool {
	switch kind {
	case AudioDeviceInput:
		return selectedCapture != nil && selectedCapture.ID == id
	case AudioDeviceOutput:
		return selectedPlayback != nil && selectedPlayback.ID == id
	}
	return false
}

func AudioDeviceLabel(kind AudioDeviceKind) string {
	switch kind {
	case AudioDeviceInput:
		if selectedCapture != nil { return selectedCapture.Name }
	case AudioDeviceOutput:
		if selectedPlayback != nil { return selectedPlayback.Name }
	}
	return "system default"
}

func listAudioDevices(kind malgo.DeviceType) ([]AudioDeviceOption, error) {
	// init context
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(string) {})
	if err != nil { return nil, err }
	defer func() { ctx.Uninit(); ctx.Free() }()
	// get devices info
	infos, err := ctx.Devices(kind)
	if err != nil { return nil, err }
	// fill out audio device options
	out := make([]AudioDeviceOption, 0, len(infos))
	for _, info := range infos {
		name := info.Name()
		// PulseAudio/ALSA expose every playback device's loopback tap as a
		// second "Monitor of X" capture device (for recording desktop
		// audio) - real for the OS, but not something anyone picks as a
		// microphone, so it's just noise in this list.
		if kind == malgo.Capture && strings.HasPrefix(name, "Monitor of ") { continue }
		out = append(out, AudioDeviceOption{ID: info.ID, Name: name, IsDefault: info.IsDefault != 0})
	}
	return out, nil
}

func LoadAudioDevicesCmd() tea.Cmd {
	return func() tea.Msg {
		inputs, err := listAudioDevices(malgo.Capture)
		if err != nil { return ErrorMsg{Reason: "list input devices: " + err.Error()} }
		outputs, err := listAudioDevices(malgo.Playback)
		if err != nil { return ErrorMsg{Reason: "list output devices: " + err.Error()} }
		return AudioDevicesMsg{Inputs: inputs, Outputs: outputs}
	}
}

// ----------------------------------------------------------------------

func initAudio() error {
	localVAD = newVoiceActivityDetector()
	lastSelfSpeaking = false

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(string) {})
	if err != nil { return err }
	audioCtx = ctx

	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil { return err }
	opusEncoder = enc

	// Opus RTP codecs are conventionally signaled as channels=2 in SDP
	// even when the encoded audio itself is mono - the real channel count is
	// carried in the fmtp line ("stereo=0"), not the RTP channels field. This
	// must match pion's default-registered Opus codec exactly or Bind() will
	// fail to find a matching codec and refuse to start the track.
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   sampleRate,
			Channels:    2,
			SDPFmtpLine: "minptime=10;useinbandfec=1;stereo=0",
		},
		"audio", "voice",
	)
	if err != nil { return err }
	localAudioTrack = track

	captureConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	captureConfig.Capture.Format = malgo.FormatS16
	captureConfig.Capture.Channels = channels
	captureConfig.SampleRate = sampleRate
	captureConfig.PeriodSizeInFrames = frameSizeSamples
	if selectedCapture != nil { captureConfig.Capture.DeviceID = selectedCapture.ID.Pointer() }

	capture, err := malgo.InitDevice(ctx.Context, captureConfig, malgo.DeviceCallbacks{
		Data: func(pOutputSample, pInputSample []byte, framecount uint32) {
			onCaptureData(pInputSample, framecount)
		},
	})
	if err != nil { return err }
	captureDevice = capture
	if err := capture.Start(); err != nil { return err }

	playbackConfig := malgo.DefaultDeviceConfig(malgo.Playback)
	playbackConfig.Playback.Format = malgo.FormatS16
	playbackConfig.Playback.Channels = channels
	playbackConfig.SampleRate = sampleRate
	playbackConfig.PeriodSizeInFrames = frameSizeSamples
	if selectedPlayback != nil { playbackConfig.Playback.DeviceID = selectedPlayback.ID.Pointer() }

	playback, err := malgo.InitDevice(ctx.Context, playbackConfig, malgo.DeviceCallbacks{
		Data: func(pOutputSample, pInputSample []byte, framecount uint32) {
			onPlaybackData(pOutputSample, framecount)
		},
	})
	if err != nil { return err }
	playbackDevice = playback
	if err := playback.Start(); err != nil { return err }

	return nil
}

// NOTE. StopAudio tears down the capture/playback devices and audio context.
func StopAudio() {
	if captureDevice != nil { captureDevice.Uninit(); captureDevice = nil }
	if playbackDevice != nil { playbackDevice.Uninit(); playbackDevice = nil }
	if audioCtx != nil { audioCtx.Uninit(); audioCtx.Free(); audioCtx = nil }
	opusEncoder = nil
	localAudioTrack = nil
}

// ----------------------------------------------------------------------
// voice activity detection: flags which peer is currently talking, purely
// for the "who's speaking" highlight in the peers box.
// ----------------------------------------------------------------------

const (
	vadThreshold	= 400 						// linear int16 envelope above which a peer counts as "speaking"; ~ -38dBFS
	vadAttack		= 0.6 						// evelope smoothing when rising (open fast)
	vadRelease		= 0.97 						// envelope smoothing when falling (close slow)
	vadHangover		= 400 * time.Millisecond 	// keep "speaking" true this long past the loud frame, so it survives gaps between words
)

func newVoiceActivityDetector() *voiceActivityDetector {
	return &voiceActivityDetector{}
}

// NOTE. process folds one decoded frame into the envelope and reports whether the
// peer is speaking now, plus whether that's a change from the last call.
func (v *voiceActivityDetector) process(samples []int16) (speaking bool, changed bool) {
	for _, s := range samples {
		abs := math.Abs(float64(s))
		if abs > v.envelope {
			v.envelope = vadAttack*v.envelope + (1-vadAttack)*abs
		} else {
			v.envelope = vadRelease*v.envelope + (1-vadRelease)*abs
		}
	}
	now := time.Now()
	if v.envelope > vadThreshold { v.lastAbove = now }
	speaking = !v.lastAbove.IsZero() && now.Sub(v.lastAbove) < vadHangover
	changed = speaking != v.speaking
	v.speaking = speaking
	return speaking, changed
}

// ----------------------------------------------------------------------

// NOTE. readRemoteAudio decodes incoming Opus RTP packets from one peer and feeds
// the samples into that peer's jitter buffer for the mixer to consume.
// It also runs those same decoded frames through a VAD and pushes a
// peerSpeakingMsg to the UI whenever that peer starts or stops talking
func readRemoteAudio(peerID string, track *webrtc.TrackRemote) {
	decoder, err := opus.NewDecoder(sampleRate, channels)
	if err != nil { program.Send(ErrorMsg{Reason: "opus decoder error: " + err.Error()}); return }
	buf := Mixer.addPeer(peerID)
	applySavedPeerGain(peerID, buf)
	pcm := make([]int16, frameSizeSamples*6) // headroom in case a peer uses a larger frame size
	vad := newVoiceActivityDetector()

	for {
		packet, _, err := track.ReadRTP()
		if err != nil { return }
		n, err := decoder.Decode(packet.Payload, pcm)
		if err != nil { continue }
		samples := make([]int16, n)
		copy(samples, pcm[:n])
		buf.push(samples)
		if speaking, changed := vad.process(samples); changed {
			program.Send(PeerSpeakingMsg{ ID: peerID, Speaking: speaking })
		}
	}
}
