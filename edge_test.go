package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestSrv(t *testing.T) *httptest.Server {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "edge.db") + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(3000)"
	b, err := NewBroker(dsn)
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	return srv
}

// Empty from/to must be rejected, not stored as a ghost message.
func TestSendRequiresFromTo(t *testing.T) {
	srv := newTestSrv(t)
	for _, body := range []map[string]string{
		{"to": "bob", "content": "x"},   // missing from
		{"from": "a", "content": "x"},   // missing to
		{"content": "x"},                // missing both
	} {
		status, _ := postJSON(t, srv.URL, "/send", body)
		if status != http.StatusBadRequest {
			t.Fatalf("send %v: want 400, got %d", body, status)
		}
	}
}

// An agent may always message itself without pairing (self==self allowed).
func TestSelfSendAllowed(t *testing.T) {
	srv := newTestSrv(t)
	mustPost(t, srv.URL, "/register", map[string]string{"agent": "solo", "cwd": "/"})
	status, out := postJSON(t, srv.URL, "/send", map[string]string{
		"from": "solo", "to": "solo", "content": "note to self",
	})
	if status != http.StatusOK {
		t.Fatalf("self-send: want 200, got %d (%v)", status, out)
	}
}

// Two live sessions must not silently share an agent name (the same-cwd bug).
// A different session gets 409; the same session reconnecting is fine.
func TestDuplicateNameRejected(t *testing.T) {
	srv := newTestSrv(t)
	if st, _ := postJSON(t, srv.URL, "/register", map[string]string{"agent": "dup", "cwd": "/", "session": "A"}); st != http.StatusOK {
		t.Fatalf("first register: %d", st)
	}
	if st, _ := postJSON(t, srv.URL, "/register", map[string]string{"agent": "dup", "cwd": "/", "session": "B"}); st != http.StatusConflict {
		t.Fatalf("clashing live session: want 409, got %d", st)
	}
	if st, _ := postJSON(t, srv.URL, "/register", map[string]string{"agent": "dup", "cwd": "/", "session": "A"}); st != http.StatusOK {
		t.Fatalf("same-session reconnect: want 200, got %d", st)
	}
}

// A message to an offline agent stays undelivered (delivered_at NULL) until a
// stream connects — it is never marked delivered on send. Guards the message-loss fix.
func TestOfflineStaysUndeliveredUntilConnect(t *testing.T) {
	srv := newTestSrv(t)
	mustPost(t, srv.URL, "/register", map[string]string{"agent": "rx", "cwd": "/"})
	code := mustPair(t, srv.URL, "tx", "rx")
	mustPost(t, srv.URL, "/pair/approve", map[string]string{"owner": "rx", "code": code})
	status, out := postJSON(t, srv.URL, "/send", map[string]string{"from": "tx", "to": "rx", "content": "later"})
	if status != http.StatusOK {
		t.Fatalf("send: %d %v", status, out)
	}
	if queued, _ := out["queued"].(bool); !queued {
		t.Fatalf("send to offline rx should report queued=true, got %v", out)
	}
}
