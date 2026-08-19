package screens

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	bubblekitten "github.com/arthursfares/bubblekitten"
	"rtc-client/backend"
	"rtc-client/components"
	"rtc-client/styles"
)


// NOTE. removeStringFromSlice returns a new slice with the first occurence of target
// removed, preserving the relative order of every other element.
// If target isn't found, the original slice is returned unchanged.
func removeStringFromSlice(slice []string, target string) []string {
	for i, s := range slice {
		if s == target { return append(slice[:i], slice[i+1:]...) }
	}
	return slice
}

// NOTE. mainHeightFor returns the height left for the call screen's stack
// of boxes once the banner and the footer are subtracted.
func (m Model) mainHeightFor() int {
	headerHeight := lipgloss.Height(styles.RenderCallBanner(m.width, m.call.roomName))
	footerHeight := lipgloss.Height(m.callFooterArea())
	h := m.height - headerHeight - footerHeight
	if h < 1 { h = 1 }
	return h
}

func callFooterKeys(m Model) help.KeyMap {
	if m.call.volumeMenuOpen { return volumeMenuKeys }
	if m.call.membersMenuOpen { return membersMenuKeys }
	if m.call.inviteOpen { return invitePromptKeys }
	return callKeysFor(m)
}

// NOTE. callFooterArea is whatever occupies the call screen's bottom strip: the
// command palette while it's open (which doubles as the footer while
// active), the key-hint footer otherwise.
func (m Model) callFooterArea() string {
	if m.commandPalette.active { return m.renderCommandPalette(m.width) }
	return components.RenderFooter(m.help, m.width, callFooterKeys(m))
}

const peersBoxHeight = 7
const chatInputHeight = 3

func fullColumnSplit(mainHeight int) (chatHeight int, peersHeight int) {
	peersHeight = peersBoxHeight
	if peersHeight > mainHeight { peersHeight = mainHeight }
	chatHeight = mainHeight - peersHeight
	if chatHeight < 1 { chatHeight = 1 }
	return chatHeight, peersHeight
}

func newCallModel() callModel {
	chatInput := textarea.New()
	chatInput.Prompt = styles.PromptStyle.Render("|")
	chatInput.Placeholder = "type a message (or /help to see commands list)"
	chatInput.SetVirtualCursor(true)
	chatInput.CharLimit = 500
	chatInput.SetHeight(chatInputHeight)
	chatInput.ShowLineNumbers = false
	chatInput.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter"),
		key.WithHelp("shift+enter", "line break"),
	)
	chatInputStyles := chatInput.Styles()
	chatInputStyles.Focused.CursorLine = lipgloss.NewStyle()
	chatInput.SetStyles(chatInputStyles)

	inviteInput := textinput.New()
	inviteInput.Prompt = styles.PromptStyle.Render("> ")
	inviteInput.Placeholder = "invite username"
	inviteInput.SetVirtualCursor(true)
	inviteInput.CharLimit = 156
	inviteInput.SetWidth(24)

	return callModel{
		chatInput:      chatInput,
		inviteInput:    inviteInput,
		peerNames:      make(map[string]string),
		speakingPeers:  make(map[string]bool),
		syntheticPeers: make(map[string]bool),
	}
}

func (m *Model) resizeChatArea() {
	mainHeight := m.mainHeightFor()
	chatHeight, _ := fullColumnSplit(mainHeight)
	chatViewportWidth := m.width - 2
	chatViewportHeight := chatHeight - 4 - chatInputHeight // pane border(2) + "chat" header(1) + blank line(1) + input area(chatInputHeight)
	if chatViewportWidth < 1 { chatViewportWidth = 1 }
	if chatViewportHeight < 1 { chatViewportHeight = 1 }
	m.call.chatInput.SetWidth(chatViewportWidth)
	if !m.call.viewportReady {
		m.call.chatViewport = viewport.New(viewport.WithWidth(chatViewportWidth), viewport.WithHeight(chatViewportHeight))
		m.call.chatViewport.SoftWrap = true
		m.call.viewportReady = true
	} else {
		m.call.chatViewport.SetWidth(chatViewportWidth)
		m.call.chatViewport.SetHeight(chatViewportHeight)
	}
	m.call.chatViewport.SetContent(m.renderChatLog())
}

