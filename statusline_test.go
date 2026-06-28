package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStatusLineStates(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
		peer   string
		cwd    string
		want   string
	}{
		{
			name: "co-located peer warns (occupied cwd)",
			body: `[{"agent":"alice","online":true,"cwd":"/work"},{"agent":"jim","online":true,"cwd":"/work"}]`,
			peer: "alice",
			cwd:  "/work",
			want: "● peers: alice · 2 online · ⚠ also here: jim",
		},
		{
			name: "peer in a different cwd does not warn",
			body: `[{"agent":"alice","online":true,"cwd":"/work"},{"agent":"jim","online":true,"cwd":"/other"}]`,
			peer: "alice",
			cwd:  "/work",
			want: "● peers: alice · 2 online",
		},
		{
			name: "online",
			body: `[{"agent":"alice","online":true},{"agent":"bob","online":true}]`,
			peer: "alice",
			want: "● peers: alice · 2 online",
		},
		{
			name: "self online counts all online peers",
			body: `[{"agent":"alice","online":true},{"agent":"bob","online":false}]`,
			peer: "alice",
			want: "● peers: alice · 1 online",
		},
		{
			name: "offline",
			body: `[{"agent":"alice","online":false},{"agent":"bob","online":true}]`,
			peer: "alice",
			want: "◌ peers: alice · offline",
		},
		{
			name: "not registered",
			body: `[{"agent":"bob","online":true}]`,
			peer: "alice",
			want: "○ peers: alice · not registered",
		},
		{
			name:   "non-200 status is broker down",
			status: http.StatusInternalServerError,
			peer:   "alice",
			want:   "○ peers: broker down",
		},
		{
			name: "garbage body is broker down",
			body: `not json`,
			peer: "alice",
			want: "○ peers: broker down",
		},
		{
			name: "empty peer list is not registered",
			body: `[]`,
			peer: "alice",
			want: "○ peers: alice · not registered",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status := c.status
			if status == 0 {
				status = http.StatusOK
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/peers" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(c.body))
			}))
			defer srv.Close()
			got := statusLine(srv.URL, c.peer, c.cwd)
			if got != c.want {
				t.Fatalf("statusLine(%q) = %q, want %q", c.peer, got, c.want)
			}
		})
	}
}

func TestStatusLineBrokerDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // a closed server -> connection refused, not a timeout
	got := statusLine(srv.URL, "alice", "")
	if got != "○ peers: broker down" {
		t.Fatalf("got %q, want broker down", got)
	}
}

// TestStatusLineTimeout proves the 500ms cap: a server that hangs must not
// block the statusline, and we must report broker down quickly.
func TestStatusLineTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()
	start := time.Now()
	got := statusLine(srv.URL, "alice", "")
	elapsed := time.Since(start)
	if got != "○ peers: broker down" {
		t.Fatalf("got %q, want broker down", got)
	}
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("statusline hung for %v; should cap at ~500ms", elapsed)
	}
}

func TestStatusLineNoName(t *testing.T) {
	got := statusLine("http://127.0.0.1:7900", "", "")
	if got != "○ peers: no PEER_NAME set" {
		t.Fatalf("got %q, want no PEER_NAME set", got)
	}
}
