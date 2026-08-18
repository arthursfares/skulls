package screens

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"

	"rtc-client/styles"
)

// This file defines a separate keyMap per screen/mode, each implementing
// help.KeyMap (ShortHelp/FullHelp). They exist purely to drive the footer's
// help text - actual key dispatch still lives in tui.go's big switch on
// msg.String().

// helpToggleBinding is shared by every keymap so ctrl+h always expands or
// collapses the footer's full multi-column command list.
var helpToggleBinding = key.NewBinding(key.WithKeys("ctrl+h"), key.WithHelp("ctrl+h", "toggle help"))

func newHelpModel() help.Model {
	h := help.New()
	h.Styles.ShortKey = styles.FooterKeyStyle
	h.Styles.ShortDesc = styles.FooterDescStyle
	h.Styles.ShortSeparator = styles.FooterSepStyle
	h.Styles.Ellipsis = styles.FooterSepStyle
	h.Styles.FullKey = styles.FooterKeyStyle
	h.Styles.FullDesc = styles.FooterDescStyle
	h.Styles.FullSeparator = styles.FooterSepStyle
	return h
}

// --------------------------------------
// auth screen
// --------------------------------------

type authKeyMap struct {
	Navigate	key.Binding
	Toggle		key.Binding
	Enter		key.Binding
	Help		key.Binding
	Quit		key.Binding
}

func (k authKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Enter, k.Toggle, k.Navigate, k.Quit}
}

func (k authKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Enter, k.Navigate}, {k.Toggle, k.Help}, {k.Quit}}
}

func authKeysFor(m Model) authKeyMap {
	enterHelp := "log in"
	toggleHelp := "switch to register"
	if m.auth.authMode == "register" {
		enterHelp = "register"
		toggleHelp = "switch to log in"
	}
	return authKeyMap{
		Navigate:	key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "switch field")),
		Toggle:		key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", toggleHelp)),
		Enter:		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", enterHelp)),
		Help:		helpToggleBinding,
		Quit:		key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "quit")),
	}
}

// --------------------------------------
// dashboard: browse mode
// --------------------------------------

type dashboardBrowseKeyMap struct {
	Navigate	key.Binding
	Page		key.Binding
	Filter		key.Binding
	Enter		key.Binding
	Command		key.Binding
	Help		key.Binding
}

func (k dashboardBrowseKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Enter, k.Filter, k.Command}
}

func (k dashboardBrowseKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Navigate, k.Page}, {k.Filter, k.Enter}, {k.Command, k.Help}}
}

var dashboardBrowseKeys = dashboardBrowseKeyMap{
	Navigate:	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "navigate")),
	Page:		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "page")),
	Filter:		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Enter:		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "join / accept")),
	Command:	key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "commands")),
	Help:		helpToggleBinding,
}

// --------------------------------------
// dashboard: audio settings mode
// --------------------------------------

type audioSettingsKeyMap struct {
	Navigate	key.Binding
	Switch		key.Binding
	Select		key.Binding
	Adjust		key.Binding
	Cancel		key.Binding
	Help		key.Binding
}

func (k audioSettingsKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Switch, k.Navigate, k.Adjust, k.Cancel}
}

func (k audioSettingsKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Navigate, k.Switch}, {k.Select, k.Adjust}, {k.Cancel, k.Help}}
}

var audioSettingsKeys = audioSettingsKeyMap{
	Navigate:	key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "navigate")),
	Switch:		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch row")),
	Select:		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select device")),
	Adjust:		key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "adjust normalize target")),
	Cancel:		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	Help:		helpToggleBinding,
}

// --------------------------------------
// dashboard: text-entry modes (creating / inviting / deletingAccount)
// --------------------------------------

type dashboardInputKeyMap struct {
	Confirm	key.Binding
	Cancel	key.Binding
	Help	key.Binding
}

func (k dashboardInputKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Confirm, k.Cancel}
}

func (k dashboardInputKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Confirm, k.Cancel}, {k.Help}}
}

