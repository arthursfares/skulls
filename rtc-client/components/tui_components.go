package components

import (
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"rtc-client/backend"
	"rtc-client/styles"
)


func MaxContentLines(totalHeight int) int {
	n := totalHeight - 4 // 2 border rows + 2 fixed header/blank rows
	if n < 0 { n = 0 }
	return n
}

// NOTE. PaneStyle is the bordered box every screen box renders inside of - 
// focused panes get the accent border color, blurred ones the dim one.
func PaneStyle(active bool, w int, h int) lipgloss.Style {
	style := styles.BlurredBorder
	if active { style = styles.FocusedBorder }
	return style.Width(w).Height(h)
}

// NOTE. DisplayName resolves a peer id to its display name, falling back to 
// an id if the peer hasn't announced a name yet.
func DisplayName(peerNames map[string]string, peerID string) string {
	if name, ok := peerNames[peerID]; ok && name != "" { return name }
	return peerID
}

func RenderPeersBox(data PeersBoxData, width int, height int) string {
	micLine := styles.LiveStyle.Render("LIVE")
	if data.Muted { micLine = styles.MutedStyle.Render("MUTED") }
	var humanPeers, mediaPeers []string
	
	for _, p := range data.Peers {
		if data.SyntheticPeers[p] {
			mediaPeers = append(mediaPeers, p)
		} else {
			humanPeers = append(humanPeers, p)
		}
	}

	var b strings.Builder
	b.WriteString(styles.HeaderStyle.Render("peers (" + strconv.Itoa(len(data.Peers)) + ")"))
	b.WriteString("  ")
	b.WriteString(micLine)
	b.WriteString("\n\n")

	selfName := data.Username + " (you)"
	if data.SelfSpeaking {
		b.WriteString(styles.SpeakingStyle.Render(selfName + " ●"))
	} else {
		b.WriteString(styles.MeStyle.Render(selfName + "  "))
	}
	b.WriteString("\n")

	innerWidth := width - 2 // border consumes one column each side
	if innerWidth < 0 { innerWidth = 0 }

	if len(humanPeers) == 0 {
		b.WriteString(styles.SystemStyle.Render("no one else yet"))
	} else {
		b.WriteString(PeersInline(humanPeers, data.PeerNames, data.SpeakingPeers, innerWidth))
	}

	// /play media sources get their own line, for a volume-controllable audio feed.
	if len(mediaPeers) > 0 {
		b.WriteString("\n")
		b.WriteString(mediaPeersLine(mediaPeers, data.PeerNames, innerWidth))
	}

	return PaneStyle(false, width, height).Render(b.String()) // read-only, never focused
}

// NOTE. mediaPeersLine renders active /play media sources on a single ';'-separated line
func mediaPeersLine(peers []string, peerNames map[string]string, maxWidth int) string {
	if maxWidth < 0 { maxWidth = 0 }

	names := make([]string, len(peers))
	for i, p := range peers { names[i] = "♪ " + DisplayName(peerNames, p) }
	joined := strings.Join(names, "; ")
	if lipgloss.Width(joined) <= maxWidth { return styles.MediaStyle.Render(joined) }

	budget := maxWidth - 3 // reserve room for "..."
	if budget < 0 { budget = 0 }
	kept, width := 0, 0
	for _, s := range names {
		w := lipgloss.Width(s)
		sep := 0
		if kept > 0 { sep = 2 } // "; "
		if width+sep+w > budget { break }
		width += sep + w
		kept++
	}
	if kept == 0 { return styles.MediaStyle.Render(names[0]) }
	return styles.MediaStyle.Render(strings.Join(names[:kept], "; ") + "...")
}

