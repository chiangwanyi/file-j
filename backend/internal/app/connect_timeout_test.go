package app

import (
	"net"
	"testing"
	"time"

	"github.com/jiangwanyi/file-j/backend/internal/config"
)

// TestSessionConnectTimeoutIsBounded pins the fix for the launch hang.
//
// A host that accepts the TCP connection but never sends an SSH banner (a
// firewalled port, a wedged sshd, a cellular link) used to hold the request
// until the IPC-wide 5 minute budget expired, because the SSH handshake
// inherited that context deadline. The splash screen waited with it. The handler
// now caps the handshake at remote.connectTimeoutSeconds + a small grace, so the
// pane can fall back to "not connected" while the user is still looking at it.
func TestSessionConnectTimeoutIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for a real socket timeout")
	}

	// Silent server: accept, never write the version banner, keep the socket.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	release := make(chan struct{})
	defer close(release)
	go func() {
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func() {
				<-release
				_ = conn.Close()
			}()
		}
	}()

	_, srv, _ := newTestApp(t)
	c := &rpcClient{t: t, conn: dial(t, srv, "test-token")}

	var cfg struct {
		Config config.Config `json:"config"`
	}
	if perr := c.call("config.get", nil, &cfg); perr != nil {
		t.Fatalf("config.get: %v", perr)
	}
	cfg.Config.Remote.ConnectTimeoutSeconds = 3
	if perr := c.call("config.set", cfg.Config, nil); perr != nil {
		t.Fatalf("config.set: %v", perr)
	}

	port := ln.Addr().(*net.TCPAddr).Port
	var saved struct {
		Session config.Session `json:"session"`
	}
	// Saved without a password on purpose: the credential store would touch the
	// OS keychain, and the password arrives with the connect call anyway.
	if perr := c.call("session.save", map[string]any{
		"session": config.Session{
			Name: "silent", Host: "127.0.0.1", Port: port, User: "nobody", AuthMethod: "password",
		},
	}, &saved); perr != nil {
		t.Fatalf("session.save: %v", perr)
	}

	started := time.Now()
	perr := c.call("session.connect", map[string]string{
		"id": saved.Session.ID, "password": "x",
	}, nil)
	elapsed := time.Since(started)

	if perr == nil {
		t.Fatal("connecting to a silent server should fail")
	}
	// The handshake is capped at 3s + 5s grace; anything approaching the old
	// 5 minute budget means the cap is gone.
	if elapsed > 20*time.Second {
		t.Errorf("connect took %s, want it capped near the 3s configured timeout", elapsed)
	}
}
