package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// onlineSeconds: a peer is "online" if seen within this window. Also the
// staleness cutoff for reclaiming an agent name from a session that went quiet.
const onlineSeconds int64 = 30

type Peer struct {
	Agent    string `json:"agent"`
	Machine  string `json:"machine"`
	Cwd      string `json:"cwd"`
	LastSeen int64  `json:"last_seen"`
	Online   bool   `json:"online"`
	Session  string `json:"session,omitempty"` // per-process id; distinguishes a reconnect from a name clash
}

// DeliverMode is how a message is injected into the recipient's agent runtime.
// Finite set; the named type keeps a typo'd mode from reaching an adapter.
type DeliverMode string

const (
	DeliverSteer    DeliverMode = "steer"    // mid-turn, before the next model call
	DeliverFollowUp DeliverMode = "followUp" // when the agent next goes idle
	DeliverNextTurn DeliverMode = "nextTurn" // queued for the next explicit prompt
)

func (d DeliverMode) valid() bool {
	switch d {
	case DeliverSteer, DeliverFollowUp, DeliverNextTurn:
		return true
	}
	return false
}

type Message struct {
	ID          int64       `json:"id"`
	From        string      `json:"from"`
	To          string      `json:"to"`
	Content     string      `json:"content"`
	DeliverAs   DeliverMode `json:"deliverAs"`
	CreatedAt   int64       `json:"createdAt"`
	DeliveredAt *int64      `json:"deliveredAt,omitempty"`
}

type Broker struct {
	*http.ServeMux // promotes ServeHTTP; Broker is its own handler
	db             *sql.DB
	mu             sync.Mutex
	subs           map[string][]chan *Message
}

