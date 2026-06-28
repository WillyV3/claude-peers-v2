package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
)

// defaultPeerName returns a hostname-based peer name (short host, lowercased),
// falling back to "peer" if the hostname can't be read.
func defaultPeerName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "peer"
	}
	if i := strings.IndexByte(h, '.'); i > 0 {
		h = h[:i]
	}
	return strings.ToLower(h)
}

func claudeSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// mergeSettings is the SAFETY primitive for touching user JSON config.
//
// Read-merge-write, never clobber:
//   - If path does not exist: create parent dirs + the file with just our key.
//     No .bak (nothing to back up).
//   - If path exists: write a .bak copy of the original bytes BEFORE overwriting.
//     Parse the existing JSON (treat an empty file as {}); on parse error, bail
//     out without overwriting so the user's file is not destroyed.
//   - Set doc[key] = val (we own this key; other keys are left untouched).
//   - Idempotent: if key already holds a value that marshals equal to val, do
//     nothing — no write, no .bak. Running setup twice is a no-op.
//
// The value comparison is done by marshaling both sides to JSON and comparing
// bytes, so map structural equality works without reflect.
func mergeSettings(path, key string, val any) error {
	data, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	doc := map[string]any{}
	if exists && len(data) > 0 {
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	}

	if old, ok := doc[key]; ok {
		ob, mErr := json.Marshal(old)
		nb, nErr := json.Marshal(val)
		if mErr == nil && nErr == nil && bytes.Equal(ob, nb) {
			return nil // already set to the same value: no-op
		}
	}

	doc[key] = val
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	if exists {
		if err := os.WriteFile(path+".bak", data, 0o644); err != nil {
			return fmt.Errorf("write %s.bak: %w", path, err)
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
		}
	}
	return os.WriteFile(path, out, 0o644)
}

// mergeMCPServer merges a single server entry under .mcp.json's "mcpServers"
// object without clobbering the other servers. Same SAFETY rules as
// mergeSettings (.bak before overwrite, create if missing, idempotent).
func mergeMCPServer(path, server string, entry map[string]any) error {
	data, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	doc := map[string]any{}
	if exists && len(data) > 0 {
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	}

	servers, _ := doc["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}

	if old, ok := servers[server]; ok {
		ob, mErr := json.Marshal(old)
		nb, nErr := json.Marshal(entry)
		if mErr == nil && nErr == nil && bytes.Equal(ob, nb) {
			return nil
		}
	}

	servers[server] = entry
	doc["mcpServers"] = servers
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	if exists {
		if err := os.WriteFile(path+".bak", data, 0o644); err != nil {
			return fmt.Errorf("write %s.bak: %w", path, err)
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, out, 0o644)
}

func detectAgents() map[string]bool {
	detected := map[string]bool{}
	for _, a := range []string{"claude", "pi", "codex", "opencode"} {
		if _, err := exec.LookPath(a); err == nil {
			detected[a] = true
		}
	}
	return detected
}

func cmdSetup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	agent := fs.String("agent", "", "agent to wire: claude|pi|codex|opencode")
	name := fs.String("name", defaultPeerName(), "peer name (default: hostname)")
	broker := fs.String("broker", defaultBaseURL(), "broker URL")
	mcp := fs.String("mcp", "", "path to .mcp.json to merge the peers server into")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	agentOrder := []string{"claude", "pi", "codex", "opencode"}
	detected := detectAgents()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tSTATUS")
	for _, a := range agentOrder {
		status := "not installed"
		if detected[a] {
			status = "installed"
		}
		fmt.Fprintf(w, "%s\t%s\n", a, status)
	}
	w.Flush()

	if *agent == "" {
		fmt.Println()
		fmt.Println("usage: cpv2 setup --agent <claude|pi|codex|opencode> [--name <peer>] [--broker <url>] [--mcp <path>]")
		return 0
	}

	switch *agent {
	case "claude":
		return setupClaude(*name, *broker, *mcp)
	case "pi", "codex", "opencode":
		return setupManual(*agent, *name, *broker)
	default:
		fmt.Fprintf(os.Stderr, "setup: unknown agent %q (claude|pi|codex|opencode)\n", *agent)
		return 2
	}
}

// setupClaude does the dirty work: merge the statusLine entry into
// ~/.claude/settings.json (no-clobber), optionally merge the peers MCP server
// into a .mcp.json, and print the channel launch note + a summary of changed
// files.
func setupClaude(name, broker, mcp string) int {
	settingsPath, err := claudeSettingsPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup: home dir:", err)
		return 1
	}

	if err := mergeSettings(settingsPath, "statusLine", map[string]any{
		"type":    "command",
		"command": "cpv2 statusline",
	}); err != nil {
		fmt.Fprintln(os.Stderr, "setup: merge settings:", err)
		return 1
	}
	fmt.Printf("merged statusLine into %s\n", settingsPath)
	changed := []string{settingsPath}

	fmt.Println()
	fmt.Println("Launch Claude with the peers dev channel loaded:")
	fmt.Println("  claude --dangerously-load-development-channels server:peers")
	fmt.Println("  (or just: cpv2 run)")
	fmt.Println()
	fmt.Println("Your project also needs a .mcp.json entry for the `peers` server,")
	fmt.Println("pointing at adapters/claude/channel.ts, e.g.:")
	fmt.Printf(`  {"mcpServers":{"peers":{"command":"bun","args":["adapters/claude/channel.ts"],"env":{"PEER_NAME":%q,"BROKER":%q}}}}`+"\n", name, broker)

	if mcp != "" {
		entry := map[string]any{
			"command": "bun",
			"args":    []string{"adapters/claude/channel.ts"},
			"env": map[string]any{
				"PEER_NAME": name,
				"BROKER":    broker,
			},
		}
		if err := mergeMCPServer(mcp, "peers", entry); err != nil {
			fmt.Fprintln(os.Stderr, "setup: merge mcp:", err)
			return 1
		}
		fmt.Printf("merged `peers` server into %s\n", mcp)
		changed = append(changed, mcp)
	}

	fmt.Println()
	fmt.Println("Files changed:")
	for _, p := range changed {
		fmt.Printf("  - %s\n", p)
	}
	fmt.Println()
	fmt.Printf("Peer name: %s (set PEER_NAME env to override at runtime)\n", name)
	fmt.Printf("Broker:    %s\n", broker)
	return 0
}