// --------------------------------------
// volume menu
// --------------------------------------

func (m Model) openVolumeMenu() (tea.Model, tea.Cmd) {
	m.call.volumeMenuOpen = !m.call.volumeMenuOpen
	if m.call.volumeMenuOpen {
		m.call.membersMenuOpen = false
		if m.call.volumeMenuIndex >= len(m.call.peers) { m.call.volumeMenuIndex = 0 }
	}
	return m, nil
}

func (m Model) volumeMenuUp() (tea.Model, tea.Cmd) {
	m.call.volumeMenuIndex--
	if m.call.volumeMenuIndex < 0 { m.call.volumeMenuIndex = len(m.call.peers) - 1 }
	return m, nil
}

func (m Model) volumeMenuDown() (tea.Model, tea.Cmd) {
	m.call.volumeMenuIndex = (m.call.volumeMenuIndex + 1) % len(m.call.peers)
	return m, nil
}

func (m Model) adjustVolumeMenuGain(delta float64) (tea.Model, tea.Cmd) {
	peerID := m.call.peers[m.call.volumeMenuIndex]
	gain := backend.Mixer.AdjustPeerGain(peerID, delta)
	backend.SavePeerVolume(m.username, m.call.roomID, m.call.peerNames[peerID], gain)
	return m, nil
}

// --------------------------------------
// audio settings
// --------------------------------------

func (m Model) openCallAudioSettings() (tea.Model, tea.Cmd) {
	m.call.audioSettingsOpen = true
	m.audioDeviceFocus = audioFocusInput
	m.call.chatInput.Blur()
	return m, backend.LoadAudioDevicesCmd()
}

func (m Model) updateCallAudioSettings(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.call.audioSettingsOpen = false
		return m, m.call.chatInput.Focus()

	case "tab", "shift+tab":
		return m.toggleAudioDeviceFocus()

	case "left":
		if m.audioDeviceFocus == audioFocusNormalize { backend.AdjustCompressorTargetDB(-backend.CompressorTargetStep) }
		return m, nil

	case "right":
		if m.audioDeviceFocus == audioFocusNormalize { backend.AdjustCompressorTargetDB(backend.CompressorTargetStep) }
		return m, nil

	case "enter":
		if m.audioDeviceFocus == audioFocusNormalize { return m, nil }
		activeList := &m.inputDeviceList
		if m.audioDeviceFocus == audioFocusOutput { activeList = &m.outputDeviceList }
		if item, ok := activeList.SelectedItem().(components.AudioDeviceItem); ok {
			backend.SelectAudioDevice(item.Kind, item.Option)
		}
		return m, nil
	}

	var cmd tea.Cmd
	switch m.audioDeviceFocus {
	case audioFocusInput:
		m.inputDeviceList, cmd = m.inputDeviceList.Update(msg)
	case audioFocusOutput:
		m.outputDeviceList, cmd = m.outputDeviceList.Update(msg)
	}
	return m, cmd
}

// --------------------------------------
// invite
// --------------------------------------

func (m Model) startCallInviteUser() (tea.Model, tea.Cmd) {
	m.call.inviteOpen = true
	m.call.inviteInput.SetValue("")
	m.call.chatInput.Blur()
	return m, m.call.inviteInput.Focus()
}

func (m Model) updateCallInvitePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.call.inviteOpen = false
		m.call.inviteInput.Blur()
		m.call.inviteInput.SetValue("")
		return m, m.call.chatInput.Focus()

	case "enter":
		target := strings.TrimSpace(m.call.inviteInput.Value())
		roomID := m.call.roomID
		m.call.inviteOpen = false
		m.call.inviteInput.Blur()
		m.call.inviteInput.SetValue("")
		focusCmd := m.call.chatInput.Focus()
		if target == "" { return m, focusCmd }
		return m, tea.Batch(focusCmd, backend.InviteUserCmd(m.token, roomID, target))
	}

	var cmd tea.Cmd
	m.call.inviteInput, cmd = m.call.inviteInput.Update(msg)
	return m, cmd
}