func dashboardInputKeysFor(mode string) dashboardInputKeyMap {
	confirmHelp := "confirm"
	switch mode {
	case "creating":
		confirmHelp = "create room"
	case "inviting":
		confirmHelp = "send invite"
	case "deletingAccount":
		confirmHelp = "delete account"
	}
	return dashboardInputKeyMap{
		Confirm:	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", confirmHelp)),
		Cancel:		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		Help:		helpToggleBinding,
	}
}

// --------------------------------------
// dashboard: confirm delete mode
// --------------------------------------

type dashboardConfirmKeyMap struct {
	Confirm	key.Binding
	Cancel	key.Binding
	Help	key.Binding
}

func (k dashboardConfirmKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Confirm, k.Cancel}
}

func (k dashboardConfirmKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Confirm, k.Cancel}, {k.Help}}
}

var dashboardConfirmKeys = dashboardConfirmKeyMap{
	Confirm:	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm delete")),
	Cancel:		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	Help:		helpToggleBinding,
}

// --------------------------------------
// call screen
// --------------------------------------

type callKeyMap struct {
	Send		key.Binding
	LineBreak	key.Binding
	Scroll		key.Binding
	Mute		key.Binding
	Command		key.Binding
	Leave		key.Binding
	Help		key.Binding
	Quit		key.Binding
}

func (k callKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.Mute, k.Command, k.Leave, k.Help}
}

func (k callKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Send, k.LineBreak}, {k.Scroll, k.Mute}, {k.Leave, k.Quit}, {k.Help, k.Command}}
}

func callKeysFor(m Model) callKeyMap {
	muteHelp := "mute"
	if m.call.muted { muteHelp = "unmute" }
	return callKeyMap{
		Send:		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
		LineBreak:	key.NewBinding(key.WithKeys("shift+enter"), key.WithHelp("shift+enter", "line break")),
		Scroll:		key.NewBinding(key.WithKeys("ctrl+up", "ctrl+down"), key.WithHelp("ctrl+↑/↓", "scroll chat")),
		Mute:		key.NewBinding(key.WithKeys("ctrl+m"), key.WithHelp("ctrl+m", muteHelp)),
		Command:	key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("ctrl+o", "commands")),
		Leave:		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "leave room")),
		Help:		helpToggleBinding,
		Quit:		key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	}
}

// --------------------------------------
// call screen: peer volume menu
// --------------------------------------

type volumeMenuKeyMap struct {
	Navigate	key.Binding
	Adjust		key.Binding
	Close		key.Binding
	Help		key.Binding
}

func (k volumeMenuKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Navigate, k.Adjust, k.Close}
}

func (k volumeMenuKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Navigate, k.Adjust}, {k.Close}, {k.Help}}
}

var volumeMenuKeys = volumeMenuKeyMap{
	Navigate:	key.NewBinding(key.WithKeys("ctrl+up", "ctrl+down"), key.WithHelp("ctrl+↑/↓", "select peer")),
	Adjust:		key.NewBinding(key.WithKeys("ctrl+left", "ctrl+right"), key.WithHelp("ctrl+←/→", "volume")),
	Close:		key.NewBinding(key.WithKeys("esc", "ctrl+v"), key.WithHelp("esc", "close")),
	Help:		helpToggleBinding,
}

// --------------------------------------
// call screen: invite prompt
// --------------------------------------

type invitePromptKeyMap struct {
	Confirm	key.Binding
	Cancel	key.Binding
	Help	key.Binding
}

func (k invitePromptKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Confirm, k.Cancel}
}

func (k invitePromptKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Confirm, k.Cancel}, {k.Help}}
}

var invitePromptKeys = invitePromptKeyMap{
	Confirm:	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send invite")),
	Cancel:		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	Help:		helpToggleBinding,
}

// --------------------------------------
// call screen: room members view
// --------------------------------------

type membersMenuKeyMap struct {
	Navigate	key.Binding
	Close		key.Binding
	Help		key.Binding
}

func (k membersMenuKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Navigate, k.Close}
}

func (k membersMenuKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Navigate}, {k.Close}, {k.Help}}
}

var membersMenuKeys = membersMenuKeyMap{
	Navigate:	key.NewBinding(key.WithKeys("ctrl+up", "ctrl+down"), key.WithHelp("ctrl+↑/↓", "scroll")),
	Close:		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close")),
	Help:		helpToggleBinding,
}
