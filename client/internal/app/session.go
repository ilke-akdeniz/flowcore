package app

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// NewSessionID mints an identifier for a visitor's own copy of the data.
//
// Sessions themselves live in the console's database now, not in memory, because
// the data they scope does. This is the only part of the old in-memory session
// store that survived the move.
func NewSessionID() string {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		// crypto/rand does not fail in practice, and a colliding session id is a
		// cosmetic problem rather than a correctness one, so falling back to the
		// clock beats refusing to serve the page.
		return "s" + time.Now().Format("150405.000000")
	}

	return "s" + hex.EncodeToString(raw)
}
