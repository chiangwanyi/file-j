// Package credentials stores SSH passwords and key passphrases.
//
// The application's persistent state lives in YAML, but secrets do not belong in
// a plain text file. Secrets are delegated to the OS secret store through a
// tiny platform adapter:
//
//   - macOS: the system Keychain via the `security(1)` tool (secret_darwin.go),
//   - Windows: the Credential Manager via CredWrite/CredRead (secret_windows.go),
//   - anything else: memory only.
//
// The YAML document only ever holds an opaque credential reference. The OS
// store is not a database - it is the OS secret store - which keeps the "no
// database" constraint and avoids plain text passwords on disk.
//
// Every secret is also mirrored in memory so an OS-store failure (locked
// keychain, headless CI, unsupported platform) degrades to "works for this run,
// asks again next time" instead of breaking the feature outright.
package credentials

import (
	"errors"
	"runtime"
	"sync"
)

// Store is a tiny secret store keyed by account name.
type Store struct {
	service string

	mu     sync.RWMutex
	mem    map[string]string
	usable bool
}

// New creates a store. service is the OS-store namespace, e.g. "com.example.filej".
func New(service string) *Store {
	return &Store{
		service: service,
		mem:     map[string]string{},
		usable:  osStoreAvailable(),
	}
}

// NewMemory returns a store that never touches the OS secret store. Used by
// tests and by development builds so running the test suite cannot pollute the
// user's Keychain / Credential Manager.
func NewMemory() *Store {
	return &Store{service: "file-j-memory", mem: map[string]string{}, usable: false}
}

// Available reports whether secrets can survive a restart.
func (s *Store) Available() bool { return s.usable }

// Platform names the OS secret store in use (for diagnostics).
func (s *Store) Platform() string {
	if !s.usable {
		return "memory"
	}
	return runtime.GOOS
}

// Set stores a secret under account. It always succeeds in memory; the returned
// error only reports that persistence was impossible.
func (s *Store) Set(account, secret string) error {
	if account == "" {
		return errors.New("empty credential account")
	}
	s.mu.Lock()
	s.mem[account] = secret
	s.mu.Unlock()

	if !s.usable {
		// Memory-only store: the secret works for this process but not after a
		// restart. Callers report that to the user via Available().
		return nil
	}
	return osStoreSet(s.service, account, secret)
}

// Get returns the secret for account.
func (s *Store) Get(account string) (string, bool) {
	s.mu.RLock()
	if v, ok := s.mem[account]; ok {
		s.mu.RUnlock()
		return v, true
	}
	s.mu.RUnlock()

	if !s.usable || account == "" {
		return "", false
	}
	secret, ok := osStoreGet(s.service, account)
	if !ok {
		return "", false
	}
	s.mu.Lock()
	s.mem[account] = secret
	s.mu.Unlock()
	return secret, true
}

// Delete removes a secret from both the cache and the OS store.
func (s *Store) Delete(account string) error {
	s.mu.Lock()
	delete(s.mem, account)
	s.mu.Unlock()

	if !s.usable || account == "" {
		return nil
	}
	// A missing item is not an error for the caller.
	_ = osStoreDelete(s.service, account)
	return nil
}
