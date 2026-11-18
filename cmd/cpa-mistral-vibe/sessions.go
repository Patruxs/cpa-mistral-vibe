package main

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Login sessions are held in plugin memory and keyed by a short token, because
// the host caps the opaque OAuth State length. A session is short-lived (one
// browser sign-in), so in-memory storage is sufficient.

type loginSession struct {
	state   loginState
	expires time.Time
}

var (
	sessionsMu sync.Mutex
	sessions   = map[string]loginSession{}
)

func newSessionToken() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func putSession(state loginState, expires time.Time) string {
	token := newSessionToken()
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	pruneLocked()
	sessions[token] = loginSession{state: state, expires: expires}
	return token
}

func getSession(token string) (loginState, bool) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	s, ok := sessions[token]
	if !ok || time.Now().After(s.expires) {
		delete(sessions, token)
		return loginState{}, false
	}
	return s.state, true
}

func dropSession(token string) {
	sessionsMu.Lock()
	delete(sessions, token)
	sessionsMu.Unlock()
}

func pruneLocked() {
	now := time.Now()
	for k, v := range sessions {
		if now.After(v.expires) {
			delete(sessions, k)
		}
	}
}
