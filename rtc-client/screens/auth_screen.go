package screens

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"rtc-client/backend"
	"rtc-client/components"
	"rtc-client/styles"
)


func newAuthModel() authModel {
	username := textinput.New()
	username.Prompt = styles.PromptStyle.Render("username> ")
	username.Placeholder = "your username"
	username.SetVirtualCursor(true)
	username.Focus()
	username.CharLimit = 64
	username.SetWidth(24)

	password := textinput.New()
	password.Prompt = styles.PromptStyle.Render("password> ")
	password.Placeholder = "your password"
	password.SetVirtualCursor(true)
	password.CharLimit = 128
	password.SetWidth(24)
	password.EchoMode = textinput.EchoPassword
	password.EchoCharacter = '*'

	return authModel{
		usernameInput: username,
		passwordInput: password,
		authMode:      "login",
	}
}

func (m Model) toggleAuthFocus() (tea.Model, tea.Cmd) {
	m.auth.authFocus = 1 - m.auth.authFocus
	if m.auth.authFocus == 0 { m.auth.passwordInput.Blur(); return m, m.auth.usernameInput.Focus() }
	m.auth.usernameInput.Blur(); return m, m.auth.passwordInput.Focus()
}

func (m Model) toggleAuthMode() (tea.Model, tea.Cmd) {
	if m.auth.authMode == "login" {
		m.auth.authMode = "register"
	} else {
		m.auth.authMode = "login"
	}
	return m, nil
}

func (m Model) handleAuthEnter() (tea.Model, tea.Cmd) {
	username := strings.TrimSpace(m.auth.usernameInput.Value())
	password := m.auth.passwordInput.Value()
	if username == "" || password == "" {
		m.log = append(m.log, components.LogEntry{Kind: "error", Text: "username and password are required"})
		return m, nil
	}
	if m.auth.authMode == "register" { return m, backend.RegisterCmd(username, password) }
	backend.SetHistoryKey(backend.DeriveHistoryKey(username, password))
	return m, backend.LoginCmd(username, password)
}

func (m Model) handleRegistered() Model {
	m.auth.authMode = "login"
	m.log = append(m.log, components.LogEntry{Kind: "system", Text: "registered - you can log in now"})
	return m
}

func (m Model) handleLoggedIn(msg backend.LoggedInMsg) (tea.Model, tea.Cmd) {
	m.token = msg.Token
	m.username = msg.Username
	backend.SetCurrentUsername(msg.Username)
	m.stage = stageDashboard
	m.auth.usernameInput.Blur()
	m.auth.passwordInput.Blur()
	m.log = []components.LogEntry{{Kind: "system", Text: "logged in as " + msg.Username}}
	return m, backend.FetchRoomsCmd(m.token)
}

func (m Model) renderAuthView() tea.View {
	title := "log in"
	if m.auth.authMode == "register" { title = "create an account" }

	content := styles.RenderLogo() + "\n\n" +
		styles.HeaderStyle.Render(title) + "\n\n" +
		m.auth.usernameInput.View() + "\n" +
		m.auth.passwordInput.View()

	if len(m.log) > 0 {
		last := m.log[len(m.log)-1]
		style := styles.SystemStyle
		if last.Kind == "error" { style = styles.ErrorStyle }
		content += "\n\n" + style.Render(last.Text)
	}

	footer := components.RenderFooter(m.help, m.width, authKeysFor(m))
	bodyHeight := m.height - lipgloss.Height(footer)
	if bodyHeight < 1 { bodyHeight = 1 }

	body := styles.AppStyle.Width(m.width).Height(bodyHeight).Render(content)
	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, body, footer))
	v.AltScreen = true
	return v
}