// NOTE. PeersInline renders peer names on a single ';'-separated line so the box
// stays a fixed height as the room fills up, instead of growing/scrolling
// one name per row. Speaking (VAD-active) peers sort first so who's talking
// stays visible even when the line has to be truncated with "...".
func PeersInline(peers []string, peerNames map[string]string, speaking map[string]bool, maxWidth int) string {
	if maxWidth < 0 { maxWidth = 0 }

	ordered := make([]string, len(peers))
	copy(ordered, peers)
	sort.SliceStable(ordered, func(i, j int) bool {
		return speaking[ordered[i]] && !speaking[ordered[j]]
	})

	plain := make([]string, len(ordered))
	styled := make([]string, len(ordered))
	for i, p := range ordered {
		name := DisplayName(peerNames, p)
		if speaking[p] {
			plain[i] = name + " ●"
			styled[i] = styles.SpeakingStyle.Render(plain[i])
		} else {
			plain[i] = name
			styled[i] = styles.PeerStyle.Render(plain[i])
		}
	}

	if lipgloss.Width(strings.Join(plain, "; ")) <= maxWidth { return strings.Join(styled, "; ") }

	budget := maxWidth - 3 // reserve room for "..."
	if budget < 0 { budget = 0 }
	kept := 0
	width := 0
	for _, s := range plain {
		w := lipgloss.Width(s)
		sep := 0
		if kept > 0 { sep = 2 } // "; "
		if width+sep+w > budget { break }
		width += sep + w
		kept++
	}
	if kept == 0 { return styled[0] }
	return strings.Join(styled[:kept], "; ") + styles.SystemStyle.Render("...")
}

// NOTE. volumeBarWidth is how many cells wide each peer's volume bar renders,
// scaled against backend.PeerVolumeMax.
const volumeBarWidth = 12

func RenderVolumeBar(gain float64) string {
	filled := int(gain/backend.PeerVolumeMax*volumeBarWidth + 0.5)
	if filled > volumeBarWidth { filled = volumeBarWidth }
	if filled < 0 { filled = 0 }
	bar := strings.Repeat("█", filled) + strings.Repeat("░", volumeBarWidth-filled)
	pct := int(gain*100 + 0.5)
	return bar + " " + strconv.Itoa(pct) + "%"
}

// NOTE. compressorBarWidth mirrors volumeBarWidth, scaled between
// backend.CompressorTargetMinDB and backend.CompressorTargetMaxDB.
const compressorBarWidth = 12

func RenderCompressorTargetRow(focused bool) string {
	header := styles.HeaderStyle.Render("normalize peers to")
	if focused { header = styles.CursorStyle.Render("> ") + header }

	db := backend.CompressorTargetDB()
	span := backend.CompressorTargetMaxDB - backend.CompressorTargetMinDB
	filled := int((db-backend.CompressorTargetMinDB)/span*compressorBarWidth + 0.5)
	if filled > compressorBarWidth { filled = compressorBarWidth }
	if filled < 0 { filled = 0 }
	bar := strings.Repeat("█", filled) + strings.Repeat("░", compressorBarWidth-filled)

	hint := ""
	if focused { hint = "  (←/→ to adjust)" }
	return header + "\n" + bar + " " + strconv.Itoa(int(db)) + " dB" + hint
}

// NOTE. RenderVolumeMenuBox replaces the peers box while the volume menu is open:
// same dimensions and border as RenderPeersBox, but lists peers with a selection cursor
// and a volume bar instead of speaking indicators. Scrolls its window to
// keep the selected peer visible when there are more peers than rows.
func RenderVolumeMenuBox(data VolumeMenuData, width int, height int) string {
	maxRows := MaxContentLines(height)
	if maxRows < 0 { maxRows = 0 }

	var b strings.Builder
	b.WriteString(styles.HeaderStyle.Render("peer volume"))
	b.WriteString("\n\n")

	if len(data.Peers) == 0 {
		b.WriteString(styles.SystemStyle.Render("no one else yet"))
	} else {
		start := 0
		if len(data.Peers) > maxRows && maxRows > 0 {
			start = data.VolumeMenuIndex - maxRows/2
			if start < 0 { start = 0 }
			if start > len(data.Peers)-maxRows { start = len(data.Peers) - maxRows }
		}
		end := start + maxRows
		if end > len(data.Peers) { end = len(data.Peers) }

		for i := start; i < end; i++ {
			p := data.Peers[i]
			row := DisplayName(data.PeerNames, p) + "  " + RenderVolumeBar(backend.Mixer.PeerGain(p))
			if i == data.VolumeMenuIndex {
				b.WriteString(styles.CursorStyle.Render("> " + row))
			} else {
				b.WriteString(styles.PeerStyle.Render("  " + row))
			}
			b.WriteString("\n")
		}
	}
	return PaneStyle(true, width, height).Render(b.String()) // focused - it's the active control surface while open
}

