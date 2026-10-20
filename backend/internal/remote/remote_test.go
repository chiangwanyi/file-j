package remote

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jiangwanyi/file-j/backend/internal/config"
	"github.com/jiangwanyi/file-j/backend/internal/credentials"
	"github.com/jiangwanyi/file-j/backend/internal/testutil"
)

type fixture struct {
	mgr       *Manager
	store     *config.Store
	sessionID string
	server    *testutil.SSHD
}

func newFixture(t *testing.T, policy string) *fixture {
	t.Helper()
	server := testutil.StartSSHD(t)
	dir := t.TempDir()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}
	cfg := store.Config()
	cfg.Remote.HostKeyPolicy = policy
	cfg.Remote.KeepAliveSeconds = 5
	cfg.Remote.ConnectTimeoutSeconds = 5
	if _, err := store.SetConfig(cfg); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	creds := credentials.NewMemory()
	sess, err := store.SaveSession(config.Session{
		Name: "test", Host: "127.0.0.1", Port: server.Port(),
		User: server.User, AuthMethod: "password", CredentialRef: "session/test",
	})
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := creds.Set("session/test", server.Password); err != nil {
		t.Fatalf("creds.Set: %v", err)
	}

	mgr := NewManager(store, creds, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(mgr.Close)
	return &fixture{mgr: mgr, store: store, sessionID: sess.ID, server: server}
}

func TestConnectAndBrowse(t *testing.T) {
	f := newFixture(t, PolicyInsecure)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := f.mgr.Connect(ctx, f.sessionID, "")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if conn.SFTP == nil || conn.SSH == nil {
		t.Fatal("connection is missing clients")
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello sftp"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := conn.List(ctx, root, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("entries = %d, want 2 (%+v)", len(res.Entries), res.Entries)
	}
	// Remote paths are POSIX on the wire even when the test server runs on
	// Windows, so the expectation is the POSIX parent of the normalised root.
	if want := path.Dir(toPosixPath(root)); res.Parent != want {
		t.Errorf("parent = %q, want %q", res.Parent, want)
	}

	file, err := conn.Stat(ctx, filepath.Join(root, "hello.txt"))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if file.IsDir || file.Size != int64(len("hello sftp")) {
		t.Errorf("stat = %+v", file)
	}
	if len(file.Mode) != 10 {
		t.Errorf("mode = %q, want ls style", file.Mode)
	}

	// mkdir / write / read / rename / remove round trip
	sub := filepath.Join(root, "nested", "deep")
	if err := conn.MkdirAll(ctx, sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	w, err := conn.Create(ctx, filepath.Join(sub, "a.bin"), 0o600, 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Write([]byte("payload")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	rc, err := conn.Open(ctx, filepath.Join(sub, "a.bin"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, _ := io.ReadAll(rc)
	rc.Close()
	if string(data) != "payload" {
		t.Errorf("read back %q", data)
	}

	if err := conn.Rename(ctx, filepath.Join(sub, "a.bin"), filepath.Join(sub, "b.bin")); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := conn.Stat(ctx, filepath.Join(sub, "b.bin")); err != nil {
		t.Errorf("renamed file missing: %v", err)
	}
	if err := conn.Remove(ctx, filepath.Join(root, "nested")); err != nil {
		t.Fatalf("Remove tree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "nested")); !errors.Is(err, os.ErrNotExist) {
		t.Error("directory still exists after Remove")
	}
}

func TestHostKeyTOFUAndMismatch(t *testing.T) {
	// strict refuses an unknown host outright.
	strict := newFixture(t, PolicyStrict)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := strict.mgr.Connect(ctx, strict.sessionID, ""); err == nil {
		t.Fatal("strict policy accepted an unknown host key")
	} else {
		var hk *HostKeyError
		if !errors.As(err, &hk) {
			t.Fatalf("error = %v, want HostKeyError", err)
		}
	}

	// tofu records the key, then accepts on the next attempt.
	tofu := newFixture(t, PolicyTOFU)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	if _, err := tofu.mgr.Connect(ctx2, tofu.sessionID, ""); err != nil {
		t.Fatalf("tofu connect: %v", err)
	}
	hostport := "127.0.0.1:" + strconv.Itoa(tofu.server.Port())
	known, ok := tofu.store.KnownHost(hostport)
	if !ok {
		t.Fatalf("tofu did not record host key for %s (known=%v)", hostport, ok)
	}
	if !strings.HasPrefix(known.Fingerprint, "SHA256:") {
		t.Errorf("fingerprint = %q", known.Fingerprint)
	}

	// A changed key must abort the connection.
	known.Fingerprint = "SHA256:definitely-not-the-key"
	if err := tofu.store.PutKnownHost(known); err != nil {
		t.Fatal(err)
	}
	tofu.mgr.Disconnect(tofu.sessionID)
	ctx3, cancel3 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel3()
	if _, err := tofu.mgr.Connect(ctx3, tofu.sessionID, ""); err == nil {
		t.Fatal("expected host key mismatch")
	}
}

func TestBadPassword(t *testing.T) {
	f := newFixture(t, PolicyInsecure)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := f.mgr.Connect(ctx, f.sessionID, "wrong"); err == nil {
		t.Fatal("expected authentication failure")
	}
}
