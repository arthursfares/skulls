package screens

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"rtc-client/backend"
	"rtc-client/components"
	"rtc-client/styles"
)


func newCommandPaletteInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = styles.PromptStyle.Render("")
	ti.SetVirtualCursor(true)
	ti.CharLimit = 64
	ti.SetWidth(40)
	return ti
}

// NOTE. dashboardCommands lists the actions available from the dashboard's browse mode.
// TODO. Invite/delete-room only make sense with a room selected. Add room name to invite/delete 
// instead of getting the selected one.
func dashboardCommands(m Model) []commandDef {
	return []commandDef{
		{
			Name: "new-room",
			Desc: "create a new room",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				return m.closePalette().startCreateRoom()
			},
		},
		{
			Name: "invite",
			Desc: "invite a user to the selected room",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.closePalette()
				if item, ok := m.dashboard.roomsList.SelectedItem().(components.DashboardItem); ok && item.Kind == components.DashboardItemRoom {
					return m.startInviteUser(item)
				}
				return m, nil
			},
		},
		{
			Name: "delete-room",
			Desc: "delete the selected room (owner only)",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.closePalette()
				if item, ok := m.dashboard.roomsList.SelectedItem().(components.DashboardItem); ok && item.Kind == components.DashboardItemRoom {
					return m.startDeleteRoom(item)
				}
				return m, nil
			},
		},
		{
			Name: "audio-config",
			Desc: "open audio input/output device settings",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				return m.closePalette().openAudioSettings()
			},
		},
		{
			Name: "history-enable",
			Desc: "save encrypted chat history locally and restore it on rejoin",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.closePalette()
				m.historySettings.Enabled = true
				backend.SaveHistorySettings(m.historySettings)
				return m.appendNotice("chat history saving enabled (" + backend.HistoryDir() + ")"), nil
			},
		},
		{
			Name: "history-disable",
			Desc: "stop saving chat history locally",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.closePalette()
				m.historySettings.Enabled = false
				backend.SaveHistorySettings(m.historySettings)
				return m.appendNotice("chat history saving disabled"), nil
			},
		},
		{
			Name: "delete-account",
			Desc: "permanently delete your account and all rooms you own",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				return m.closePalette().startDeleteAccount()
			},
		},
		{
			Name: "refresh",
			Desc: "refresh the room list",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.closePalette()
				return m, backend.FetchRoomsCmd(m.token)
			},
		},
		{
			Name: "quit",
			Desc: "quit the app",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.closePalette()
				m.quitting = true
				return m, tea.Quit
			},
		},
	}
}

// NOTE. callCommands lists the actions available during a call.
func callCommands(m Model) []commandDef {
	return []commandDef{
		{
			Name: "peer-volume",
			Desc: "open the peer volume menu",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m, focusCmd := m.closePaletteAndRefocusChat()
				model, cmd := m.openVolumeMenu()
				return model, tea.Batch(focusCmd, cmd)
			},
		},
		{
			Name: "members",
			Desc: "list all room members, and pending invites if you own the room",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m, focusCmd := m.closePaletteAndRefocusChat()
				return m, tea.Batch(focusCmd, backend.RoomMembersCmd(m.token, m.call.roomID))
			},
		},
		{
			Name: "invite",
			Desc: "invite a user to this room",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				return m.closePalette().startCallInviteUser()
			},
		},
		{
			Name: "audio-config",
			Desc: "open audio input/output device settings",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				return m.closePalette().openCallAudioSettings()
			},
		},
		{
			Name: "quit",
			Desc: "leave the room",
			Run: func(m Model) (tea.Model, tea.Cmd) {
				m = m.closePalette()
				return m, backend.LeaveRoomCmd()
			},
		},
	}
}