// NOTE. RenderMembersBox replaces the peers box while the room members view is oepn
func RenderMembersBox(data MembersBoxData, width int, height int) string {
	maxRows := MaxContentLines(height)
	if maxRows < 0 { maxRows = 0 }

	var b strings.Builder
	b.WriteString(styles.HeaderStyle.Render("room members (" + strconv.Itoa(len(data.Members)) + ")"))
	b.WriteString("\n\n")

	rows := make([]string, 0, len(data.Members)+len(data.PendingInvites))
	styled := make([]string, 0, cap(rows))
	for _, username := range data.Members {
		tag := "offline"
		if data.Connected[username] { tag = "connected" }
		if username == data.Owner { tag = "owner, " + tag }
		text := username + " (" + tag + ")"
		rows = append(rows, text)
		styled = append(styled, styles.PeerStyle.Render(text))
	}
	for _, username := range data.PendingInvites {
		text := "invited: " + username
		rows = append(rows, text)
		styled = append(styled, styles.MediaStyle.Render(text))
	}

	if len(rows) == 0 {
		b.WriteString(styles.SystemStyle.Render("no members"))
	} else {
		start := 0
		if len(rows) > maxRows && maxRows > 0 {
			start = data.SelectedIndex - maxRows/2
			if start < 0 { start = 0 }
			if start > len(rows)-maxRows { start = len(rows) - maxRows }
		}
		end := start + maxRows
		if end > len(rows) { end = len(rows) }

		for i := start; i < end; i++ {
			if i == data.SelectedIndex {
				b.WriteString(styles.CursorStyle.Render("> " + rows[i]))
			} else {
				b.WriteString("  ")
				b.WriteString(styled[i])
			}
			b.WriteString("\n")
		}
	}

	return PaneStyle(true, width, height).Render(b.String()) // focused - it's the active control surface while open
}

// NOTE. RenderInvitePromptBox replaces the peers box while the invite-username prompt is open.
func RenderInvitePromptBox(roomName string, inputView string, width int, height int) string {
	var b strings.Builder
	b.WriteString(styles.HeaderStyle.Render("invite to " + roomName))
	b.WriteString("\n\n")
	b.WriteString(styles.PromptStyle.Render("username: "))
	b.WriteString(inputView)
	return PaneStyle(true, width, height).Render(b.String()) // focused - it's the active control surface while open
}

// NOTE. RenderChatLog renders the chat-facing entries from log ("me"/"peer" plus
// the /private, /roll, notice and error kinds), for display inside the chat
// viewport. "system" entries (connection events, mute toggles, peer
// joined/left) are still recorded in the log but not displayed here - but errors
// (e.g. a failed /play) need to actually be seen, so they're shown same as
// a notice.
func RenderChatLog(log []LogEntry) string {
	var b strings.Builder
	for _, e := range log {
		stamp := styles.TimestampStyle.Render(e.At.Format("2006-01-02 15:04") + " ")
		switch e.Kind {
		case "me":
			b.WriteString(stamp)
			b.WriteString(styles.MeStyle.Render("me: " + e.Text))
			b.WriteString("\n")
		case "peer":
			b.WriteString(stamp)
			b.WriteString(styles.PeerStyle.Render(e.Text))
			b.WriteString("\n")
		case "private-out", "private-in":
			b.WriteString(stamp)
			b.WriteString(styles.PrivateStyle.Render(e.Text))
			b.WriteString("\n")
		case "roll":
			b.WriteString(stamp)
			b.WriteString(styles.RollStyle.Render(e.Text))
			b.WriteString("\n")
		case "help":
			b.WriteString(stamp)
			b.WriteString(styles.HelpStyle.Render(e.Text))
			b.WriteString("\n")
		case "notice", "error":
			b.WriteString(stamp)
			b.WriteString(styles.ErrorStyle.Render(e.Text))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func RenderChatBox(chatViewport viewport.Model, chatInput textarea.Model, volumeMenuOpen bool, width int, height int) string {
	header := styles.HeaderStyle.Render("chat")
	viewportHeight := height - 4 - chatInput.Height() 				// pane border(2) + "chat" header(1) + blank line(1) + input area
	if viewportHeight < 1 { viewportHeight = 1 }
	chatViewport.SetHeight(viewportHeight)
	body := header + "\n\n" + chatViewport.View() + "\n" + chatInput.View()
	return PaneStyle(!volumeMenuOpen, width, height).Render(body) 	// focused unless the volume menu has taken over arrow/enter input
}

// NOTE. RenderFooter renders the command hints pinned to the bottom of the
// screen: whatever keymap the caller passes in determines what shows up.
func RenderFooter(h help.Model, width int, km help.KeyMap) string {
	h.SetWidth(width)
	return styles.FooterStyle.Render(h.View(km))
}
