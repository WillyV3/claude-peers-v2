package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPeersParsesList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/peers" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `[{"agent":"alice","machine":"m1","cwd":"/a","last_seen":1,"online":true},`+
			`{"agent":"bob","machine":"m2","cwd":"/b","last_seen":2,"online":false}]`)
	}))
	defer srv.Close()

	peers, err := Peers(srv.URL)
	if err != nil {
		t.Fatalf("Peers: %v", err)
	}
	if len(peers) != 2 {
		t.Fatalf("Peers: want 2, got %d (%+v)", len(peers), peers)
	}
	if peers[0].Agent != "alice" || !peers[0].Online {
		t.Fatalf("Peers[0]: want alice/online, got %+v", peers[0])
	}
	if peers[1].Agent != "bob" || peers[1].Online {
		t.Fatalf("Peers[1]: want bob/offline, got %+v", peers[1])
	}
	if peers[0].Machine != "m1" || peers[1].Cwd != "/b" {
		t.Fatalf("Peers: fields not parsed: %+v", peers)
	}
}

func TestSendPostsBodyAndReturnsQueued(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/send" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content-type: want application/json, got %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"queued":true}`)
	}))
	defer srv.Close()

	queued, err := Send(srv.URL, "alice", "bob", "hello bob")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !queued {
		t.Fatalf("Send: want queued true, got false")
	}
	if got["from"] != "alice" || got["to"] != "bob" || got["content"] != "hello bob" {
		t.Fatalf("Send posted body: want from=alice,to=bob,content=hello bob, got %v", got)
	}
}

func TestSendSurfaces403AsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":"not paired","hint":"POST /pair {from,to} then have the target approve"}`)
	}))
	defer srv.Close()

	queued, err := Send(srv.URL, "carol", "bob", "unsolicited")
	if err == nil {
		t.Fatalf("Send: want error for 403, got nil (queued=%v)", queued)
	}
	if queued {
		t.Fatalf("Send: want queued false on 403, got true")
	}
	se, ok := err.(*StatusError)
	if !ok {
		t.Fatalf("Send: want *StatusError, got %T (%v)", err, err)
	}
	if se.Status != http.StatusForbidden {
		t.Fatalf("Send: want status 403, got %d", se.Status)
	}
	if !strings.Contains(se.Body, "not paired") {
		t.Fatalf("Send: want body containing hint, got %q", se.Body)
	}
}

func TestStreamParsesDataFrames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stream/tui" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		io.WriteString(w, ": connected\n\n")
		if fl != nil {
			fl.Flush()
		}
		io.WriteString(w, `data: {"id":1,"from":"alice","to":"tui","content":"hi","deliverAs":"steer","createdAt":100}`+"\n\n")
		if fl != nil {
			fl.Flush()
		}
	}))
	defer srv.Close()

	out := make(chan Message, 4)
	go Stream(srv.URL, "tui", out)

	msg, ok := <-out
	if !ok {
		t.Fatalf("Stream: channel closed before any message")
	}
	if msg.From != "alice" || msg.To != "tui" || msg.Content != "hi" || msg.DeliverAs != "steer" {
		t.Fatalf("Stream: unexpected message %+v", msg)
	}
}