// --------------------------------------
// members
// --------------------------------------

func (m Model) membersMenuRowCount() int {
	return len(m.call.members) + len(m.call.pendingInvites)
}

func (m Model) membersMenuUp() (tea.Model, tea.Cmd) {
	m.call.membersIndex--
	if m.call.membersIndex < 0 { m.call.membersIndex = m.membersMenuRowCount() - 1 }
	return m, nil
}

func (m Model) membersMenuDown() (tea.Model, tea.Cmd) {
	m.call.membersIndex = (m.call.membersIndex + 1) % m.membersMenuRowCount()
	return m, nil
}



func (m Model) scrollChatUp() (tea.Model, tea.Cmd) {
	m.call.chatViewport.ScrollUp(1)
	return m, nil
}

func (m Model) scrollChatDown() (tea.Model, tea.Cmd) {
	m.call.chatViewport.ScrollDown(1)
	return m, nil
}

// NOTE. chatCommandAt returns the registered command whose name matches the
// leading "/word" of text, and the remainder of the line, if text looks
// like a recognized command.
func chatCommandAt(text string) (cmd chatCommand, args string, ok bool) {
	if !strings.HasPrefix(text, "/") { return chatCommand{}, "", false }
	name, rest, _ := strings.Cut(text[1:], " ")
	cmd, ok = chatCommands[name]
	return cmd, rest, ok
}

func (m Model) handleCallEnter() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.call.chatInput.Value())
	if text == "" { return m, nil }
	if cmd, args, ok := chatCommandAt(text); ok {
		m.call.chatInput.Reset()
		newM, cmdMsg := cmd.run(m, args)
		return newM, cmdMsg
	}
	backend.BroadcastToAllPeers(text)
	entry := components.LogEntry{Kind: "me", Text: text, At: time.Now()}
	m.log = append(m.log, entry)
	m.saveHistoryEntry(entry)
	if m.call.viewportReady {
		m.call.chatViewport.SetContent(m.renderChatLog())
		m.call.chatViewport.GotoBottom()
	}
	m.call.chatInput.Reset()
	return m, nil
}

func (m Model) saveHistoryEntry(e components.LogEntry) {
	switch e.Kind {
	case "me", "peer", "private-in", "private-out", "roll":
		backend.AppendHistoryEntry(
			m.historySettings,
			m.username,
			backend.HistoryEntry{RoomID: m.call.roomID, Kind: e.Kind, Text: e.Text, At: e.At},
		)
	}
}

// NOTE. renderChatLog gathers each active image's current bubblekitten
// rendering (which changes over time as its async transmit completes) and
// hands it to components.RenderChatLog alongside the log itself.
func (m Model) renderChatLog() string {
	images := make([]string, len(m.call.chatImages))
	for i := range m.call.chatImages {
		images[i] = m.call.chatImages[i].View()
	}
	return components.RenderChatLog(m.log, images)
}

func (m *Model) refreshChatLog() {
	if !m.call.viewportReady { return }
	m.call.chatViewport.SetContent(m.renderChatLog())
	m.call.chatViewport.GotoBottom()
}

func (m Model) handleWelcome(msg backend.WelcomeMsg) Model {
	for _, p := range msg.Peers {
		m.call.peers = append(m.call.peers, p.ID)
		m.call.peerNames[p.ID] = p.Name
		if p.Synthetic { m.call.syntheticPeers[p.ID] = true }
	}
	m.log = append(m.log, components.LogEntry{Kind: "system", Text: "connected as " + m.username})
	m.refreshChatLog()
	return m
}

func (m Model) handleConnected(msg backend.ConnectedMsg) Model {
	m.log = append(m.log, components.LogEntry{Kind: "system", Text: "connected to room " + m.call.roomName})
	m.refreshChatLog()
	return m
}

