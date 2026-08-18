package backend

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gen2brain/malgo"
	"github.com/pion/webrtc/v4"
)

// --------------------------------------
// accounts client http api types
// --------------------------------------

type RoomSummary struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Owner string `json:"owner"`
}

type InviteSummary struct {
	RoomID   string `json:"roomID"`
	RoomName string `json:"roomName"`
	From     string `json:"from"`
}

type roomsListResponse struct {
	Rooms		[]RoomSummary		`json:"rooms"`
	Invites		[]InviteSummary		`json:"invites"`
}

type RoomMembersResponse struct {
	Owner          string   `json:"owner"`
	Members        []string `json:"members"`
	PendingInvites []string `json:"pendingInvites,omitempty"`
}

// --------------------------------------
// signaling server client types 
// --------------------------------------

type SignalMessage struct {
	Type string          `json:"type"`
	From string          `json:"from,omitempty"` 	// signaling server fills this in
	To   string          `json:"to,omitempty"`
	As   string          `json:"as,omitempty"` 		// sign this message as a synthetic identity we own, instead of our real ID
	Data json.RawMessage `json:"data,omitempty"`
}

type PeerInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Synthetic bool   `json:"synthetic,omitempty"` 	// true for a /play media source, not a real person
}

type Peer struct {
	peerConnection *webrtc.PeerConnection
	dataChannel    *webrtc.DataChannel 				// chat; nil until it opens (or until the offerer creates it)
}

// --------------------------------------
// audio 
// --------------------------------------

type peerAudioBuffer struct {
	mu      sync.Mutex
	samples []int16
	gain    float64    								// linear volume multiplier for this peer, adjusted from the call screen's volume menu
	comp    compressor 								// per-peer normalizer, applied upstream of gain - see mixInto in audio.go
}

// compressor is a per-peer automatic-gain-control style normalizer that
// pulls a peer's envelope toward a shared target peak - see the block
// comment above the "peer audio normalization" section in audio.go.
type compressor struct {
	envelope float64
	gain     float64
}

// limiterState is a lightweight peak limiter applied to the final mixed
// output, independent of the per-peer compressor above - see masterLimiter
// in audio.go.
type limiterState struct {
	envelope float64
	gain     float64
}

type AudioMixer struct {
	mu      sync.Mutex
	buffers map[string]*peerAudioBuffer
}

// highPassFilter is a simple one-pole RC high-pass. At 100HZ it removes
// AC hum, desk/table thumps, and mic handling runble without touching the
// voice band, so it makes everything downstream (including the noise gate's
// envelope) work off a cleaner signal
type highPassFilter struct {
	prevIn  float64
	prevOut float64
	alpha   float64
}

// noiseGate attenuates the signal when its smoothed envelope drops below
// threshold (i.e. between words / during silence), instead of passing raw
// room noise/hiss straight to the encoder. Gain moves slowly (attack/release
// on the envelope, plus its own smoothing) so it fades rather than clicks,
// and never goes fully silent so it doesn't sound like it's cutting out.
type noiseGate struct {
	envelope  float64
	gain      float64
	threshold float64 // linear int 16 amplitude below which the gate starts closing
	floor     float64 // gain applied when fully closed (not 0, to avoid hard mute artifacts)
	attack    float64 // envelope smoothing when signal is rising (open fast)
	release   float64 // envelope smoothing when signal is falling (closes slow)
}

// voiceActivityDetector flags whether a remote peer is currently talking,
// using the same attack/release envelope technique as noiseGate but applied
// to decoded remote audio purely to drive the "who's speaking" UI - it never
// touches the samples themselves. hangover keeps "speaking" true for a
// short window after the envelope dips below threshold, so the indicator
// doesn't flicker off between syllables or short pauses.
type voiceActivityDetector struct {
	envelope  float64
	speaking  bool
	lastAbove time.Time
}

// AudioDeviceOption is the UI-facing view of a malgo.DeviceInfo
type AudioDeviceOption struct {
	ID        malgo.DeviceID
	Name      string
	IsDefault bool
}

// AudioDeviceKind distinguishes which of the two picker lists an item
// belongs to (input vs output).
type AudioDeviceKind int

const (
	AudioDeviceInput AudioDeviceKind = iota
	AudioDeviceOutput
)

// --------------------------------------
// Messages sent into screens.Model.Update() 
// via Program.Send(...).
// --------------------------------------

type RegisteredMsg 			struct{}
type LoggedInMsg 			struct{ Token, Username string }
type RoomsMsg 				struct{ Rooms []RoomSummary; Invites []InviteSummary }
type RefreshTickMsg 		struct{}
type InviteSentMsg 			struct{}
type ConnectedMsg 			struct{}
type WelcomeMsg 			struct{ ID string; Peers []PeerInfo }
type PeerJoinedMsg 			struct{ ID, Name string; Synthetic bool }
type PeerLeftMsg 			struct{ ID string }
type PeerSpeakingMsg 		struct{ ID string; Speaking bool }
type SelfSpeakingMsg 		struct{ Speaking bool }
type ChatMsg 				struct{ From, Text string; Private bool }
type RollResultMsg 			struct{ Roller, Notation string; Results []int; Total int }
type PlaybackStartedMsg 	struct{ Title string }
type PlaybackStoppedMsg 	struct{}
type ErrorMsg 				struct{ Reason string }
type LogMsg 				struct{ Text string }
type MuteChangedMsg 		struct{ Muted bool }
type LeftRoomMsg 			struct{}
type DisconnectedMsg 		struct{ Reason string }
type AccountDeletedMsg 		struct{}
type AudioDevicesMsg 		struct{ Inputs []AudioDeviceOption; Outputs []AudioDeviceOption }
type RoomMembersMsg 		struct{ Owner string; Members []string; PendingInvites []string }

// --------------------------------------
// synthetic peer
// --------------------------------------

type syntheticWelcome struct {
	id    string
	peers []PeerInfo
}

// --------------------------------------
// data channel
// --------------------------------------

// dataChannelMsg is the wire format sent over each peer's chat data
// channel. Kind distinguishes a broadcast chat line from a /private one so
// the receiving end can render it differently, even though both travel over
// the same channel.
type dataChannelMsg struct {
	Kind string `json:"kind"` // "chat" | "private"
	Text string `json:"text"`
}

// --------------------------------------
// volume control
// --------------------------------------

// PeerVolumes is the locally-persisted record of each peer's volume-menu
// gain, keyed by room ID then by the peer's username - not their
// connection-scoped peer ID, which is a fresh UUID every time they
// reconnect. Reopening a room this way automatically restores the levels
// already dialed in for the people in it.
type PeerVolumes map[string]map[string]float64

// --------------------------------------
// media playback
// --------------------------------------

// tailWriter keeps only the last n bytes written to it, so a subprocess's
// final diagnostic output can be surfaced without holding its entire
// (potentially large) stderr in memory.
type tailWriter struct {
	mu  sync.Mutex
	buf []byte
	n   int
}

// --------------------------------------
// chat history
// --------------------------------------

type HistorySettings struct {
	Enabled bool 	`json:"enabled"`
}

type HistoryEntry struct {
	RoomID string    `json:"roomId"`
	Kind   string    `json:"kind"`
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
}