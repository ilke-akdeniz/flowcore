package app

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Session is one visitor's private slice of the world.
//
// It exists because hosting means concurrent visitors sharing one database, and
// without isolation the first person to delete a seeded workflow ruins it for
// everyone after them. A reset button does not fix that — it only undoes damage
// while causing more, wiping out whatever an active visitor was doing.
//
// Every field here is the client's own bookkeeping. FlowCore has no notion of a
// session, no tenant column, and no opinion about any of this, which is the point:
// `CLAUDE.md` names tenant_id as its canonical example of structure with no
// caller, and this is the caller arriving and not needing it.
type Session struct {
	ID        string
	CreatedAt time.Time
	LastSeen  time.Time

	// DefinitionIDs are the workflow definitions this session created.
	//
	// The client has to track these regardless, which is a happy accident rather
	// than a design: Catalog has no List method, only Get(id), so there is no way
	// to ask the library "what definitions exist" — and therefore no way to
	// accidentally show one session another's work.
	DefinitionIDs []uuid.UUID

	// ActingAs is the roster member this visitor is currently acting as. It is
	// the client's notion of "signed in", and FlowCore never learns it — only the
	// opaque reference that reaches completedBy.
	ActingAs string

	// Releases is the subject store. FlowCore holds only an opaque reference like
	// "s7f3a2:release:v2.4.0" and never the release itself, so somebody has to,
	// and that somebody is the client.
	Releases map[string]Release
}

// Release is a subject: the thing a workflow is about.
//
// It never reaches FlowCore. The library stores the reference string and the
// version token, and everything below stays here — which is what "not a document
// store" means in practice.
type Release struct {
	Version   string
	Commit    string
	Title     string
	Changelog string
	DiffStat  string
}

// SubjectReference is what FlowCore records for this release: opaque to the
// library, and prefixed with the session so two visitors working the same seeded
// release have two separate runs.
func (s *Session) SubjectReference(version string) string {
	return s.ID + ":release:" + version
}

// SessionStore holds live sessions in memory.
//
// In memory on purpose: sessions are demonstration state, not records. Losing
// them on restart costs a visitor a reseed, and persisting them would mean a
// second storage story that teaches nothing about the library.
type SessionStore struct {
	mutex    sync.Mutex
	sessions map[string]*Session
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*Session)}
}

// Get returns the session with this id, and whether it existed.
func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	session, ok := s.sessions[id]
	if ok {
		session.LastSeen = time.Now()
	}

	return session, ok
}

// Create makes a new session with a fresh id.
func (s *SessionStore) Create() *Session {
	now := time.Now()
	session := &Session{
		ID:        newSessionID(),
		CreatedAt: now,
		LastSeen:  now,
		ActingAs:  Roster[0].Reference,
		Releases:  make(map[string]Release),
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.sessions[session.ID] = session

	return session
}

// Expired returns the sessions idle for longer than ttl, and forgets them. A zero
// ttl returns nothing, which is how "never expire" is expressed.
//
// It returns them rather than deleting their data itself: the definitions belong
// to FlowCore, so removing them is a Catalog call the caller makes.
func (s *SessionStore) Expired(ttl time.Duration) []*Session {
	if ttl == 0 {
		return nil
	}

	cutoff := time.Now().Add(-ttl)

	s.mutex.Lock()
	defer s.mutex.Unlock()

	var expired []*Session
	for id, session := range s.sessions {
		if session.LastSeen.Before(cutoff) {
			expired = append(expired, session)
			delete(s.sessions, id)
		}
	}

	return expired
}

func newSessionID() string {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		// crypto/rand does not fail in practice, and a session id that collides
		// is a cosmetic problem rather than a correctness one, so falling back to
		// the clock is better than refusing to serve the page.
		return "s" + time.Now().Format("150405.000000")
	}

	return "s" + hex.EncodeToString(raw)
}
