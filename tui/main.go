package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	name := fs.String("name", "", "this TUI's agent name (overrides PEER_NAME)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	base := os.Getenv("CPV2_URL")
	if base == "" {
		base = "http://127.0.0.1:7900"
	}
	me := *name
	if me == "" {
		if me = os.Getenv("PEER_NAME"); me == "" {
			me = "tui"
		}
	}

	p := tea.NewProgram(NewModel(base, me), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
		return 1
	}
	return 0
}
