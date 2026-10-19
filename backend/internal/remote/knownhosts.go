// Package remote owns SSH/SFTP connectivity. It deliberately delegates every
// protocol detail to golang.org/x/crypto/ssh (the reference Go SSH client) and
// github.com/pkg/sftp (the de-facto Go SFTP implementation) instead of
// re-implementing either protocol.
package remote

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/jiangwanyi/file-j/backend/internal/config"
)

// KnownHostStore is the subset of the config store needed for host key trust.
// *config.Store satisfies it; tests use an in-memory fake.
type KnownHostStore interface {
	KnownHost(hostport string) (h config.KnownHost, ok bool)
	PutKnownHost(h config.KnownHost) error
}

// HostKeyError is returned when a host presents an unexpected key. The UI turns
// this into the classic "host key changed" warning instead of a generic failure.
type HostKeyError struct {
	Host     string
	Expected string
	Actual   string
}

func (e *HostKeyError) Error() string {
	if e.Expected == "" {
		return fmt.Sprintf("主机 %s 的密钥未被信任（%s）", e.Host, e.Actual)
	}
	return fmt.Sprintf("主机 %s 的密钥已变更：期望 %s，实际 %s", e.Host, e.Expected, e.Actual)
}

// Policy values accepted from settings.
const (
	PolicyStrict   = "strict"
	PolicyTOFU     = "tofu"
	PolicyInsecure = "insecure"
)

// hostKeyCallback builds a callback implementing the configured trust policy.
//
//   - strict:   the key must already exist in known_hosts.yaml and match.
//   - tofu:     record the key on first contact, then require it to match.
//   - insecure: accept anything (opt-in, for throwaway lab machines).
func hostKeyCallback(policy string, store KnownHostStore, hostport, algorithmHint string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if policy == PolicyInsecure {
			return nil
		}
		actual := ssh.FingerprintSHA256(key)
		known, ok := store.KnownHost(hostport)
		if !ok {
			if policy == PolicyStrict {
				return &HostKeyError{Host: hostport, Actual: actual}
			}
			return store.PutKnownHost(config.KnownHost{
				Host:        hostport,
				Algorithm:   key.Type(),
				Fingerprint: actual,
				AddedAt:     time.Now(),
			})
		}
		if known.Fingerprint != actual {
			return &HostKeyError{Host: hostport, Expected: known.Fingerprint, Actual: actual}
		}
		return nil
	}
}
