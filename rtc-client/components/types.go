package components

import (
	"time"

	"rtc-client/backend"
)

// LogEntry lets the call screen color-code each line by where it came from.
type LogEntry struct {
	Kind      string 					// system, error, me, peer, image, ...
	Text      string 					// for Kind == "image", the sender's display name ("me" for the local user) instead of message text
	ImageIdx  int    					// for Kind == "image" on a kitty-capable terminal, index into the images slice passed to RenderChatLog; -1 if unused
	ImagePlaceholder string 			// for Kind == "image" on a terminal that can't render it inline, the text to show instead (the /image caption, or a generic notice)
	At        time.Time
}

type PeersBoxData struct {
	Peers          []string
	PeerNames      map[string]string
	SpeakingPeers  map[string]bool
	SyntheticPeers map[string]bool 	// peerID -> true if it's a /play media source, not a real person
	Muted          bool
	SelfSpeaking   bool
	Username       string
}

type VolumeMenuData struct {
	Peers           []string
	PeerNames       map[string]string
	VolumeMenuIndex int
}

type MembersBoxData struct {
	Owner          string
	Members        []string
	PendingInvites []string        	// populated only when the viewer owns the room
	Connected      map[string]bool 	// username -> currently connected to the call
	SelectedIndex  int             	// index into Members+PendingInvites of the highlighted/scroll-anchor row
}

// --------------------------------------
// dashboard list
// --------------------------------------

// DashboardItemKind distinguishes between rooms the user can join and
// pending invites that they can accept.
type DashboardItemKind int

const (
	DashboardItemRoom DashboardItemKind = iota
	DashboardItemInvite
)

type DashboardItem struct {
	Kind   DashboardItemKind
	Room   backend.RoomSummary   	// set when Kind == DashboardItemRoom
	Invite backend.InviteSummary 	// set when Kind == DashboardItemInvite
	Mine   bool                  	// Room.Owner == the logged-in user
}

type DashboardDelegate struct{}

// --------------------------------------
// audio device list
// --------------------------------------

type AudioDeviceItem struct {
	Kind   backend.AudioDeviceKind
	Option backend.AudioDeviceOption
}

type AudioDeviceDelegate struct{}