func (m Model) handlePeerJoined(msg backend.PeerJoinedMsg) Model {
	m.call.peers = append(m.call.peers, msg.ID)
	m.call.peerNames[msg.ID] = msg.Name
	if msg.Synthetic { m.call.syntheticPeers[msg.ID] = true }
	m.log = append(m.log, components.LogEntry{Kind: "system", Text: "peer joined: " + components.DisplayName(m.call.peerNames, msg.ID)})
	m.refreshChatLog()
	return m
}

func (m Model) handlePeerLeft(msg backend.PeerLeftMsg) Model {
	m.call.peers = removeStringFromSlice(m.call.peers, msg.ID)
	m.log = append(m.log, components.LogEntry{Kind: "system", Text: "peer left: " + components.DisplayName(m.call.peerNames, msg.ID)})
	delete(m.call.peerNames, msg.ID)
	delete(m.call.speakingPeers, msg.ID)
	delete(m.call.syntheticPeers, msg.ID)
	if m.call.volumeMenuIndex >= len(m.call.peers) {
		m.call.volumeMenuIndex = len(m.call.peers) - 1
		if m.call.volumeMenuIndex < 0 { m.call.volumeMenuIndex = 0 }
	}
	m.refreshChatLog()
	return m
}

func (m Model) handlePeerSpeaking(msg backend.PeerSpeakingMsg) Model {
	if msg.Speaking {
		m.call.speakingPeers[msg.ID] = true
	} else {
		delete(m.call.speakingPeers, msg.ID)
	}
	return m
}

func (m Model) handleSelfSpeaking(msg backend.SelfSpeakingMsg) Model {
	m.call.selfSpeaking = msg.Speaking
	return m
}

func (m Model) handleMuteChanged(msg backend.MuteChangedMsg) Model {
	m.call.muted = msg.Muted
	if m.call.muted {
		m.log = append(m.log, components.LogEntry{Kind: "system", Text: "you muted your mic"})
	} else {
		m.log = append(m.log, components.LogEntry{Kind: "system", Text: "you unmuted your mic"})
	}
	m.refreshChatLog()
	return m
}

func (m Model) handleChat(msg backend.ChatMsg) Model {
	kind := "peer"
	prefix := ""
	if msg.Private {
		kind = "private-in"
		prefix = "(private) "
	}
	entry := components.LogEntry{Kind: kind, Text: prefix + components.DisplayName(m.call.peerNames, msg.From) + ": " + msg.Text, At: time.Now()}
	m.log = append(m.log, entry)
	m.saveHistoryEntry(entry)
	if m.call.viewportReady {
		m.call.chatViewport.SetContent(m.renderChatLog())
		m.call.chatViewport.GotoBottom()
	}
	return m
}

// maxImageCols and maxImageRows bound how large a /image render gets in the
// chat log, in terminal cells - large enough to actually see, small enough
// that one image doesn't push everything else out of the scrollback.
const maxImageCols = 48
const maxImageRows = 24

// NOTE. imageDisplaySize picks a terminal-cell size for an image so it fits
// the chat viewport's width, preserving the source image's aspect ratio.
// Terminal cells are roughly twice as tall as they are wide, so rows are
// scaled by half relative to a naive pixel width/height ratio.
func imageDisplaySize(viewportWidth, imgW, imgH int) (cols, rows int) {
	cols = maxImageCols
	if viewportWidth > 0 && viewportWidth < cols { cols = viewportWidth }
	if cols < 1 { cols = 1 }
	if imgW <= 0 { imgW = 1 }
	if imgH <= 0 { imgH = 1 }
	rows = int(float64(cols) * float64(imgH) / float64(imgW) * 0.5)
	if rows < 1 { rows = 1 }
	if rows > maxImageRows { rows = maxImageRows }
	return cols, rows
}

