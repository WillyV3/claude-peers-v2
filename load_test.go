package main

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConcurrentLoad hammers the broker with N senders pairing + sending to one
// offline hub concurrently, then asserts the hub drains exactly N queued messages.
// Run with -race: this is the gate for the subscriber-map mutex and concurrent
// SQLite writes under load.
func TestConcurrentLoad(t *testing.T) {
	const N = 50
	dsn := filepath.Join(t.TempDir(), "load.db") + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)"
	b, err := NewBroker(dsn)
	if err != nil {
		t.Fatalf("new broker: %v", err)
	}
	srv := httptest.NewServer(b)
	defer srv.Close()

	mustPost(t, srv.URL, "/register", map[string]string{"agent": "hub", "cwd": "/"})

	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			from := fmt.Sprintf("s%d", i)
			code := mustPair(t, srv.URL, from, "hub")
			mustPost(t, srv.URL, "/pair/approve", map[string]string{"owner": "hub", "code": code})
			mustPost(t, srv.URL, "/send", map[string]string{
				"from": from, "to": "hub", "content": fmt.Sprintf("msg %d", i),
			})
		}(i)
	}
	wg.Wait()

	// Hub never streamed, so all N are queued. Drain and count.
	resp, err := http.Get(srv.URL + "/stream/hub")
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer resp.Body.Close()

	got := 0
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data: ") {
				got++
				if got == N {
					close(done)
					return
				}
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("drained %d/%d before timeout", got, N)
	}
}
