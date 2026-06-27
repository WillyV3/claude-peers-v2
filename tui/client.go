package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Peer mirrors the broker's GET /peers JSON shape.
type Peer struct {
	Agent    string `json:"agent"`
	Machine  string `json:"machine"`
	Cwd      string `json:"cwd"`
	LastSeen int64  `json:"last_seen"`
	Online   bool   `json:"online"`
}

// Message mirrors the broker's SSE data: payload shape.
type Message struct {
	ID          int64  `json:"id"`
	From        string `json:"from"`
	To          string `json:"to"`
	Content     string `json:"content"`
	DeliverAs   string `json:"deliverAs"`
	CreatedAt   int64  `json:"createdAt"`
	DeliveredAt *int64 `json:"deliveredAt,omitempty"`
}

// StatusError is returned by client funcs when the broker responds with a
// non-2xx status. Carries the code + body so the TUI can branch on 403
// (not paired) and surface the broker's hint.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("broker status %d: %s", e.Status, e.Body)
}

var (
	// pollClient is for short request/response calls (Peers, Send).
	pollClient = &http.Client{Timeout: 10 * time.Second}
	// streamClient has no timeout: the SSE stream is long-lived.
	streamClient = &http.Client{}
)

// Peers fetches the registered peer list from the broker.
func Peers(base string) ([]Peer, error) {
	resp, err := pollClient.Get(base + "/peers")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var peers []Peer
	if err := json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		return nil, err
	}
	return peers, nil
}

// Send posts a message. On 2xx it returns the broker's queued flag. On a
// non-2xx response (e.g. 403 not paired) it returns a *StatusError so the
// caller can branch on the code.
func Send(base, from, to, content string) (bool, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]string{
		"from":    from,
		"to":      to,
		"content": content,
	}); err != nil {
		return false, err
	}
	resp, err := pollClient.Post(base+"/send", "application/json", &buf)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, &StatusError{Status: resp.StatusCode, Body: string(data)}
	}
	var out struct {
		Queued bool `json:"queued"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return false, err
	}
	return out.Queued, nil
}

// Stream opens the SSE endpoint GET /stream/{me} and pumps decoded Messages
// into out. It blocks until the connection closes or errors, then closes out.
// Intended to run inside a tea.Cmd.
func Stream(base, me string, out chan<- Message) {
	defer close(out)
	resp, err := streamClient.Get(base + "/stream/" + url.PathEscape(me))
	if err != nil {
		return
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var msg Message
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &msg); err != nil {
			continue
		}
		out <- msg
	}
}
