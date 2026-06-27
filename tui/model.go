package main

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type focus int

const (
	focusPeers focus = iota
	focusSend
)

// model is the bubbletea cockpit state. All I/O is initiated from tea.Cmds in
// update.go; the struct itself only holds widget state and rendered lines.
type model struct {
	base string
	me   string

	width  int
	height int

	focus    focus
	peers    list.Model
	stream   viewport.Model
	input    textinput.Model
	streamCh chan Message

	lines    []string
	selected string
	status   string
	err      string
}

// peerItem adapts a broker Peer to a bubbles list item.
type peerItem struct{ peer Peer }

func (p peerItem) Title() string {
	mark := "○"
	if p.peer.Online {
		mark = "●"
	}
	return fmt.Sprintf("%s %s", mark, p.peer.Agent)
}
func (p peerItem) Description() string { return p.peer.Machine }
func (p peerItem) FilterValue() string { return p.peer.Agent }

type peersLoadedMsg struct {
	peers []Peer
	err   error
}

type streamMsg struct{ msg Message }
type streamEndMsg struct{}
type sentMsg struct {
	queued bool
	err    error
}

// NewModel builds the cockpit. base is the broker URL, me is this TUI's agent
// name (used as /stream target and send `from`).
func NewModel(base, me string) model {
	peersList := list.New(nil, list.NewDefaultDelegate(), 30, 10)
	peersList.Title = "peers"
	peersList.SetShowStatusBar(false)
	peersList.SetShowHelp(false)
	peersList.SetShowPagination(false)
	peersList.SetFilteringEnabled(false)
	peersList.SetShowFilter(false)

	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "type a message…"
	input.CharLimit = 0
	input.Focus()

	return model{
		base:     base,
		me:       me,
		focus:    focusSend,
		peers:    peersList,
		stream:   viewport.New(60, 10),
		input:    input,
		streamCh: make(chan Message, 16),
	}
}

func (m model) Init() tea.Cmd {
	// Re-issue the cursor blink cmd; the focused state was set in NewModel.
	blink := m.input.Focus()
	return tea.Batch(
		fetchPeers(m.base),
		startStream(m.base, m.me, m.streamCh),
		readStreamMsg(m.streamCh),
		blink,
	)
}
