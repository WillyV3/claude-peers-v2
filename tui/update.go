package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func fetchPeers(base string) tea.Cmd {
	return func() tea.Msg {
		peers, err := Peers(base)
		return peersLoadedMsg{peers: peers, err: err}
	}
}

func pollPeers(base string) tea.Cmd {
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg {
		peers, err := Peers(base)
		return peersLoadedMsg{peers: peers, err: err}
	})
}

func startStream(base, me string, ch chan<- Message) tea.Cmd {
	return func() tea.Msg {
		Stream(base, me, ch)
		return streamEndMsg{}
	}
}

func readStreamMsg(ch <-chan Message) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return streamEndMsg{}
		}
		return streamMsg{msg: msg}
	}
}

func sendMessage(base, from, to, content string) tea.Cmd {
	return func() tea.Msg {
		queued, err := Send(base, from, to, content)
		return sentMsg{queued: queued, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case peersLoadedMsg:
		if msg.err != nil {
			m.err = "peers: " + msg.err.Error()
		} else {
			m.err = ""
			items := make([]list.Item, len(msg.peers))
			for i, p := range msg.peers {
				items[i] = peerItem{p}
			}
			setCmd := m.peers.SetItems(items)
			if sel := m.peers.SelectedItem(); sel != nil {
				m.selected = sel.(peerItem).peer.Agent
			}
			return m, tea.Batch(pollPeers(m.base), setCmd)
		}
		return m, pollPeers(m.base)

	case streamMsg:
		m.lines = append(m.lines, formatMessage(msg.msg))
		m.refreshStream()
		return m, readStreamMsg(m.streamCh)

	case streamEndMsg:
		m.lines = append(m.lines, "— stream ended —")
		m.refreshStream()
		return m, nil

	case sentMsg:
		m.status = formatSendResult(msg)
		m.input.Reset()
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		}
		switch m.focus {
		case focusPeers:
			return m.updatePeersKeys(msg)
		case focusSend:
			return m.updateSendKeys(msg)
		}
	}
	return m, nil
}

func (m model) updatePeersKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		m.focus = focusSend
		return m, m.input.Focus()
	case "q":
		return m, tea.Quit
	}
	l, cmd := m.peers.Update(msg)
	m.peers = l
	if sel := m.peers.SelectedItem(); sel != nil {
		m.selected = sel.(peerItem).peer.Agent
	}
	return m, cmd
}

func (m model) updateSendKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		m.focus = focusPeers
		m.input.Blur()
		return m, nil
	case "enter":
		content := m.input.Value()
		if content == "" {
			return m, nil
		}
		if m.selected == "" {
			m.status = "select a peer first (tab to peers, ↑/↓, then tab back)"
			return m, nil
		}
		m.status = "sending…"
		return m, sendMessage(m.base, m.me, m.selected, content)
	}
	ti, cmd := m.input.Update(msg)
	m.input = ti
	return m, cmd
}

func formatMessage(msg Message) string {
	return fmt.Sprintf("[%s] %s → %s: %s", fmtTime(msg.CreatedAt), msg.From, msg.To, msg.Content)
}

func formatSendResult(msg sentMsg) string {
	if msg.err != nil {
		if se, ok := msg.err.(*StatusError); ok && se.Status == http.StatusForbidden {
			return "not paired: use the CLI (cpv2 pair) to pair with " + strings.TrimSpace(se.Body)
		}
		return "send failed: " + msg.err.Error()
	}
	if msg.queued {
		return "queued (recipient offline; delivered on next connect)"
	}
	return "sent"
}

func fmtTime(unix int64) string {
	if unix <= 0 {
		return "--:--"
	}
	return time.Unix(unix, 0).Format("15:04")
}
