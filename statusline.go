package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// statusLine returns the one compact line a coding-agent statusline shows.
// It is pure: given a broker base URL and an agent name it produces the line
// with no side effects, so it can be tested directly without touching os.Args
// or os.Stdin.
//
// States (always one line, never an error):
//   - no name set          -> "○ peers: no PEER_NAME set"
//   - broker unreachable   -> "○ peers: broker down"
//   - self not in list     -> "○ peers: <name> · not registered"
//   - self present, offline-> "◌ peers: <name> · offline"
//   - self present, online -> "● peers: <name> · <N> online"  (N = count of online peers)
//
// When OTHER online peers share this session's cwd, the line appends
// "· ⚠ also here: <names>" so opening a session in an already-occupied directory
// is visible instead of silently confusing. cwd "" disables that check.
//
// The HTTP call is capped at 500ms so a down/slow broker cannot hang the line.
func statusLine(base, name, cwd string) string {
	if name == "" {
		return "○ peers: no PEER_NAME set"
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get(base + "/peers")
	if err != nil {
		return "○ peers: broker down"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "○ peers: broker down"
	}
	var peers []Peer
	if err := json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		return "○ peers: broker down"
	}
	onlineCount := 0
	found := false
	selfOnline := false
	var here []string // other online peers sharing this cwd
	for i := range peers {
		p := peers[i]
		if p.Online {
			onlineCount++
		}
		if p.Agent == name {
			found = true
			selfOnline = p.Online
		}
		if cwd != "" && p.Cwd == cwd && p.Agent != name && p.Online {
			here = append(here, p.Agent)
		}
	}

	var line string
	switch {
	case !found:
		line = fmt.Sprintf("○ peers: %s · not registered", name)
	case !selfOnline:
		line = fmt.Sprintf("◌ peers: %s · offline", name)
	default:
		line = fmt.Sprintf("● peers: %s · %d online", name, onlineCount)
	}
	if len(here) > 0 {
		line += " · ⚠ also here: " + strings.Join(here, ", ")
	}
	return line
}

// drainStdin reads and discards stdin when it is a pipe (not a TTY). Claude
// Code pumps session JSON on stdin to statusline commands; if we don't drain
// it the pipe fills and the host blocks. We only drain non-char-device stdin
// so an interactive `cpv2 statusline` in a terminal does not hang on EOF.
func drainStdin() {
	if fi, err := os.Stdin.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) == 0 {
		io.Copy(io.Discard, os.Stdin)
	}
}

// cmdStatusLine is the subcommand wrapper. It resolves the name (flag or
// PEER_NAME env), drains stdin, prints the line, and always exits 0 — a
// statusline must never error out the host that calls it.
func cmdStatusLine(args []string) int {
	fs := flag.NewFlagSet("statusline", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	name := fs.String("name", "", "peer name (defaults to $PEER_NAME)")
	// A statusline must never error out the host: swallow flag parse errors too.
	_ = fs.Parse(args)
	drainStdin()
	n := *name
	if n == "" {
		n = os.Getenv("PEER_NAME")
	}
	cwd, _ := os.Getwd()
	fmt.Println(statusLine(defaultBaseURL(), n, cwd))
	return 0
}
