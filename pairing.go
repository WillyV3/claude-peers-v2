package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"math/big"
	"net/http"
	"time"
)

const codeAlphabet = "abcdefghijkmnopqrstuvwxyz"

func generatePairingCode() string {
	max := big.NewInt(int64(len(codeAlphabet)))
	code := make([]byte, 6)
	for i := range code {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		code[i] = codeAlphabet[n.Int64()]
	}
	return string(code)
}

func (b *Broker) isAllowed(sender, owner string) bool {
	if sender == owner {
		return true
	}
	var count int
	err := b.db.QueryRow(`
		SELECT COUNT(*) FROM allowlist
		WHERE owner = ? AND sender = ?`, owner, sender).Scan(&count)
	return err == nil && count > 0
}

func (b *Broker) handlePair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	var code string
	for i := 0; i < 10; i++ {
		code = generatePairingCode()
		_, err := b.db.Exec(`
			INSERT INTO pairings(code, requester, owner, created_at)
			VALUES (?, ?, ?, ?)`,
			code, body.From, body.To, time.Now().Unix())
		if err == nil {
			break
		}
		code = ""
	}
	if code == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "could not generate pairing code"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": code})
}

func (b *Broker) handlePairApprove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Owner string `json:"owner"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	var requester string
	var owner string
	err := b.db.QueryRow(`
		SELECT requester, owner FROM pairings WHERE code = ?`, body.Code).Scan(&requester, &owner)
	if err == sql.ErrNoRows || owner != body.Owner {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "no such pending pairing"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	tx, err := b.db.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO allowlist(owner, sender, since)
		VALUES (?, ?, ?)
		ON CONFLICT(owner, sender) DO NOTHING`,
		owner, requester, time.Now().Unix()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM pairings WHERE code = ?`, body.Code); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "paired": requester})
}

type pendingPairing struct {
	Code      string `json:"code"`
	Requester string `json:"requester"`
	CreatedAt int64  `json:"createdAt"`
}

type allowedSender struct {
	Sender string `json:"sender"`
	Since  int64  `json:"since"`
}

func (b *Broker) handlePairs(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")

	pendingRows, err := b.db.Query(`
		SELECT code, requester, created_at FROM pairings WHERE owner = ?`, owner)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer pendingRows.Close()

	pending := []pendingPairing{}
	for pendingRows.Next() {
		var p pendingPairing
		if err := pendingRows.Scan(&p.Code, &p.Requester, &p.CreatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		pending = append(pending, p)
	}
	if err := pendingRows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	allowedRows, err := b.db.Query(`
		SELECT sender, since FROM allowlist WHERE owner = ?`, owner)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer allowedRows.Close()

	allowed := []allowedSender{}
	for allowedRows.Next() {
		var a allowedSender
		if err := allowedRows.Scan(&a.Sender, &a.Since); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		allowed = append(allowed, a)
	}
	if err := allowedRows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"pending": pending,
		"allowed": allowed,
	})
}
