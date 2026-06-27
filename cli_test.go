package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestBroker(t *testing.T) (*Broker, string) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "test.db") + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(3000)"
	b, err := NewBroker(dsn)
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	return b, srv.URL
}

func registerPeer(t *testing.T, base, agent, machine, cwd string) {
	t.Helper()
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]string{
		"agent": agent, "machine": machine, "cwd": cwd,
	}); err != nil {
		t.Fatalf("encode register: %v", err)
	}
	resp, err := http.Post(base+"/register", "application/json", &buf)
	if err != nil {
		t.Fatalf("register %s: %v", agent, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("register %s: status %d", agent, resp.StatusCode)
	}
}

func TestClientPairApproveSend(t *testing.T) {
	_, base := newTestBroker(t)

	code, status, err := clientPair(base, "alice", "bob")
	if err != nil {
		t.Fatalf("clientPair: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("clientPair status: want 200, got %d", status)
	}
	if code == "" {
		t.Fatalf("clientPair returned empty code")
	}

	astatus, _, err := clientApprove(base, "bob", code)
	if err != nil {
		t.Fatalf("clientApprove: %v", err)
	}
	if astatus != http.StatusOK {
		t.Fatalf("clientApprove status: want 200, got %d", astatus)
	}

	sstatus, _, err := clientSend(base, "alice", "bob", DeliverSteer, "hello bob")
	if err != nil {
		t.Fatalf("clientSend: %v", err)
	}
	if sstatus != http.StatusOK {
		t.Fatalf("clientSend status: want 200, got %d", sstatus)
	}
}

func TestClientSendUnpaired(t *testing.T) {
	_, base := newTestBroker(t)

	status, _, err := clientSend(base, "carol", "bob", DeliverSteer, "unsolicited")
	if err != nil {
		t.Fatalf("clientSend: %v", err)
	}
	if status == http.StatusOK {
		t.Fatalf("unpaired send should not succeed, got 200")
	}
	if status != http.StatusForbidden {
		t.Fatalf("unpaired send: want 403, got %d", status)
	}
}

func TestClientPeers(t *testing.T) {
	_, base := newTestBroker(t)
	registerPeer(t, base, "alice", "m1", "/a")
	registerPeer(t, base, "bob", "m2", "/b")

	peers, status, err := clientPeers(base)
	if err != nil {
		t.Fatalf("clientPeers: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("clientPeers status: want 200, got %d", status)
	}
	if len(peers) != 2 {
		t.Fatalf("clientPeers: want 2 peers, got %d", len(peers))
	}
	got := map[string]bool{}
	for _, p := range peers {
		got[p.Agent] = true
	}
	if !got["alice"] || !got["bob"] {
		t.Fatalf("clientPeers: missing alice/bob in %v", peers)
	}
}