func NewBroker(dsn string) (*Broker, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}

	schema := `
CREATE TABLE IF NOT EXISTS peers (
	agent TEXT PRIMARY KEY,
	machine TEXT,
	cwd TEXT,
	last_seen INTEGER NOT NULL,
	session TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	from_agent TEXT NOT NULL,
	to_agent TEXT NOT NULL,
	content TEXT NOT NULL,
	deliver_as TEXT NOT NULL DEFAULT 'steer',
	created_at INTEGER NOT NULL,
	delivered_at INTEGER
);
CREATE TABLE IF NOT EXISTS allowlist (
	owner TEXT NOT NULL,
	sender TEXT NOT NULL,
	since INTEGER NOT NULL,
	PRIMARY KEY(owner, sender)
);
CREATE TABLE IF NOT EXISTS pairings (
	code TEXT PRIMARY KEY,
	requester TEXT NOT NULL,
	owner TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
`
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}

	b := &Broker{
		ServeMux: http.NewServeMux(),
		db:       db,
		subs:     make(map[string][]chan *Message),
	}
	b.HandleFunc("POST /register", b.handleRegister)
	b.HandleFunc("POST /heartbeat", b.handleHeartbeat)
	b.HandleFunc("GET /peers", b.handlePeers)
	b.HandleFunc("POST /pair", b.handlePair)
	b.HandleFunc("POST /pair/approve", b.handlePairApprove)
	b.HandleFunc("GET /pairs/{owner}", b.handlePairs)
	b.HandleFunc("POST /send", b.handleSend)
	b.HandleFunc("GET /stream/{agent}", b.handleStream)
	b.HandleFunc("POST /ack", b.handleAck)
	return b, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func (b *Broker) handleRegister(w http.ResponseWriter, r *http.Request) {
	var p Peer
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if p.Agent == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "agent is required"})
		return
	}
	now := time.Now().Unix()

	// Reject taking a name another LIVE session already holds — stops two sessions
	// (e.g. both defaulting to the same name in one cwd) from silently sharing an
	// identity. A stale name (owner went quiet) or the same session reconnecting
	// may take it over.
	var exSession sql.NullString
	var exSeen int64
	switch err := b.db.QueryRow(`SELECT session, last_seen FROM peers WHERE agent = ?`, p.Agent).Scan(&exSession, &exSeen); err {
	case nil:
		live := now-exSeen < onlineSeconds
		if live && exSession.String != "" && exSession.String != p.Session {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "agent name in use by another live session",
				"hint":  "start with a unique PEER_NAME",
			})
			return
		}
	case sql.ErrNoRows:
		// new name — fall through to insert
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	if _, err := b.db.Exec(`
		INSERT INTO peers(agent, machine, cwd, last_seen, session)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(agent) DO UPDATE SET
			machine=excluded.machine, cwd=excluded.cwd,
			last_seen=excluded.last_seen, session=excluded.session`,
		p.Agent, p.Machine, p.Cwd, now, p.Session); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (b *Broker) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Agent string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	_, err := b.db.Exec(`UPDATE peers SET last_seen = ? WHERE agent = ?`, time.Now().Unix(), body.Agent)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (b *Broker) handlePeers(w http.ResponseWriter, r *http.Request) {
	rows, err := b.db.Query(`SELECT agent, machine, cwd, last_seen FROM peers`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()

	now := time.Now().Unix()
	var peers []Peer
	for rows.Next() {
		var p Peer
		if err := rows.Scan(&p.Agent, &p.Machine, &p.Cwd, &p.LastSeen); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		p.Online = (now-p.LastSeen) < onlineSeconds
		peers = append(peers, p)
	}
	writeJSON(w, http.StatusOK, peers)
}

func (b *Broker) handleSend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From      string      `json:"from"`
		To        string      `json:"to"`
		Content   string      `json:"content"`
		DeliverAs DeliverMode `json:"deliverAs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if body.From == "" || body.To == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "from and to are required"})
		return
	}
	if body.DeliverAs == "" {
		body.DeliverAs = DeliverSteer
	}
	if !body.DeliverAs.valid() {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid deliverAs",
			"hint":  "steer|followUp|nextTurn",
		})
		return
	}
	if !b.isAllowed(body.From, body.To) {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "not paired",
			"hint":  "POST /pair {from,to} then have the target approve",
		})
		return
	}
	createdAt := time.Now().Unix()

	res, err := b.db.Exec(`
		INSERT INTO messages(from_agent, to_agent, content, deliver_as, created_at, delivered_at)
		VALUES (?, ?, ?, ?, ?, NULL)`,
		body.From, body.To, body.Content, body.DeliverAs, createdAt)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	msg := &Message{
		ID:        id,
		From:      body.From,
		To:        body.To,
		Content:   body.Content,
		DeliverAs: body.DeliverAs,
		CreatedAt: createdAt,
	}

	// queued = no live subscriber accepted it now; it persists until the recipient
	// connects and drains. delivered_at is set only after a successful stream write
	// (see deliver), so a client dropping mid-write never silently loses the message.
	livePickup := b.fanout(body.To, msg)
	writeJSON(w, http.StatusOK, map[string]any{"queued": !livePickup})
}

func (b *Broker) fanout(to string, msg *Message) bool {
	b.mu.Lock()
	chs := b.subs[to]
	b.mu.Unlock()

	delivered := false
	for _, ch := range chs {
		select {
		case ch <- msg:
			delivered = true
		default:
		}
	}
	return delivered
}

func (b *Broker) addSub(agent string, ch chan *Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[agent] = append(b.subs[agent], ch)
}

func (b *Broker) removeSub(agent string, ch chan *Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	list := b.subs[agent]
	for i, c := range list {
		if c == ch {
			b.subs[agent] = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(b.subs[agent]) == 0 {
		delete(b.subs, agent)
	}
}

func (b *Broker) handleStream(w http.ResponseWriter, r *http.Request) {
	agent := r.PathValue("agent")
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	ch := make(chan *Message, 64)
	b.addSub(agent, ch)
	defer b.removeSub(agent, ch)

	if err := b.drain(agent, w, flusher); err != nil {
		log.Printf("drain error: %v", err)
		return
	}

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if err := b.deliver(w, flusher, msg); err != nil {
				log.Printf("deliver: %v", err)
				return
			}
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (b *Broker) drain(agent string, w http.ResponseWriter, flusher http.Flusher) error {
	rows, err := b.db.Query(`
		SELECT id, from_agent, to_agent, content, deliver_as, created_at
		FROM messages
		WHERE to_agent = ? AND delivered_at IS NULL
		ORDER BY id ASC`, agent)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.From, &msg.To, &msg.Content, &msg.DeliverAs, &msg.CreatedAt); err != nil {
			return err
		}
		if err := b.deliver(w, flusher, &msg); err != nil {
			return err
		}
	}
	return rows.Err()
}

// deliver writes a message to the stream and marks it delivered ONLY on a
// successful write. If the client drops mid-write, delivered_at stays NULL and
// the message re-drains on the next connect — at-least-once, never silently lost.
func (b *Broker) deliver(w http.ResponseWriter, flusher http.Flusher, msg *Message) error {
	if err := b.writeMessage(w, flusher, msg); err != nil {
		return err
	}
	_, err := b.db.Exec(`UPDATE messages SET delivered_at = ? WHERE id = ?`, time.Now().Unix(), msg.ID)
	return err
}

func (b *Broker) writeMessage(w http.ResponseWriter, flusher http.Flusher, msg *Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func (b *Broker) handleAck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func serve() error {
	addr := os.Getenv("CPV2_ADDR")
	if addr == "" {
		addr = "127.0.0.1:7900"
	}
	dbPath := os.Getenv("CPV2_DB")
	if dbPath == "" {
		dbPath = "./broker.db"
	}
	dsn := dbPath + "?_pragma=journal_mode(wal)&_pragma=busy_timeout(3000)"

	b, err := NewBroker(dsn)
	if err != nil {
		return fmt.Errorf("open broker: %w", err)
	}

	server := &http.Server{
		Addr:    addr,
		Handler: b,
	}
	log.Printf("broker listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

func main() {
	sub := "serve"
	if len(os.Args) > 1 {
		sub = os.Args[1]
	}
	switch sub {
	case "serve":
		if err := serve(); err != nil {
			log.Fatal(err)
		}
	case "send":
		os.Exit(cmdSend(os.Args[2:]))
	case "pair":
		os.Exit(cmdPair(os.Args[2:]))
	case "approve":
		os.Exit(cmdApprove(os.Args[2:]))
	case "peers":
		os.Exit(cmdPeers(os.Args[2:]))
	case "statusline":
		os.Exit(cmdStatusLine(os.Args[2:]))
	case "setup":
		os.Exit(cmdSetup(os.Args[2:]))
	case "run":
		os.Exit(cmdRun(os.Args[2:]))
	default:
		fmt.Fprintln(os.Stderr, "usage: cpv2 <serve|send|pair|approve|peers|statusline|setup|run> [options]")
		os.Exit(2)
	}
}
