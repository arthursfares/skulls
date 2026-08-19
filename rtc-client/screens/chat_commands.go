package screens

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"rtc-client/backend"
	"rtc-client/components"
)


const (
	helpUsage	 = "/help"
	privateUsage = "/private <user> <message>"
	rollUsage    = "/roll <NdM>"
	playUsage    = "/play <youtube-url>"
	stopUsage    = "/stop"
	imageUsage   = "/image <path or url> [caption]"
)

var chatCommands map[string]chatCommand

func init() { // go runtime call init() automatically at package import
	chatCommands = map[string]chatCommand{
		"help":	   {usage: helpUsage, run: runHelpCommand},
		"private": {usage: privateUsage, run: runPrivateCommand},
		"roll":    {usage: rollUsage, run: runRollCommand},
		"play":    {usage: playUsage, run: runPlayCommand},
		"stop":    {usage: stopUsage, run: runStopCommand},
		"image":   {usage: imageUsage, run: runImageCommand},
	}
}

func (m Model) appendNotice(text string) Model {
	m.log = append(m.log, components.LogEntry{ Kind: "notice", Text: text, At: time.Now() })
	m.refreshChatLog()
	return m
}

func runHelpCommand(m Model, args string) (Model, tea.Cmd) {
	names := make([]string, 0, len(chatCommands))
	for name := range chatCommands { names = append(names, name) }
	sort.Strings(names)
	usages := make([]string, 0, len(names))
	for _, name := range names { usages = append(usages, chatCommands[name].usage) }
	text := strings.Join(usages, "\n")
	m.log = append(m.log, components.LogEntry{ Kind: "help", Text: text, At: time.Now() })
	m.refreshChatLog()
	return m, nil
}

// NOTE. runPrivateCommand resolves <user> against the room's known peers (by
// display name, case-insensitive) and sends the rest of the line to only
// that peer's data channel.
func runPrivateCommand(m Model, args string) (Model, tea.Cmd) {
	username, text, _ := strings.Cut(strings.TrimSpace(args), " ")
	text = strings.TrimSpace(text)
	if username == "" || text == "" { return m.appendNotice("usage: " + privateUsage), nil }
	peerID := ""
	for id, name := range m.call.peerNames {
		if strings.EqualFold(name, username) { peerID = id; break }
	}
	if peerID == "" { return m.appendNotice("no such user in this room: " + username), nil }
	if !backend.SendToPeer(peerID, text) { return m.appendNotice("couldn't reach " + username + " (chat channel not open)"), nil }
	entry := components.LogEntry{ Kind: "private-out", Text: "(private to " + username + ") " + text, At: time.Now() }
	m.log = append(m.log, entry)
	m.saveHistoryEntry(entry)
	m.refreshChatLog()
	return m, nil
}

// NOTE. runRollCommand does a cheap local sanity check and then defers to the
// signaling server, which parses the notation and rolls; the result comes
// back as a RollResultMsg broadcast to the whole room.
func runRollCommand(m Model, args string) (Model, tea.Cmd) {
	notation := strings.TrimSpace(args)
	if !strings.Contains(notation, "d") { return m.appendNotice("usage: " + rollUsage), nil }
	backend.RequestRoll(notation)
	return m, nil
}

// NOTE. runPlayCommand starts (or replaces) media playback from a YouTube URL.
// No client-side URL validation.
func runPlayCommand(m Model, args string) (Model, tea.Cmd) {
	url := strings.TrimSpace(args)
	if url == "" { return m.appendNotice("usage: " + playUsage), nil }
	return m.appendNotice("starting playback..."), backend.PlayCmd(url)
}

// NOTE. runStopCommand stops this client's own active playback, if any.
func runStopCommand(m Model, args string) (Model, tea.Cmd) {
	return m, backend.StopCmd()
}

// NOTE. runImageCommand starts loading a local file or http(s) URL for
// broadcast, with an optional caption; the actual decode/downscale/send
// happens off the UI goroutine in backend.LoadImageCmd, which reports back
// with an ImageMsg (or ErrorMsg on failure).
func runImageCommand(m Model, args string) (Model, tea.Cmd) {
	source, caption, _ := strings.Cut(strings.TrimSpace(args), " ")
	caption = strings.TrimSpace(caption)
	if source == "" { return m.appendNotice("usage: " + imageUsage), nil }
	return m.appendNotice("loading image..."), backend.LoadImageCmd(source, caption)
}

