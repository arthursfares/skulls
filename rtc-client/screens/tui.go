package screens

import (
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"rtc-client/backend"
	"rtc-client/components"
)


const (
	stageAuth = iota
	stageDashboard
	stageCall
)

func NewModel() Model {
	return Model{
		stage:            stageAuth,
		auth:             newAuthModel(),
		dashboard:        newDashboardModel(),
		call:             newCallModel(),
		help:             newHelpModel(),
		historySettings:  backend.LoadHistorySettings(),
		inputDeviceList:  newAudioDeviceList(),
		outputDeviceList: newAudioDeviceList(),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, backend.RefreshTickCmd())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeChatArea()
		m.resizeDashboardList()
		m.resizeAudioDeviceLists()
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" { 
			m.quitting = true
			return m, tea.Quit
		}

		if m.commandPalette.active {
			return m.updateCommandPalette(msg)
		}

		if m.stage == stageCall && m.call.audioSettingsOpen {
			return m.updateCallAudioSettings(msg)
		}

		if m.stage == stageCall && m.call.inviteOpen {
			return m.updateCallInvitePrompt(msg)
		}

		if m.stage == stageDashboard && m.dashboard.mode == "browse" && m.dashboard.roomsList.SettingFilter() {
			var cmd tea.Cmd
			m.dashboard.roomsList, cmd = m.dashboard.roomsList.Update(msg)
			return m, cmd
		}

		switch msg.String() {

		case "ctrl+h":
			m.help.ShowAll = !m.help.ShowAll
			if m.stage == stageCall { m.resizeChatArea() }
			return m, nil

		case "esc":
			if m.stage == stageCall && m.call.volumeMenuOpen {
				m.call.volumeMenuOpen = false
				return m, nil
			}
			if m.stage == stageCall && m.call.membersMenuOpen {
				m.call.membersMenuOpen = false
				return m, nil
			}
			if m.stage == stageDashboard && m.dashboard.mode != "browse" {
				return m.cancelDashboardInput()
			}
			if m.stage == stageDashboard && m.dashboard.mode == "browse" && m.dashboard.roomsList.IsFiltered() {
				var cmd tea.Cmd
				m.dashboard.roomsList, cmd = m.dashboard.roomsList.Update(msg)
				return m, cmd
			}
			if m.stage == stageCall { return m, backend.LeaveRoomCmd() }
			m.quitting = true
			return m, tea.Quit

		case "ctrl+r":
			if m.stage == stageAuth { return m.toggleAuthMode() }

		case "tab":
			if m.stage == stageAuth { return m.toggleAuthFocus() }
			if m.stage == stageDashboard && m.dashboard.mode == "audioSettings" { return m.toggleAudioDeviceFocus() }

		case "shift+tab":
			if m.stage == stageAuth { return m.toggleAuthFocus() }
			if m.stage == stageDashboard && m.dashboard.mode == "audioSettings" { return m.toggleAudioDeviceFocus() }

		case "ctrl+o":
			if m.stage == stageDashboard && m.dashboard.mode == "browse" { return m.openCommandPalette(dashboardCommands(m)) }
			if m.stage == stageCall && !m.call.volumeMenuOpen && !m.call.membersMenuOpen { return m.openCommandPalette(callCommands(m)) }

		case "ctrl+m":
			if m.stage == stageCall { return m, backend.ToggleMuteCmd() }

		case "ctrl+up":
			if m.stage == stageCall && m.call.volumeMenuOpen && len(m.call.peers) > 0 { return m.volumeMenuUp() }
			if m.stage == stageCall && m.call.membersMenuOpen && m.membersMenuRowCount() > 0 { return m.membersMenuUp() }
			if m.stage == stageCall { return m.scrollChatUp() }

		case "ctrl+down":
			if m.stage == stageCall && m.call.volumeMenuOpen && len(m.call.peers) > 0 { return m.volumeMenuDown() }
			if m.stage == stageCall && m.call.membersMenuOpen && m.membersMenuRowCount() > 0 { return m.membersMenuDown() }
			if m.stage == stageCall { return m.scrollChatDown() }

		case "ctrl+left":
			if m.stage == stageCall && m.call.volumeMenuOpen && len(m.call.peers) > 0 { return m.adjustVolumeMenuGain(-backend.PeerVolumeStep) }

		case "ctrl+right":
			if m.stage == stageCall && m.call.volumeMenuOpen && len(m.call.peers) > 0 { return m.adjustVolumeMenuGain(backend.PeerVolumeStep) }

		case "left":
			if m.stage == stageDashboard && m.dashboard.mode == "audioSettings" && m.audioDeviceFocus == audioFocusNormalize {
				backend.AdjustCompressorTargetDB(-backend.CompressorTargetStep)
				return m, nil
			}

		case "right":
			if m.stage == stageDashboard && m.dashboard.mode == "audioSettings" && m.audioDeviceFocus == audioFocusNormalize {
				backend.AdjustCompressorTargetDB(backend.CompressorTargetStep)
				return m, nil
			}

		case "enter":
			return m.handleEnter()
		}

	case backend.RegisteredMsg:
		m = m.handleRegistered()

	case backend.LoggedInMsg:
		return m.handleLoggedIn(msg)

	case backend.RoomsMsg:
		return m.handleRooms(msg)

	case backend.RefreshTickMsg:
		if m.stage == stageDashboard {
			return m, tea.Batch(backend.FetchRoomsCmd(m.token), backend.RefreshTickCmd())
		}
		return m, backend.RefreshTickCmd()

	case backend.AudioDevicesMsg:
		return m.handleAudioDevices(msg)

	case backend.InviteSentMsg:
		m = m.handleInviteSent()

	case backend.WelcomeMsg:
		m = m.handleWelcome(msg)

	case backend.ConnectedMsg:
		m = m.handleConnected(msg)

	case backend.PeerJoinedMsg:
		m = m.handlePeerJoined(msg)

	case backend.PeerLeftMsg:
		m = m.handlePeerLeft(msg)

	case backend.PeerSpeakingMsg:
		m = m.handlePeerSpeaking(msg)

	case backend.SelfSpeakingMsg:
		m = m.handleSelfSpeaking(msg)

	case backend.MuteChangedMsg:
		m = m.handleMuteChanged(msg)

	case backend.ChatMsg:
		m = m.handleChat(msg)

	case backend.RollResultMsg:
		m = m.handleRollResult(msg)

	case backend.RoomMembersMsg:
		m = m.handleRoomMembers(msg)

	case backend.ImageMsg:
		return m.handleImage(msg)

	case backend.PlaybackStartedMsg:
		m = m.appendNotice("now playing: " + msg.Title)

	case backend.PlaybackStoppedMsg:
		m = m.appendNotice("stopped playback")

	case backend.LeftRoomMsg:
		return m.resetToDashboard(components.LogEntry{Kind: "system", Text: "left room", At: time.Now()})

	case backend.DisconnectedMsg:
		return m.resetToDashboard(components.LogEntry{Kind: "error", Text: msg.Reason, At: time.Now()})

	case backend.AccountDeletedMsg:
		return m.handleAccountDeleted()

	case backend.LogMsg:
		m.log = append(m.log, components.LogEntry{Kind: "system", Text: msg.Text, At: time.Now()})
		m.refreshChatLog()

	case backend.ErrorMsg:
		m.log = append(m.log, components.LogEntry{Kind: "error", Text: msg.Reason, At: time.Now()})
		m.refreshChatLog()
	}

	var cmd tea.Cmd
	switch {
	case m.stage == stageAuth:
		if m.auth.authFocus == 0 {
			m.auth.usernameInput, cmd = m.auth.usernameInput.Update(msg)
		} else {
			m.auth.passwordInput, cmd = m.auth.passwordInput.Update(msg)
		}
	case m.stage == stageDashboard && m.dashboard.mode == "browse":
		m.dashboard.roomsList, cmd = m.dashboard.roomsList.Update(msg)
	case m.stage == stageDashboard && m.dashboard.mode == "audioSettings":
		switch m.audioDeviceFocus {
		case audioFocusInput:
			m.inputDeviceList, cmd = m.inputDeviceList.Update(msg)
		case audioFocusOutput:
			m.outputDeviceList, cmd = m.outputDeviceList.Update(msg)
		}
	case m.stage == stageDashboard && m.dashboard.mode != "browse" && m.dashboard.mode != "confirmDelete":
		m.dashboard.input, cmd = m.dashboard.input.Update(msg)
	case m.stage == stageCall:
		m.call.chatInput, cmd = m.call.chatInput.Update(msg)
	}

	// Every bubblekitten.Model for an image sent/received via /image needs to
	// see every message: the async encode/AltScreen-sync/transmit-complete
	// messages driving its own display are unexported types this package
	// can't switch on, so instead of picking them out, just forward
	// everything and let each model's own id check discard the rest. 
	// Ready() flipping means its View() output changed, which the chat 
	// log needs to pick up.
	if len(m.call.chatImages) > 0 {
		imgCmds := make([]tea.Cmd, 0, len(m.call.chatImages)+1)
		changed := false
		for i := range m.call.chatImages {
			before := m.call.chatImages[i].Ready()
			var imgCmd tea.Cmd
			m.call.chatImages[i], imgCmd = m.call.chatImages[i].Update(msg)
			if imgCmd != nil { imgCmds = append(imgCmds, imgCmd) }
			if m.call.chatImages[i].Ready() != before { changed = true }
		}
		if changed { m.refreshChatLog() }
		if cmd != nil { imgCmds = append(imgCmds, cmd) }
		if len(imgCmds) > 0 { cmd = tea.Batch(imgCmds...) }
	}

	return m, cmd
}

func (m Model) handleEnter() (tea.Model, tea.Cmd) {
	switch m.stage {
	case stageAuth:			return m.handleAuthEnter()
	case stageDashboard: 	return m.handleDashboardEnter()
	case stageCall:			return m.handleCallEnter()
	}
	return m, nil
}

func (m Model) View() tea.View {
	if m.quitting { return tea.NewView("bye\n") }
	switch m.stage {
	case stageAuth: 		return m.renderAuthView()
	case stageDashboard:	return m.renderDashboardView()
	default:				return m.renderCallView()
	}
}