// NOTE. openCommandPalette opens the palette with cmds as its full command set.
func (m Model) openCommandPalette(cmds []commandDef) (tea.Model, tea.Cmd) {
	ti := newCommandPaletteInput()
	focusCmd := ti.Focus()
	m.commandPalette = commandPaletteModel{
		active:   true,
		input:    ti,
		commands: cmds,
		filtered: cmds,
	}
	if m.stage == stageCall {
		m.call.chatInput.Blur()
		m.resizeChatArea()
	}
	return m, focusCmd
}

// NOTE. closePalette clears the palette state without touching anything else -
// callers that need to restore focus (the call screen's chat box) use
// closePaletteAndRefocusChat instead.
func (m Model) closePalette() Model {
	m.commandPalette = commandPaletteModel{}
	return m
}

func (m Model) closePaletteAndRefocusChat() (Model, tea.Cmd) {
	m = m.closePalette()
	cmd := m.call.chatInput.Focus()
	m.resizeChatArea()
	return m, cmd
}

// NOTE. updateCommandPalette handles a keypress while the palette is open
func (m Model) updateCommandPalette(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.stage == stageCall {
			m, cmd := m.closePaletteAndRefocusChat()
			return m, cmd
		}
		return m.closePalette(), nil

	case "enter":
		if len(m.commandPalette.filtered) == 0 { return m, nil }
		return m.commandPalette.filtered[m.commandPalette.selected].Run(m)

	case "up":
		n := len(m.commandPalette.filtered)
		if n == 0 { return m, nil }
		m.commandPalette.selected = (m.commandPalette.selected - 1 + n) % n
		return m, nil

	case "down":
		n := len(m.commandPalette.filtered)
		if n == 0 { return m, nil }
		m.commandPalette.selected = (m.commandPalette.selected + 1) % n
		return m, nil

	case "tab":
		if len(m.commandPalette.filtered) > 0 {
			m.commandPalette.input.SetValue(m.commandPalette.filtered[m.commandPalette.selected].Name)
			m.commandPalette.input.CursorEnd()
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.commandPalette.input, cmd = m.commandPalette.input.Update(msg)
	m.filterCommandPalette()
	return m, cmd
}

// NOTE. filterCommandPalette re-filters commands against the current input value
func (m *Model) filterCommandPalette() {
	query := strings.ToLower(strings.TrimSpace(m.commandPalette.input.Value()))
	if query == "" {
		m.commandPalette.filtered = m.commandPalette.commands
		m.commandPalette.selected = 0
		return
	}

	var prefixMatches, otherMatches []commandDef
	for _, c := range m.commandPalette.commands {
		name := strings.ToLower(c.Name)
		switch {
		case strings.HasPrefix(name, query):
			prefixMatches = append(prefixMatches, c)
		case strings.Contains(name, query), strings.Contains(strings.ToLower(c.Desc), query):
			otherMatches = append(otherMatches, c)
		}
	}
	m.commandPalette.filtered = append(prefixMatches, otherMatches...)
	m.commandPalette.selected = 0
}

// NOTE. renderCommandPalette draws the prompt and its live-filtered command list
func (m Model) renderCommandPalette(width int) string {
	var b strings.Builder
	b.WriteString(m.commandPalette.input.View())
	b.WriteString("\n")

	if len(m.commandPalette.filtered) == 0 {
		b.WriteString(styles.SystemStyle.Render("no matching commands"))
		b.WriteString("\n")
	} else {
		for i, c := range m.commandPalette.filtered {
			name := c.Name
			if i == m.commandPalette.selected {
				b.WriteString(styles.CursorStyle.Render("> " + name))
			} else {
				b.WriteString("  ");b.WriteString(styles.PeerStyle.Render(name))
			}
			b.WriteString("  ")
			b.WriteString(styles.SystemStyle.Render(c.Desc))
			b.WriteString("\n")
		}
	}

	height := len(m.commandPalette.filtered) + 1 // +1 for the prompt line
	if height < 2 { height = 2 }
	return components.PaneStyle(true, width, height).Render(strings.TrimRight(b.String(), "\n"))
}
