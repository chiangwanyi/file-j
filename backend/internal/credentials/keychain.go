// Package credentials stores SSH passwords and key passphrases.
//
// The application's persistent state lives in YAML, but secrets do not belong in
// a plain text file. On macOS we delegate to the system Keychain through the
// `security(1)` tool, and the YAML document only ever holds an opaque
// credential reference. The Keychain is not a database - it is the OS secret
// store - which keeps the "no database" constraint and avoids plain text
// passwords on disk.
//
// Every secret is also mirrored in memory so a Keychain failure (locked
// keychain, headless CI, non-macOS dev machine) degrades to "works for this run,
// asks again next time" instead of breaking the feature outright.
package credentials

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// Store is a tiny secret store keyed by account name.
type Store struct {
	service string

	mu     sync.RWMutex
	mem    map[string]string
	usable bool
}

// New creates a store. service is the Keychain service name, e.g. "com.example.filej".
func New(service string) *Store {
	return &Store{
		service: service,
		mem:     map[string]string{},
		usable:  runtime.GOOS == "darwin",
	}
}

// NewMemory returns a store that never touches the Keychain. Used by tests and
// by development builds so running the test suite cannot pollute the user's
// Keychain.
func NewMemory() *Store {
	return &Store{service: "file-j-memory", mem: map[string]string{}, usable: false}
}

// Available reports whether secrets can survive a restart.
func (s *Store) Available() bool { return s.usable }

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
	// -U updates an existing item, -A avoids an access prompt when the app is
	// rebuilt and its code signature changes.
	out, err := exec.Command("security", "add-generic-password",
		"-U", "-A",
		"-s", s.service,
		"-a", account,
		"-w", secret,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("写入钥匙串失败: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
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
	out, err := exec.Command("security", "find-generic-password",
		"-s", s.service, "-a", account, "-w",
	).Output()
	if err != nil {
		return "", false
	}
	secret := strings.TrimRight(string(out), "\n")
	s.mu.Lock()
	s.mem[account] = secret
	s.mu.Unlock()
	return secret, true
}

// Delete removes a secret from both the cache and the Keychain.
func (s *Store) Delete(account string) error {
	s.mu.Lock()
	delete(s.mem, account)
	s.mu.Unlock()

	if !s.usable || account == "" {
		return nil
	}
	cmd := exec.Command("security", "delete-generic-password", "-s", s.service, "-a", account)
	if err := cmd.Run(); err != nil {
		// A missing item is not an error for the caller.
		return nil
	}
	return nil
}
