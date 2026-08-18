package screens

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"rtc-client/backend"
	"rtc-client/components"
	"rtc-client/styles"
)


const dashboardListPageSize = 5
const dashboardListChromeHeight = 2
const dashboardListWidth = 44

const audioDeviceListPageSize = 4
const audioDeviceListChromeHeight = 1
const audioDeviceListWidth = 40

func newDashboardModel() dashboardModel {
	dashInput := textinput.New()
	dashInput.Prompt = styles.PromptStyle.Render("> ")
	dashInput.SetVirtualCursor(true)
	dashInput.CharLimit = 156
	dashInput.SetWidth(24)

	roomsList := list.New(nil, components.DashboardDelegate{}, 0, 0)
	roomsList.SetShowTitle(false)
	roomsList.SetShowStatusBar(false)
	roomsList.SetShowHelp(false)
	roomsList.SetFilteringEnabled(true)
	roomsList.DisableQuitKeybindings()

	return dashboardModel{ mode: "browse", input: dashInput, roomsList: roomsList }
}

func newAudioDeviceList() list.Model {
	l := list.New(nil, components.AudioDeviceDelegate{}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	return l
}

func (m *Model) resizeDashboardList() {
	width := dashboardListWidth
	if width > m.width-4 { width = m.width - 4 }
	if width < 1 { width = 1 }
	m.dashboard.roomsList.SetSize(width, dashboardListPageSize+dashboardListChromeHeight)
}

func (m *Model) resizeAudioDeviceLists() {
	width := audioDeviceListWidth
	if width > m.width-4 { width = m.width - 4 }
	if width < 1 { width = 1 }
	height := audioDeviceListPageSize + audioDeviceListChromeHeight
	m.inputDeviceList.SetSize(width, height)
	m.outputDeviceList.SetSize(width, height)
}

func (m Model) toggleAudioDeviceFocus() (tea.Model, tea.Cmd) {
	m.audioDeviceFocus = (m.audioDeviceFocus + 1) % audioFocusCount
	return m, nil
}

func (m Model) openAudioSettings() (tea.Model, tea.Cmd) {
	m.dashboard.mode = "audioSettings"
	m.audioDeviceFocus = audioFocusInput
	return m, backend.LoadAudioDevicesCmd()
}

// NOTE. startDeleteRoom is only called once the caller has confirmed the
// selected list item is a joinable room
func (m Model) startDeleteRoom(item components.DashboardItem) (tea.Model, tea.Cmd) {
	if item.Room.Owner != m.username {
		m.log = append(m.log, components.LogEntry{Kind: "error", Text: "only the room owner can delete it"})
		return m, nil
	}
	m.dashboard.deleteRoomID = item.Room.ID
	m.dashboard.mode = "confirmDelete"
	return m, nil
}

func (m Model) startCreateRoom() (tea.Model, tea.Cmd) {
	m.dashboard.mode = "creating"
	m.dashboard.input.Placeholder = "new room name"
	m.dashboard.input.SetValue("")
	cmd := m.dashboard.input.Focus()
	return m, cmd
}

// NOTE. startInviteUser is only called once the caller has confirmed the
// selected list item is a joinable room
func (m Model) startInviteUser(item components.DashboardItem) (tea.Model, tea.Cmd) {
	m.dashboard.inviteRoomID = item.Room.ID
	m.dashboard.mode = "inviting"
	m.dashboard.input.Placeholder = "invite username"
	m.dashboard.input.SetValue("")
	cmd := m.dashboard.input.Focus()
	return m, cmd
}

func (m Model) startDeleteAccount() (tea.Model, tea.Cmd) {
	m.dashboard.mode = "deletingAccount"
	m.dashboard.input.Placeholder = "confirm password to delete account"
	m.dashboard.input.SetValue("")
	m.dashboard.input.EchoMode = textinput.EchoPassword
	m.dashboard.input.EchoCharacter = '*'
	cmd := m.dashboard.input.Focus()
	return m, cmd
}

// NOTE. cancelDashboardInput backs out of whichever text-entry mode is active
// (creating/inviting/deletingAccount) back to browse, discarding the input.
func (m Model) cancelDashboardInput() (tea.Model, tea.Cmd) {
	m.dashboard.mode = "browse"
	m.dashboard.input.Blur()
	m.dashboard.input.SetValue("")
	m.dashboard.input.EchoMode = textinput.EchoNormal
	return m, nil
}

func (m Model) handleDashboardEnter() (tea.Model, tea.Cmd) {
	switch m.dashboard.mode {
	case "browse":
		item, ok := m.dashboard.roomsList.SelectedItem().(components.DashboardItem)
		if !ok { return m, nil }
		switch item.Kind {
		case components.DashboardItemRoom:
			m.call.roomName = item.Room.Name
			m.call.roomID = item.Room.ID
			m.log = nil // drop any log carried over from a previous room/visit before loading this room's own history
			if m.historySettings.Enabled {
				if entries, err := backend.LoadHistory(m.historySettings, m.username, item.Room.ID); err == nil && len(entries) > 0 {
					for _, e := range entries {
						m.log = append(m.log, components.LogEntry{Kind: e.Kind, Text: e.Text, At: e.At})
					}
				}
			}
			m.stage = stageCall
			focusCmd := m.call.chatInput.Focus()
			return m, tea.Batch(focusCmd, backend.ConnectCmd(m.token, item.Room.ID))
		case components.DashboardItemInvite:
			return m, backend.AcceptInviteCmd(m.token, item.Invite.RoomID)
		}
		return m, nil

	case "creating":
		name := strings.TrimSpace(m.dashboard.input.Value())
		if name == "" { return m, nil }
		m.dashboard.mode = "browse"
		m.dashboard.input.Blur()
		m.dashboard.input.SetValue("")
		return m, backend.CreateRoomCmd(m.token, name)

	case "inviting":
		target := strings.TrimSpace(m.dashboard.input.Value())
		if target == "" { return m, nil }
		roomID := m.dashboard.inviteRoomID
		m.dashboard.mode = "browse"
		m.dashboard.input.Blur()
		m.dashboard.input.SetValue("")
		return m, backend.InviteUserCmd(m.token, roomID, target)

	case "confirmDelete":
		roomId := m.dashboard.deleteRoomID
		m.dashboard.mode = "browse"
		m.dashboard.deleteRoomID = ""
		return m, backend.DeleteRoomCmd(m.token, roomId)

	case "audioSettings":
		if m.audioDeviceFocus == audioFocusNormalize { return m, nil }
		activeList := &m.inputDeviceList
		if m.audioDeviceFocus == audioFocusOutput { activeList = &m.outputDeviceList }
		if item, ok := activeList.SelectedItem().(components.AudioDeviceItem); ok {
			backend.SelectAudioDevice(item.Kind, item.Option)
		}
		return m, nil

	case "deletingAccount":
		password := m.dashboard.input.Value()
		m.dashboard.mode = "browse"
		m.dashboard.input.Blur()
		m.dashboard.input.SetValue("")
		m.dashboard.input.EchoMode = textinput.EchoNormal
		if password == "" { return m, nil }
		return m, backend.DeleteAccountCmd(m.token, password)
	}
	return m, nil
}

func (m Model) handleRooms(msg backend.RoomsMsg) (tea.Model, tea.Cmd) {
	m.dashboard.rooms = msg.Rooms
	m.dashboard.invites = msg.Invites
	cmd := m.dashboard.roomsList.SetItems(components.BuildDashboardItems(m.dashboard.rooms, m.dashboard.invites, m.username))
	return m, cmd
}

func (m Model) handleAudioDevices(msg backend.AudioDevicesMsg) (tea.Model, tea.Cmd) {
	inputCmd := m.inputDeviceList.SetItems(components.BuildAudioDeviceItems(backend.AudioDeviceInput, msg.Inputs))
	outputCmd := m.outputDeviceList.SetItems(components.BuildAudioDeviceItems(backend.AudioDeviceOutput, msg.Outputs))
	return m, tea.Batch(inputCmd, outputCmd)
}

func (m Model) handleInviteSent() Model {
	m.log = append(m.log, components.LogEntry{Kind: "system", Text: "invite sent"})
	return m
}

// NOTE. handleAccountDeleted resets straight back to a fresh NewModel (which
// defaults to the auth screen)
func (m Model) handleAccountDeleted() (tea.Model, tea.Cmd) {
	m2 := NewModel()
	m2.width = m.width
	m2.height = m.height
	m2.log = append(m2.log, components.LogEntry{Kind: "system", Text: "account deleted"})
	return m2, textinput.Blink
}

func (m Model) renderDashboardView() tea.View {
	var b strings.Builder
	b.WriteString(styles.RenderLogo())
	b.WriteString("\n\n")
	b.WriteString(styles.HeaderStyle.Render("welcome, " + m.username))
	b.WriteString("\n")
	b.WriteString(styles.SystemStyle.Render("mic: " + backend.AudioDeviceLabel(backend.AudioDeviceInput) + "  ·  speaker: " + backend.AudioDeviceLabel(backend.AudioDeviceOutput)))
	b.WriteString("\n")
	historyStatus := "off"
	if m.historySettings.Enabled { historyStatus = "on" }
	b.WriteString(styles.SystemStyle.Render("chat history: " + historyStatus))
	b.WriteString("\n\n")

	if m.dashboard.mode == "audioSettings" {
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
		bodyHeight := m.height - lipgloss.Height(footer)
		if bodyHeight < 1 { bodyHeight = 1 }
		body := styles.AppStyle.Width(m.width).Height(bodyHeight).Render(b.String())
		v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, body, footer))
		v.AltScreen = true
		return v
	}

	if len(m.dashboard.rooms) == 0 && len(m.dashboard.invites) == 0 {
		b.WriteString(styles.SystemStyle.Render("no rooms yet - press 'n' to create one"))
		b.WriteString("\n")
	} else {
		b.WriteString(m.dashboard.roomsList.View())
		b.WriteString("\n")
	}

	var km help.KeyMap
	switch m.dashboard.mode {
	case "creating":
		b.WriteString("\n")
		b.WriteString(styles.PromptStyle.Render("new room name: "))
		b.WriteString(m.dashboard.input.View())
		km = dashboardInputKeysFor(m.dashboard.mode)
	case "inviting":
		b.WriteString("\n")
		b.WriteString(styles.PromptStyle.Render("invite username: "))
		b.WriteString(m.dashboard.input.View())
		km = dashboardInputKeysFor(m.dashboard.mode)
	case "confirmDelete":
		name := ""
		for _, r := range m.dashboard.rooms {
			if r.ID == m.dashboard.deleteRoomID { name = r.Name; break }
		}
		b.WriteString("\n")
		b.WriteString(styles.ErrorStyle.Render("delete room \"" + name + "\"? this cannot be undone."))
		km = dashboardConfirmKeys
	case "deletingAccount":
		b.WriteString("\n")
		b.WriteString(styles.ErrorStyle.Render("type your password to permanently delete your account and all rooms you own:"))
		b.WriteString("\n")
		b.WriteString(styles.PromptStyle.Render("password: "))
		b.WriteString(m.dashboard.input.View())
		km = dashboardInputKeysFor(m.dashboard.mode)
	default:
		km = dashboardBrowseKeys
	}

	if len(m.log) > 0 {
		last := m.log[len(m.log)-1]
		if last.Kind == "error" {
			b.WriteString("\n")
			b.WriteString(styles.ErrorStyle.Render(last.Text))
		}
	}

	footer := components.RenderFooter(m.help, m.width, km)
	if m.commandPalette.active {
		footer = m.renderCommandPalette(m.width)
	}
	bodyHeight := m.height - lipgloss.Height(footer)
	if bodyHeight < 1 { bodyHeight = 1 }

	body := styles.AppStyle.Width(m.width).Height(bodyHeight).Render(b.String())
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, body, footer))
	v.AltScreen = true
	return v
}