// NOTE. handleImage appends a new chat log entry for an image sent (From ==
// "") or received (From == peer id) via /image. Terminal support is
// per-viewer (each client detects its own), so this is where sender and
// receiver diverge, not backend.LoadImageCmd:
//
//   - Kitty-capable: it creates the entry's bubblekitten.Model right away and
//     returns the commands that kick off its async encode/transmit - View
//     won't show anything for it until those land back through Update (see
//     the chatImages dispatch loop there) and flip its Ready() state.
//   - Not kitty-capable: no point encoding/transmitting bytes the terminal
//     will never draw, so instead of showing the image, the entry gets a
//     text placeholder: the caption sent alongside /image, if any, or a
//     generic notice otherwise. The first time this happens in a session, a
//     one-off warning is logged too.
func (m Model) handleImage(msg backend.ImageMsg) (tea.Model, tea.Cmd) {
	label := "me"
	if msg.From != "" { label = components.DisplayName(m.call.peerNames, msg.From) }

	img := bubblekitten.New()
	entry := components.LogEntry{Kind: "image", Text: label, ImageIdx: -1, At: time.Now()}

	if !img.Supported() {
		if !m.imageSupportWarned {
			m.imageSupportWarned = true
			m.log = append(m.log, components.LogEntry{
				Kind: "error",
				Text: "this terminal doesn't support the kitty graphics protocol - /image will show a text placeholder instead of displaying inline",
				At:   time.Now(),
			})
		}
		if msg.Caption != "" {
			entry.ImagePlaceholder = "(this terminal can't display images inline: " + msg.Caption + ")"
		} else {
			entry.ImagePlaceholder = "(this terminal can't display images inline)"
		}
		m.log = append(m.log, entry)
		m.refreshChatLog()
		return m, nil
	}

	var cmds []tea.Cmd
	var c tea.Cmd
	img, c = img.SetImage(msg.Image)
	cmds = append(cmds, c)
	img, c = img.SetAltScreen(true) // every screen in this app renders with AltScreen on
	cmds = append(cmds, c)
	b := msg.Image.Bounds()
	cols, rows := imageDisplaySize(m.call.chatViewport.Width(), b.Dx(), b.Dy())
	img, c = img.SetSize(cols, rows)
	cmds = append(cmds, c)

	m.call.chatImages = append(m.call.chatImages, img)
	entry.ImageIdx = len(m.call.chatImages) - 1
	m.log = append(m.log, entry)
	m.refreshChatLog()
	return m, tea.Batch(cmds...)
}

// NOTE. handleRoomMembers stores the server's response to a :members request and
// opens the members view in place of the peers box.
func (m Model) handleRoomMembers(msg backend.RoomMembersMsg) Model {
	m.call.membersMenuOpen = true
	m.call.membersIndex = 0
	m.call.volumeMenuOpen = false
	m.call.membersOwner = msg.Owner
	m.call.members = msg.Members
	m.call.pendingInvites = msg.PendingInvites
	return m
}

func (m Model) handleRollResult(msg backend.RollResultMsg) Model {
	text := fmt.Sprintf("%s rolled %s: %v = %d", msg.Roller, msg.Notation, msg.Results, msg.Total)
	entry := components.LogEntry{Kind: "roll", Text: text, At: time.Now()}
	m.log = append(m.log, entry)
	m.saveHistoryEntry(entry)
	m.refreshChatLog()
	return m
}

func (m Model) resetToDashboard(entry components.LogEntry) (tea.Model, tea.Cmd) {
	m.stage = stageDashboard
	m.call.roomName = ""
	m.call.roomID = ""
	m.call.peers = nil
	m.call.peerNames = make(map[string]string)
	m.call.speakingPeers = make(map[string]bool)
	m.call.syntheticPeers = make(map[string]bool)
	m.call.muted = false
	m.call.selfSpeaking = false
	m.call.volumeMenuOpen = false
	m.call.volumeMenuIndex = 0
	m.call.membersMenuOpen = false
	m.call.membersIndex = 0
	m.call.membersOwner = ""
	m.call.members = nil
	m.call.pendingInvites = nil
	m.call.audioSettingsOpen = false
	m.call.inviteOpen = false
	m.call.inviteInput.Blur()
	m.call.inviteInput.SetValue("")
	m.call.chatInput.Blur()
	m.call.chatInput.Reset()
	var closeCmds []tea.Cmd
	for _, img := range m.call.chatImages {
		if c := img.Close(); c != nil { closeCmds = append(closeCmds, c) }
	}
	m.call.chatImages = nil
	m.log = append(m.log, entry)
	return m, tea.Batch(append(closeCmds, backend.FetchRoomsCmd(m.token))...)
}

