package components

import (
	"fmt"
	"io"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"rtc-client/backend"
	"rtc-client/styles"
)

// NOTE. FilterValue is what the fuzzy filter searches against, so that using the
// search bar compares the text with both room names and invites.
func (i DashboardItem) FilterValue() string {
	if i.Kind == DashboardItemRoom { return i.Room.Name }
	return i.Invite.RoomName
}

// NOTE. BuildDashboardItems flattens rooms and invites into the single list the dashboard displays (with invites displayed first)
// TODO. make separate tabs for rooms and invites
func BuildDashboardItems(rooms []backend.RoomSummary, invites []backend.InviteSummary, username string) []list.Item {
	items := make([]list.Item, 0, len(rooms)+len(invites))
	for _, inv := range invites {
		items = append(items, DashboardItem{Kind: DashboardItemInvite, Invite: inv})
	}
	for _, r := range rooms {
		items = append(items, DashboardItem{Kind: DashboardItemRoom, Room: r, Mine: r.Owner == username})
	}
	return items
}

func (d DashboardDelegate) Height() int  								{ return 1 }
func (d DashboardDelegate) Spacing() int 								{ return 0 }
func (d DashboardDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd 	{ return nil }

func (d DashboardDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(DashboardItem)
	if !ok { return }
	cursor := " "
	if index == m.Index() { cursor = styles.CursorStyle.Render("> ") }
	var line string
	switch item.Kind {
	case DashboardItemRoom:
		line = styles.PeerStyle.Render(item.Room.Name)
		if item.Mine { line += styles.SystemStyle.Render(" (yours)") }
	case DashboardItemInvite:
		line = styles.LiveStyle.Render("invite: "+item.Invite.RoomName) + " from " + item.Invite.From
	}
	fmt.Fprint(w, cursor+line)
}