package components

import (
	"fmt"
	"io"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	"rtc-client/backend"
	"rtc-client/styles"
)

func (i AudioDeviceItem) FilterValue() string { return i.Option.Name }

func BuildAudioDeviceItems(kind backend.AudioDeviceKind, options []backend.AudioDeviceOption) []list.Item {
	items := make([]list.Item, len(options))
	for i, opt := range options {
		items[i] = AudioDeviceItem{Kind: kind, Option: opt}
	}
	return items
}

func (d AudioDeviceDelegate) Height() int                               { return 1 }
func (d AudioDeviceDelegate) Spacing() int                              { return 0 }
func (d AudioDeviceDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }

func (d AudioDeviceDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(AudioDeviceItem)
	if !ok { return }
	cursor := "  "
	if index == m.Index() { cursor = styles.CursorStyle.Render("> ") }
	check := "  "
	if backend.IsSelectedAudioDevice(item.Kind, item.Option.ID) { check = styles.LiveStyle.Render("* ") }
	name := item.Option.Name
	if item.Option.IsDefault { name += styles.SystemStyle.Render(" (system default)") }
	fmt.Fprint(w, cursor+check+styles.PeerStyle.Render(name))
}
