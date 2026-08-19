package screens

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	bubblekitten "github.com/arthursfares/bubblekitten"
	"rtc-client/backend"
	"rtc-client/components"
)

// Model is the top-level bubbletea model: the three screens (auth, dashboard,
// call) each keep their state in their own sub-model, switched on by stage.
type Model struct {
	stage int
	// --- per-screen state---
	auth      authModel
	dashboard dashboardModel
	call      callModel
	// --- session, once logged in - read by both dashboard and call screens ---
	token    string
	username string
	// --- local encrypted chat history ---
	historySettings backend.HistorySettings
	// --- footer help bar, shared across every screen ---
	help help.Model
	// --- ':' command palette, shared across every screen that supports it ---
	commandPalette commandPaletteModel
	// --- audio device pickers (mic/speaker), shared by the dashboard's and call screen's audio settings views ---
	inputDeviceList  list.Model
	outputDeviceList list.Model
	audioDeviceFocus int
	// --- metadata ---
	log                []components.LogEntry
	quitting           bool
	width              int
	height             int
	imageSupportWarned bool // whether already told the user their terminal can't render /image inline this session
}

// audioDeviceFocus values - which row of the audio settings view (shared by
// the dashboard and call screen) currently has focus.
const (
	audioFocusInput = iota
	audioFocusOutput
	audioFocusNormalize
	audioFocusCount 					// number of rows, for cycling with tab
)


type authModel struct {
	usernameInput 	textinput.Model
	passwordInput 	textinput.Model
	authFocus     	int    				// 0 = username, 1 = password
	authMode      	string 				// "login" or "register"
}

type dashboardModel struct {
	rooms          []backend.RoomSummary
	invites        []backend.InviteSummary
	roomsList      list.Model 			// invites + rooms
	mode           string     			// "browse", "creating", "inviting", "confirmDelete", "deletingAccount", "audioSettings"
	deleteRoomID   string     			// set while mode == "confirmDelete"
	input          textinput.Model
	inviteRoomID   string 				// set while mode == "inviting"
}

type callModel struct {
	roomName  		string
	roomID    		string
	peers     		[]string
	peerNames 		map[string]string 	// peerID -> display name
	speakingPeers	map[string]bool   	// peerID -> currently talking, per the audio-side VAD
	syntheticPeers	map[string]bool   	// peerID -> true if it's a /play media source, not a real person
	muted     		bool
	selfSpeaking	bool 				// local mic is currently above the VAD threshold and unmuted - see onCaptureData
	// --- peer volume menu ---
	volumeMenuOpen	bool
	volumeMenuIndex	int 				// index into peers of the currently-selected peer, while volumeMenuOpen
	// --- room members view ---
	membersMenuOpen	bool
	membersIndex	int 				// index into members+pendingInvites of the currently-highlighted row, while membersMenuOpen
	membersOwner	string
	members			[]string 			// every member of the room, connected or not - as opposed to peers, which is who's currently on the call
	pendingInvites	[]string 			// populated only when the requester is the room's owner
	// --- audio settings (mic/speaker pickers) - takes over the whole screen while open, reusing Model's shared device lists ---
	audioSettingsOpen bool
	// --- invite prompt - replaces the peers box while open ---
	inviteOpen  	bool
	inviteInput 	textinput.Model
	// --- chat (over webrtc data channel) ---
	chatInput     	textarea.Model
	chatViewport  	viewport.Model
	viewportReady 	bool
	chatImages    	[]bubblekitten.Model // images sent/received via /image, indexed by LogEntry.ImageIdx
}

// chatCommand is one entry in the chat "/" command registry: a usage string
// for explanation or error messages and the handler that runs when the command is typed.
type chatCommand struct {
	usage string
	run   func(m Model, args string) (Model, tea.Cmd)
}

// commandDef is one entry in the palette: a name you type (":name"), a
// one-line description shown alongside it, and the handler to run on
// selection.
type commandDef struct {
	Name string
	Desc string
	Run  func(m Model) (tea.Model, tea.Cmd)
}

// commandPaletteModel holds the transient state of an open prompt: what
// has been typed so far, the commands currently matching it, and which one
// is selected for enter/tab. Zero value is "closed".
type commandPaletteModel struct {
	active   bool
	input    textinput.Model
	commands []commandDef 				// full command set for whichever screen opened the palette
	filtered []commandDef
	selected int
}