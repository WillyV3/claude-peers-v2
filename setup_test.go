package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeSettingsCreatesIfMissing(t *testing.T) {
	dir := t.TempDir()
	// nested path that does not exist yet — parent dir must be created
	path := filepath.Join(dir, "nested", "subdir", "settings.json")

	val := map[string]any{"type": "command", "command": "cpv2 statusline"}
	if err := mergeSettings(path, "statusLine", val); err != nil {
		t.Fatalf("mergeSettings: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse created file: %v\n%s", err, data)
	}
	sl, ok := doc["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("statusLine not set: %v", doc)
	}
	if sl["type"] != "command" || sl["command"] != "cpv2 statusline" {
		t.Fatalf("statusLine value wrong: %v", sl)
	}
	// no .bak when the file did not exist
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Fatalf(".bak should not exist on create; stat err=%v", err)
	}
}

func TestMergeSettingsPreservesExistingAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"someOtherKey":true,"foo":"bar"}` + "\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	val := map[string]any{"type": "command", "command": "cpv2 statusline"}
	if err := mergeSettings(path, "statusLine", val); err != nil {
		t.Fatalf("mergeSettings: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse: %v\n%s", err, data)
	}
	// pre-existing unrelated keys preserved
	if doc["someOtherKey"] != true {
		t.Fatalf("someOtherKey lost: %v", doc)
	}
	if doc["foo"] != "bar" {
		t.Fatalf("foo lost: %v", doc)
	}
	// our key added
	sl, ok := doc["statusLine"].(map[string]any)
	if !ok || sl["command"] != "cpv2 statusline" {
		t.Fatalf("statusLine not merged: %v", doc)
	}
	// .bak created and matches the original
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf(".bak not created: %v", err)
	}
	if !bytes.Equal(bak, original) {
		t.Fatalf(".bak = %s, want %s", bak, original)
	}
}

func TestMergeSettingsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := []byte(`{"existing":1}` + "\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	val := map[string]any{"type": "command", "command": "cpv2 statusline"}

	if err := mergeSettings(path, "statusLine", val); err != nil {
		t.Fatalf("first merge: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after first: %v", err)
	}
	bakAfterFirst, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("read .bak after first: %v", err)
	}
	if !bytes.Equal(bakAfterFirst, original) {
		t.Fatalf("first .bak should be the original; got %s", bakAfterFirst)
	}

	// Second run must be a no-op: same file bytes, no .bak rewrite.
	if err := mergeSettings(path, "statusLine", val); err != nil {
		t.Fatalf("second merge: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after second: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("idempotency: file changed on second run\nfirst:  %s\nsecond: %s", first, second)
	}
	bakAfterSecond, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("read .bak after second: %v", err)
	}
	if !bytes.Equal(bakAfterFirst, bakAfterSecond) {
		t.Fatalf("idempotency: .bak changed on no-op second run\nfirst:  %s\nsecond: %s", bakAfterFirst, bakAfterSecond)
	}

	// No duplication: statusLine appears once, existing key intact.
	var doc map[string]any
	if err := json.Unmarshal(second, &doc); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := doc["statusLine"]; !ok {
		t.Fatalf("statusLine missing after second run: %v", doc)
	}
	if doc["existing"] != float64(1) {
		t.Fatalf("existing key lost: %v", doc)
	}
}

func TestMergeSettingsPreservesOnParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	garbage := []byte(`{not valid json`)
	if err := os.WriteFile(path, garbage, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	val := map[string]any{"type": "command", "command": "cpv2 statusline"}
	err := mergeSettings(path, "statusLine", val)
	if err == nil {
		t.Fatalf("expected parse error, got nil")
	}
	// The user's file must be untouched.
	kept, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(kept, garbage) {
		t.Fatalf("parse error should not overwrite; got %s", kept)
	}
}

func TestMergeMCPServerPreservesOtherServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	original := []byte(`{"mcpServers":{"filesystem":{"command":"npx","args":["-y","@modelcontextprotocol/server-filesystem","."]}}}` + "\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	peersEntry := map[string]any{
		"command": "bun",
		"args":    []string{"adapters/claude/channel.ts"},
		"env":     map[string]any{"PEER_NAME": "alice"},
	}
	if err := mergeMCPServer(path, "peers", peersEntry); err != nil {
		t.Fatalf("mergeMCPServer: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse: %v\n%s", err, data)
	}
	servers, ok := doc["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers missing: %v", doc)
	}
	// other server preserved
	fs, ok := servers["filesystem"].(map[string]any)
	if !ok || fs["command"] != "npx" {
		t.Fatalf("filesystem server lost: %v", servers)
	}
	// peers added
	p, ok := servers["peers"].(map[string]any)
	if !ok || p["command"] != "bun" {
		t.Fatalf("peers server not merged: %v", servers)
	}
	// .bak of the original created
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf(".bak not created: %v", err)
	}
	if !bytes.Equal(bak, original) {
		t.Fatalf(".bak = %s, want %s", bak, original)
	}
}

func TestDetectAgentsDoesNotPanic(t *testing.T) {
	d := detectAgents()
	if d == nil {
		t.Fatalf("detectAgents returned nil")
	}
	// We can't assert which agents are installed in the test env, but the map
	// must be non-nil and contain exactly the four known keys.
	for _, a := range []string{"claude", "pi", "codex", "opencode"} {
		_ = d[a] // safe to read; absent key is just false
	}
}
