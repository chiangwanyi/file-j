package config

import (
	"crypto/rand"
	"encoding/hex"
)

// newID returns a short random identifier used for sessions and transfer tasks.
// It is deliberately not a UUID: 16 hex chars are plenty for a desktop app and
// keep the YAML readable.
func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