// setupManual prints the exact manual steps for runtimes we don't auto-wire.
// Lazy + honest: full auto for Claude, guided for the rest.
func setupManual(agent, name, broker string) int {
	fmt.Println()
	switch agent {
	case "pi":
		fmt.Println("Manual steps for pi:")
		fmt.Println("  1. Start the broker: cpv2 serve")
		fmt.Println("  2. Pair your peer (run as the peer):")
		fmt.Printf("       cpv2 pair --from <peer> --to %s\n", name)
		fmt.Printf("       cpv2 approve --owner %s --code <code>\n", name)
		fmt.Println("  3. Load the adapter in pi (it exports a default extension):")
		fmt.Println("       pi --extension adapters/pi/peer.ts")
		fmt.Println("     or drop adapters/pi/peer.ts into your pi extensions dir.")
		fmt.Printf("  4. Set PEER_NAME=%s so the adapter uses it.\n", name)
		fmt.Println("  Adapter: adapters/pi/peer.ts  (injects via pi.sendMessage, deliverAs=steer)")
	case "codex":
		fmt.Println("Manual steps for codex:")
		fmt.Println("  1. Start the broker: cpv2 serve")
		fmt.Println("  2. Pair your peer (run as the peer):")
		fmt.Printf("       cpv2 pair --from <peer> --to %s\n", name)
		fmt.Printf("       cpv2 approve --owner %s --code <code>\n", name)
		fmt.Println("  3. Run the adapter (requires `codex` on PATH):")
		fmt.Printf("       BROKER=%s PEER_NAME=%s bun adapters/codex/adapter.ts\n", broker, name)
		fmt.Println("  Adapter: adapters/codex/adapter.ts  (codex app-server turn/steer)")
	case "opencode":
		fmt.Println("Manual steps for opencode:")
		fmt.Println("  1. Start an opencode server: opencode serve")
		fmt.Println("  2. Start the broker: cpv2 serve")
		fmt.Println("  3. Pair your peer (run as the peer):")
		fmt.Printf("       cpv2 pair --from <peer> --to %s\n", name)
		fmt.Printf("       cpv2 approve --owner %s --code <code>\n", name)
		fmt.Println("  4. Run the adapter:")
		fmt.Printf("       BROKER=%s OPENCODE_URL=http://127.0.0.1:4096 PEER_NAME=%s bun adapters/opencode/adapter.ts\n", broker, name)
		fmt.Println("  Adapter: adapters/opencode/adapter.ts  (POST /session/{id}/prompt {delivery})")
	}
	return 0
}

// cmdRun execs the real `claude` with the dev-channel flag injected, so a user
// just runs `cpv2 run`. --as/--name in the args are stripped and exported as
// PEER_NAME. syscall.Exec replaces the process (no spawn+wait).
func cmdRun(args []string) int {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cpv2 run: claude not found on PATH")
		return 1
	}

	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--as" || a == "--name":
			if i+1 < len(args) {
				os.Setenv("PEER_NAME", args[i+1])
				i++ // skip the value
			}
		case strings.HasPrefix(a, "--as="):
			os.Setenv("PEER_NAME", strings.TrimPrefix(a, "--as="))
		case strings.HasPrefix(a, "--name="):
			os.Setenv("PEER_NAME", strings.TrimPrefix(a, "--name="))
		default:
			rest = append(rest, a)
		}
	}

	argv := append([]string{"claude", "--dangerously-load-development-channels", "server:peers"}, rest...)
	if err := syscall.Exec(claudePath, argv, os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, "cpv2 run: exec:", err)
		return 1
	}
	return 0 // unreachable on successful exec
}