// NOTE. renderCallAudioSettingsView takes the call screen over entirely with the
// audio device pickers
func (m Model) renderCallAudioSettingsView() tea.View {
	banner := styles.RenderCallBanner(m.width, m.call.roomName)

	var b strings.Builder
	inputHeader := styles.HeaderStyle.Render("input (mic)")
	outputHeader := styles.HeaderStyle.Render("output (speaker)")
	switch m.audioDeviceFocus {
	case audioFocusInput:
		inputHeader = styles.CursorStyle.Render("> ") + inputHeader
	case audioFocusOutput:
		outputHeader = styles.CursorStyle.Render("> ") + outputHeader
	}
	b.WriteString(inputHeader)
	b.WriteString("\n")
	b.WriteString(m.inputDeviceList.View())
	b.WriteString("\n\n")
	b.WriteString(outputHeader)
	b.WriteString("\n")
	b.WriteString(m.outputDeviceList.View())
	b.WriteString("\n\n")
	b.WriteString(components.RenderCompressorTargetRow(m.audioDeviceFocus == audioFocusNormalize))
	b.WriteString("\n")

	footer := components.RenderFooter(m.help, m.width, audioSettingsKeys)
	bodyHeight := m.height - lipgloss.Height(banner) - lipgloss.Height(footer)
	if bodyHeight < 1 { bodyHeight = 1 }
	body := styles.AppStyle.Width(m.width).Height(bodyHeight).Render(b.String())

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, banner, body, footer))
	v.AltScreen = true
	return v
}

func (m Model) renderCallView() tea.View {
	if m.call.audioSettingsOpen { return m.renderCallAudioSettingsView() }

	banner := styles.RenderCallBanner(m.width, m.call.roomName)
	footer := m.callFooterArea()
	mainHeight := m.mainHeightFor()

	// Single full-width vertical stack: chat (flexible) on top, then
	// peers (fixed height) at the bottom.
	chatHeight, peersHeight := fullColumnSplit(mainHeight)
	menuOpen := m.call.volumeMenuOpen || m.call.membersMenuOpen || m.call.inviteOpen
	chatBox := components.RenderChatBox(m.call.chatViewport, m.call.chatInput, menuOpen, m.width, chatHeight)
	peersBox := components.RenderPeersBox(components.PeersBoxData{
		Peers:          m.call.peers,
		PeerNames:      m.call.peerNames,
		SpeakingPeers:  m.call.speakingPeers,
		SyntheticPeers: m.call.syntheticPeers,
		Muted:          m.call.muted,
		SelfSpeaking:   m.call.selfSpeaking,
		Username:       m.username,
	}, m.width, peersHeight)
	switch {
	case m.call.volumeMenuOpen:
		peersBox = components.RenderVolumeMenuBox(components.VolumeMenuData{
			Peers:           m.call.peers,
			PeerNames:       m.call.peerNames,
			VolumeMenuIndex: m.call.volumeMenuIndex,
		}, m.width, peersHeight)
	case m.call.membersMenuOpen:
		connected := make(map[string]bool, len(m.call.peerNames)+1)
		connected[m.username] = true
		for _, name := range m.call.peerNames { connected[name] = true }
		peersBox = components.RenderMembersBox(components.MembersBoxData{
			Owner:          m.call.membersOwner,
			Members:        m.call.members,
			PendingInvites: m.call.pendingInvites,
			Connected:      connected,
			SelectedIndex:  m.call.membersIndex,
		}, m.width, peersHeight)
	case m.call.inviteOpen:
		peersBox = components.RenderInvitePromptBox(m.call.roomName, m.call.inviteInput.View(), m.width, peersHeight)
	}

	full := lipgloss.JoinVertical(lipgloss.Left, banner, chatBox, peersBox, footer)

	v := tea.NewView(full)
	v.AltScreen = true
	return v
}
