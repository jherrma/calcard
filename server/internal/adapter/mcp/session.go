package mcp

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

// Session is one client's negotiated connection state.
//
// It holds no data of its own — every tool re-reads through the repositories on
// each call — so a session is only the handshake result plus the identity it
// was opened for. UserID is the load-bearing field: a session id presented by a
// different token than the one that created it is refused, so a leaked session
// id cannot be used to act as its owner.
type Session struct {
	ID              string
	UserID          uint
	ProtocolVersion string
	ClientName      string
	CreatedAt       time.Time
	LastSeenAt      time.Time
}

const (
	// sessionTTL bounds how long an idle session id stays valid. Clients
	// re-initialize transparently, so expiring aggressively costs a round trip
	// and bounds the memory an abandoned client can pin.
	sessionTTL = 2 * time.Hour
	// sweepInterval is how often expired sessions are swept. The sweep runs
	// lazily on access rather than from a goroutine, so the store needs no
	// lifecycle management and cannot outlive the server it belongs to.
	sweepInterval = 10 * time.Minute
)

// SessionStore keeps live MCP sessions in memory.
//
// In-memory is the right scope: a session is worthless after a restart anyway
// (the client re-initializes), and persisting it would add a table whose only
// content is a handshake that costs one round trip to redo.
type SessionStore struct {
	mu        sync.Mutex
	sessions  map[string]*Session
	lastSweep time.Time
	now       func() time.Time // injectable for tests
}

func NewSessionStore() *SessionStore {
	return &SessionStore{
		sessions:  make(map[string]*Session),
		lastSweep: time.Now(),
		now:       time.Now,
	}
}

// Create opens a session for userID and returns it.
func (s *SessionStore) Create(userID uint, protocolVersion, clientName string) (*Session, error) {
	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)

	sess := &Session{
		ID:              id,
		UserID:          userID,
		ProtocolVersion: protocolVersion,
		ClientName:      clientName,
		CreatedAt:       now,
		LastSeenAt:      now,
	}
	s.sessions[id] = sess
	return sess, nil
}

// Get returns the live session with this id belonging to userID, touching its
// last-seen stamp. It returns nil when the id is unknown, expired, or owned by
// someone else — all three are the same answer to the client ("start over"),
// and collapsing them means a session id cannot be probed for existence across
// accounts.
func (s *SessionStore) Get(id string, userID uint) *Session {
	if id == "" {
		return nil
	}
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)

	sess, ok := s.sessions[id]
	if !ok || sess.UserID != userID {
		return nil
	}
	if now.Sub(sess.LastSeenAt) > sessionTTL {
		delete(s.sessions, id)
		return nil
	}
	sess.LastSeenAt = now
	return sess
}

// Delete terminates a session. Deleting someone else's id is a no-op rather
// than an error, so the endpoint cannot be used to close other users' sessions.
func (s *SessionStore) Delete(id string, userID uint) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[id]
	if !ok || sess.UserID != userID {
		return false
	}
	delete(s.sessions, id)
	return true
}

// Len reports the number of live sessions (used by tests).
func (s *SessionStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

// sweepLocked drops expired sessions. Callers must hold the mutex.
func (s *SessionStore) sweepLocked(now time.Time) {
	if now.Sub(s.lastSweep) < sweepInterval {
		return
	}
	s.lastSweep = now
	for id, sess := range s.sessions {
		if now.Sub(sess.LastSeenAt) > sessionTTL {
			delete(s.sessions, id)
		}
	}
}

// newSessionID mints a session identifier. The spec requires it to be globally
// unique and cryptographically secure, because it is presented as a header and
// a guessable one would be a session-fixation primitive.
func newSessionID() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
