package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const bottomRows = 5 // send box (border 2 + header 1 + input 1 = 4) + status line 1

var (
	focusColor = lipgloss.Color("63")
	dimColor   = lipgloss.Color("240")
	errColor   = lipgloss.Color("203")
	statusPad  = lipgloss.NewStyle().Padding(0, 1)
)

func boxStyle(focused bool, innerW int) lipgloss.Style {
	s := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(innerW)
	if focused {
		return s.BorderForeground(focusColor)
	}
	return s.BorderForeground(dimColor)
}

func (m *model) layout() {
	if m.width < 20 || m.height < bottomRows+3 {
		return
	}
	leftW := m.width / 3
	if leftW < 24 {
		leftW = 24
	}
	if rightW := m.width - leftW; rightW < 20 {
		leftW = m.width - 20
	}
	rightW := m.width - leftW
	topH := m.height - bottomRows
	innerTopH := topH - 2 // box borders top+bottom
	if innerTopH < 2 {
		innerTopH = 2
	}
	m.peers.SetSize(leftW-2, innerTopH)
	m.stream.Width = rightW - 2
	m.stream.Height = innerTopH
	m.input.Width = m.width - 6
	m.refreshStream()
}

func (m *model) refreshStream() {
	m.stream.SetContent(strings.Join(m.lines, "\n"))
	m.stream.GotoBottom()
}

func (m model) View() string {
	if m.width == 0 {
		return "connecting…"
	}
	leftW := m.width / 3
	if leftW < 24 {
		leftW = 24
	}
	if m.width-leftW < 20 {
		leftW = m.width - 20
	}
	rightW := m.width - leftW

	left := boxStyle(m.focus == focusPeers, leftW-2).Render(m.peers.View())
	right := boxStyle(false, rightW-2).Render(fmt.Sprintf("stream\n%s", m.stream.View()))
	top := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	target := m.selected
	if target == "" {
		target = "—"
	}
	sendContent := fmt.Sprintf("send (to: %s)\n%s", target, m.input.View())
	send := boxStyle(m.focus == focusSend, m.width-2).Render(sendContent)

	return lipgloss.JoinVertical(lipgloss.Left, top, send, m.statusLine())
}

func (m model) statusLine() string {
	switch {
	case m.err != "":
		return statusPad.Foreground(errColor).Render(m.err)
	case m.status != "":
		return statusPad.Faint(true).Render(m.status)
	default:
		return statusPad.Faint(true).Render(fmt.Sprintf(
			"me=%s  broker=%s  | tab:focus  ↑/↓:peers  enter:send  ctrl+c/q:quit",
			m.me, m.base))
	}
}
