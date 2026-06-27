package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func postJSON(t *testing.T, base, path string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		t.Fatalf("encode body: %v", err)
	}
	resp, err := http.Post(base+path, "application/json", &buf)
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response from %s: %v", path, err)
	}
	return resp.StatusCode, out
}

func getJSON(t *testing.T, base, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response from %s: %v", path, err)
	}
	return resp.StatusCode, out
}

func mustPost(t *testing.T, base, path string, body any) {
	t.Helper()
	status, out := postJSON(t, base, path, body)
	if status != http.StatusOK {
		t.Fatalf("post %s: status %d, body %v", path, status, out)
	}
}

func mustPair(t *testing.T, base, from, to string) string {
	t.Helper()
	status, out := postJSON(t, base, "/pair", map[string]string{"from": from, "to": to})
	if status != http.StatusOK {
		t.Fatalf("pair %s->%s: status %d, body %v", from, to, status, out)
	}
	code, ok := out["code"].(string)
	if !ok || code == "" {
		t.Fatalf("pair %s->%s: missing code in %v", from, to, out)
	}
	return code
}

func TestOfflineQueueDrain(t *testing.T) {
	tmp := t.TempDir()
	dsn := filepath.Join(tmp, "test.db") + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(3000)"
	b, err := NewBroker(dsn)
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}

	srv := httptest.NewServer(b)
	defer srv.Close()

	mustPost(t, srv.URL, "/register", map[string]string{"agent": "alice", "machine": "m1", "cwd": "/a"})
	mustPost(t, srv.URL, "/register", map[string]string{"agent": "bob", "machine": "m2", "cwd": "/b"})

	code := mustPair(t, srv.URL, "alice", "bob")
	mustPost(t, srv.URL, "/pair/approve", map[string]string{"owner": "bob", "code": code})

	mustPost(t, srv.URL, "/send", map[string]string{"from": "alice", "to": "bob", "content": "hello bob"})

	resp, err := http.Get(srv.URL + "/stream/bob")
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream status: %d", resp.StatusCode)
	}

	reader := bufio.NewReader(resp.Body)

	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read connected: %v", err)
	}
	if line != ": connected\n" {
		t.Fatalf("expected ': connected', got %q", line)
	}

	empty, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read empty line: %v", err)
	}
	if empty != "\n" {
		t.Fatalf("expected empty line, got %q", empty)
	}

	dataLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read data line: %v", err)
	}
	if !strings.HasPrefix(dataLine, "data: ") {
		t.Fatalf("expected data frame, got %q", dataLine)
	}
	var msg Message
	if err := json.Unmarshal([]byte(strings.TrimPrefix(dataLine, "data: ")), &msg); err != nil {
		t.Fatalf("decode message: %v", err)
	}
	if msg.From != "alice" || msg.To != "bob" || msg.Content != "hello bob" {
		t.Fatalf("unexpected message: %+v", msg)
	}

	_, err = reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read trailing newline: %v", err)
	}
}

func TestPairingRequired(t *testing.T) {
	tmp := t.TempDir()
	dsn := filepath.Join(tmp, "test.db") + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(3000)"
	b, err := NewBroker(dsn)
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}

	srv := httptest.NewServer(b)
	defer srv.Close()

	mustPost(t, srv.URL, "/register", map[string]string{"agent": "carol", "machine": "m3", "cwd": "/c"})
	mustPost(t, srv.URL, "/register", map[string]string{"agent": "bob", "machine": "m2", "cwd": "/b"})

	status, out := postJSON(t, srv.URL, "/send", map[string]string{"from": "carol", "to": "bob", "content": "unpaired"})
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", status)
	}
	if out["error"] != "not paired" {
		t.Fatalf("expected not paired error, got %v", out)
	}

	code := mustPair(t, srv.URL, "carol", "bob")
	mustPost(t, srv.URL, "/pair/approve", map[string]string{"owner": "bob", "code": code})

	status, out = postJSON(t, srv.URL, "/send", map[string]string{"from": "carol", "to": "bob", "content": "paired"})
	if status != http.StatusOK {
		t.Fatalf("expected 200 after pairing, got %d", status)
	}
	if queued, ok := out["queued"].(bool); !ok || !queued {
		t.Fatalf("expected queued true, got %v", out)
	}
}

func TestPairApproveFlow(t *testing.T) {
	tmp := t.TempDir()
	dsn := filepath.Join(tmp, "test.db") + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(3000)"
	b, err := NewBroker(dsn)
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}

	srv := httptest.NewServer(b)
	defer srv.Close()

	mustPost(t, srv.URL, "/register", map[string]string{"agent": "dave", "machine": "m4", "cwd": "/d"})
	mustPost(t, srv.URL, "/register", map[string]string{"agent": "eve", "machine": "m5", "cwd": "/e"})

	code := mustPair(t, srv.URL, "dave", "eve")
	if len(code) != 6 {
		t.Fatalf("pairing code length: expected 6, got %d (%q)", len(code), code)
	}
	for _, c := range code {
		if c == 'l' {
			t.Fatalf("pairing code contains ambiguous 'l': %q", code)
		}
	}

	status, out := getJSON(t, srv.URL, "/pairs/eve")
	if status != http.StatusOK {
		t.Fatalf("get pairs: status %d", status)
	}
	pending, ok := out["pending"].([]any)
	if !ok || len(pending) != 1 {
		t.Fatalf("expected 1 pending pair, got %v", out)
	}
	p, ok := pending[0].(map[string]any)
	if !ok || p["code"] != code || p["requester"] != "dave" {
		t.Fatalf("unexpected pending item: %v", pending[0])
	}

	mustPost(t, srv.URL, "/pair/approve", map[string]string{"owner": "eve", "code": code})

	status, out = getJSON(t, srv.URL, "/pairs/eve")
	if status != http.StatusOK {
		t.Fatalf("get pairs after approve: status %d", status)
	}
	pending, ok = out["pending"].([]any)
	if !ok || len(pending) != 0 {
		t.Fatalf("expected 0 pending pairs, got %v", out)
	}
	allowed, ok := out["allowed"].([]any)
	if !ok || len(allowed) != 1 {
		t.Fatalf("expected 1 allowed sender, got %v", out)
	}
	a, ok := allowed[0].(map[string]any)
	if !ok || a["sender"] != "dave" {
		t.Fatalf("unexpected allowed item: %v", allowed[0])
	}
}

func TestInvalidDeliverMode(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "test.db") + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(3000)"
	b, err := NewBroker(dsn)
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}
	srv := httptest.NewServer(b)
	defer srv.Close()

	mustPost(t, srv.URL, "/register", map[string]string{"agent": "alice", "cwd": "/a"})
	mustPost(t, srv.URL, "/register", map[string]string{"agent": "bob", "cwd": "/b"})
	code := mustPair(t, srv.URL, "alice", "bob")
	mustPost(t, srv.URL, "/pair/approve", map[string]string{"owner": "bob", "code": code})

	// A bogus deliverAs must be rejected before the message is stored.
	status, out := postJSON(t, srv.URL, "/send", map[string]string{
		"from": "alice", "to": "bob", "content": "x", "deliverAs": "bogus",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("bogus deliverAs: want 400, got %d (%v)", status, out)
	}

	// A valid mode still works.
	status, _ = postJSON(t, srv.URL, "/send", map[string]string{
		"from": "alice", "to": "bob", "content": "x", "deliverAs": "followUp",
	})
	if status != http.StatusOK {
		t.Fatalf("valid deliverAs: want 200, got %d", status)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
