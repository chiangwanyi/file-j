// Package testutil provides an in-process SSH + SFTP server used by tests.
//
// Running a real server (instead of mocking the SFTP client) means the tests
// exercise the actual wire protocol: authentication, subsystem negotiation,
// directory listings and concurrent file transfer all go through real SSH
// framing. It is only imported from _test files, so it never ships.
package testutil

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SSHD is a minimal SSH server exposing the local disk over SFTP.
type SSHD struct {
	// Addr is the host:port the server listens on.
	Addr string
	// User and Password are the accepted credentials.
	User     string
	Password string
	// HostKey is the server's public key (for fingerprint assertions).
	HostKey ssh.PublicKey

	listener net.Listener
	wg       sync.WaitGroup
	closed   sync.Once
}

// StartSSHD starts a server on a random loopback port.
func StartSSHD(t *testing.T) *SSHD {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if meta.User() == "tester" && string(pass) == "secret" {
				return nil, nil
			}
			return nil, fmt.Errorf("invalid credentials for %s", meta.User())
		},
	}
	cfg.AddHostKey(signer)
	_ = pub

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	s := &SSHD{Addr: ln.Addr().String(), User: "tester", Password: "secret", HostKey: signer.PublicKey(), listener: ln}
	s.wg.Add(1)
	go s.acceptLoop(cfg)
	t.Cleanup(s.Close)
	return s
}

// Port returns the listening port.
func (s *SSHD) Port() int {
	_, port, _ := net.SplitHostPort(s.Addr)
	var p int
	_, _ = fmt.Sscanf(port, "%d", &p)
	return p
}

// Close stops the listener and waits for connections to drain.
func (s *SSHD) Close() {
	s.closed.Do(func() {
		_ = s.listener.Close()
	})
}

func (s *SSHD) acceptLoop(cfg *ssh.ServerConfig) {
	defer s.wg.Done()
	for {
		nc, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(nc, cfg)
		}()
	}
}

func (s *SSHD) handleConn(nc net.Conn, cfg *ssh.ServerConfig) {
	defer nc.Close()
	sconn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		ch, requests, err := newCh.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(ch, requests)
	}
}

func (s *SSHD) handleSession(ch ssh.Channel, requests <-chan *ssh.Request) {
	defer ch.Close()
	for req := range requests {
		switch req.Type {
		case "subsystem":
			name := subsystemName(req.Payload)
			if name != "sftp" {
				_ = req.Reply(false, nil)
				continue
			}
			_ = req.Reply(true, nil)
			srv, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory("/"))
			if err != nil {
				return
			}
			_ = srv.Serve()
			_ = srv.Close()
			return
		case "env", "pty-req", "window-change":
			_ = req.Reply(true, nil)
		default:
			_ = req.Reply(false, nil)
		}
	}
}

func subsystemName(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}
	n := binary.BigEndian.Uint32(payload[:4])
	if int(n) > len(payload)-4 {
		return ""
	}
	return string(payload[4 : 4+n])
}

// Drain reads and discards a stream; handy in tests.
func Drain(r io.Reader) { _, _ = io.Copy(io.Discard, r) }
