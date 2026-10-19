package transfer

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jiangwanyi/file-j/backend/internal/config"
	"github.com/jiangwanyi/file-j/backend/internal/credentials"
	"github.com/jiangwanyi/file-j/backend/internal/fsx"
	"github.com/jiangwanyi/file-j/backend/internal/remote"
	"github.com/jiangwanyi/file-j/backend/internal/testutil"
)

type harness struct {
	engine  *Engine
	local   *fsx.Local
	manager *remote.Manager
	store   *config.Store
	session string

	mu     sync.Mutex
	events []string
}

func newHarness(t *testing.T, mutate func(*config.Config)) *harness {
	t.Helper()
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}
	cfg := store.Config()
	cfg.Transfer.Conflict = "overwrite"
	if mutate != nil {
		mutate(&cfg)
	}
	if _, err := store.SetConfig(cfg); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	local, err := fsx.NewLocal()
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	server := testutil.StartSSHD(t)
	creds := credentials.NewMemory()
	sess, err := store.SaveSession(config.Session{
		Name: "test", Host: "127.0.0.1", Port: server.Port(), User: server.User,
		AuthMethod: "password", CredentialRef: "session/test",
	})
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := creds.Set("session/test", server.Password); err != nil {
		t.Fatalf("creds.Set: %v", err)
	}
	mgr := remote.NewManager(store, creds, slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(mgr.Close)

	h := &harness{local: local, manager: mgr, store: store, session: sess.ID}
	h.engine = New(
		func(ctx context.Context, ep Endpoint) (fsx.Provider, error) {
			if ep.Side == "local" {
				return local, nil
			}
			return mgr.Get(ctx, ep.SessionID)
		},
		func(event string, payload any) {
			h.mu.Lock()
			h.events = append(h.events, event)
			h.mu.Unlock()
		},
		func() config.Config { return store.Config() },
	)
	t.Cleanup(h.engine.Close)
	return h
}

func (h *harness) wait(t *testing.T, id string) Task {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, task := range h.engine.List() {
			if task.ID != id {
				continue
			}
			switch task.Status {
			case StatusDone, StatusFailed, StatusCanceled, StatusSkipped:
				return task
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task %s did not finish", id)
	return Task{}
}

func (h *harness) mustCreate(t *testing.T, req CreateRequest) Task {
	t.Helper()
	tasks, err := h.engine.Create(req)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks))
	}
	return h.wait(t, tasks[0].ID)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestCopyFileLocalToLocal(t *testing.T) {
	h := newHarness(t, nil)
	srcDir, dstDir := t.TempDir(), t.TempDir()
	src := filepath.Join(srcDir, "report.txt")
	writeFile(t, src, "quarterly numbers")

	task := h.mustCreate(t, CreateRequest{
		Op:    "copy",
		Items: []Item{{From: Endpoint{Side: "local", Path: src}, To: Endpoint{Side: "local", Path: dstDir}}},
	})
	if task.Status != StatusDone {
		t.Fatalf("status = %s, error = %s", task.Status, task.Error)
	}
	if task.Direction != "local-local" {
		t.Errorf("direction = %q", task.Direction)
	}
	got, err := os.ReadFile(filepath.Join(dstDir, "report.txt"))
	if err != nil || string(got) != "quarterly numbers" {
		t.Fatalf("copied content = %q err=%v", got, err)
	}
	if task.Bytes != int64(len("quarterly numbers")) {
		t.Errorf("bytes = %d", task.Bytes)
	}
	// Source must survive a copy.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("source disappeared: %v", err)
	}
}

func TestCopyDirectoryRecursiveAndKeepBoth(t *testing.T) {
	h := newHarness(t, nil)
	srcDir, dstDir := t.TempDir(), t.TempDir()
	tree := filepath.Join(srcDir, "project")
	writeFile(t, filepath.Join(tree, "README.md"), "docs")
	writeFile(t, filepath.Join(tree, "src", "main.go"), "package main")
	writeFile(t, filepath.Join(tree, "src", "util", "a.go"), "package util")

	task := h.mustCreate(t, CreateRequest{
		Op:       "copy",
		Conflict: "keepboth",
		Items:    []Item{{From: Endpoint{Side: "local", Path: tree}, To: Endpoint{Side: "local", Path: dstDir}}},
	})
	if task.Status != StatusDone {
		t.Fatalf("status = %s err=%s", task.Status, task.Error)
	}
	if task.Files != 3 {
		t.Errorf("files = %d, want 3", task.Files)
	}
	for _, rel := range []string{"README.md", "src/main.go", "src/util/a.go"} {
		if _, err := os.Stat(filepath.Join(dstDir, "project", rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}

	// Copying again with keepboth must create "project (1)" instead of merging.
	task2 := h.mustCreate(t, CreateRequest{
		Op:       "copy",
		Conflict: "keepboth",
		Items:    []Item{{From: Endpoint{Side: "local", Path: tree}, To: Endpoint{Side: "local", Path: dstDir}}},
	})
	if task2.Status != StatusDone {
		t.Fatalf("second copy failed: %s", task2.Error)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "project (1)", "README.md")); err != nil {
		t.Errorf("keepboth copy missing: %v", err)
	}
}

func TestConflictSkipAndFail(t *testing.T) {
	h := newHarness(t, nil)
	srcDir, dstDir := t.TempDir(), t.TempDir()
	src := filepath.Join(srcDir, "a.txt")
	writeFile(t, src, "new")
	writeFile(t, filepath.Join(dstDir, "a.txt"), "old")

	task := h.mustCreate(t, CreateRequest{
		Op:       "copy",
		Conflict: "skip",
		Items:    []Item{{From: Endpoint{Side: "local", Path: src}, To: Endpoint{Side: "local", Path: dstDir}}},
	})
	if task.Status != StatusSkipped {
		t.Fatalf("status = %s, want skipped", task.Status)
	}
	if data, _ := os.ReadFile(filepath.Join(dstDir, "a.txt")); string(data) != "old" {
		t.Error("skip policy overwrote the destination")
	}

	task = h.mustCreate(t, CreateRequest{
		Op:       "copy",
		Conflict: "fail",
		Items:    []Item{{From: Endpoint{Side: "local", Path: src}, To: Endpoint{Side: "local", Path: dstDir}}},
	})
	if task.Status != StatusFailed {
		t.Fatalf("status = %s, want failed", task.Status)
	}

	task = h.mustCreate(t, CreateRequest{
		Op:       "copy",
		Conflict: "overwrite",
		Items:    []Item{{From: Endpoint{Side: "local", Path: src}, To: Endpoint{Side: "local", Path: dstDir}}},
	})
	if task.Status != StatusDone {
		t.Fatalf("overwrite failed: %s", task.Error)
	}
	if data, _ := os.ReadFile(filepath.Join(dstDir, "a.txt")); string(data) != "new" {
		t.Error("overwrite did not replace the destination")
	}
}

func TestMoveLocalRemovesSource(t *testing.T) {
	h := newHarness(t, nil)
	srcDir, dstDir := t.TempDir(), t.TempDir()
	src := filepath.Join(srcDir, "move-me.txt")
	writeFile(t, src, "moving")

	task := h.mustCreate(t, CreateRequest{
		Op:    "move",
		Items: []Item{{From: Endpoint{Side: "local", Path: src}, To: Endpoint{Side: "local", Path: dstDir}}},
	})
	if task.Status != StatusDone {
		t.Fatalf("status = %s err=%s", task.Status, task.Error)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source still exists after move")
	}
	if data, _ := os.ReadFile(filepath.Join(dstDir, "move-me.txt")); string(data) != "moving" {
		t.Error("moved content mismatch")
	}
}

func TestUploadAndDownloadOverSFTP(t *testing.T) {
	h := newHarness(t, nil)
	localDir := t.TempDir()
	remoteDir := t.TempDir()
	payload := strings.Repeat("file-j over sftp ", 4096)
	writeFile(t, filepath.Join(localDir, "upload.bin"), payload)

	up := h.mustCreate(t, CreateRequest{
		Op: "copy",
		Items: []Item{{
			From: Endpoint{Side: "local", Path: filepath.Join(localDir, "upload.bin")},
			To:   Endpoint{Side: "remote", SessionID: h.session, Path: remoteDir},
		}},
	})
	if up.Status != StatusDone {
		t.Fatalf("upload status = %s err=%s", up.Status, up.Error)
	}
	if up.Direction != "upload" {
		t.Errorf("direction = %q", up.Direction)
	}
	remotePath := filepath.Join(remoteDir, "upload.bin")
	if data, err := os.ReadFile(remotePath); err != nil || string(data) != payload {
		t.Fatalf("remote content mismatch: %v len=%d", err, len(data))
	}

	// Now pull it back into a different local folder.
	downDir := t.TempDir()
	down := h.mustCreate(t, CreateRequest{
		Op: "copy",
		Items: []Item{{
			From: Endpoint{Side: "remote", SessionID: h.session, Path: remotePath},
			To:   Endpoint{Side: "local", Path: downDir},
		}},
	})
	if down.Status != StatusDone {
		t.Fatalf("download status = %s err=%s", down.Status, down.Error)
	}
	if down.Direction != "download" {
		t.Errorf("direction = %q", down.Direction)
	}
	if data, err := os.ReadFile(filepath.Join(downDir, "upload.bin")); err != nil || string(data) != payload {
		t.Fatalf("downloaded content mismatch: %v", err)
	}

	// And a remote recursive download.
	writeFile(t, filepath.Join(remoteDir, "tree", "x", "y.txt"), "nested")
	downTree := t.TempDir()
	task := h.mustCreate(t, CreateRequest{
		Op: "copy",
		Items: []Item{{
			From: Endpoint{Side: "remote", SessionID: h.session, Path: filepath.Join(remoteDir, "tree")},
			To:   Endpoint{Side: "local", Path: downTree},
		}},
	})
	if task.Status != StatusDone {
		t.Fatalf("tree download failed: %s", task.Error)
	}
	if _, err := os.Stat(filepath.Join(downTree, "tree", "x", "y.txt")); err != nil {
		t.Errorf("nested file missing: %v", err)
	}
}

func TestQueuedTaskCanBeCanceled(t *testing.T) {
	h := newHarness(t, func(c *config.Config) { c.Transfer.Concurrency = 1 })
	srcDir, dstDir := t.TempDir(), t.TempDir()
	var items []Item
	for i := 0; i < 40; i++ {
		p := filepath.Join(srcDir, "f"+time.Now().Format("150405.000000")+".txt")
		writeFile(t, p, strings.Repeat("x", 64*1024))
		items = append(items, Item{From: Endpoint{Side: "local", Path: p}, To: Endpoint{Side: "local", Path: dstDir}})
	}
	tasks, err := h.engine.Create(CreateRequest{Op: "copy", Items: items})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	last := tasks[len(tasks)-1]
	if err := h.engine.Cancel(last.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	final := h.wait(t, last.ID)
	if final.Status != StatusCanceled {
		t.Fatalf("status = %s, want canceled", final.Status)
	}
}

func TestRemoteToRemoteRejected(t *testing.T) {
	h := newHarness(t, nil)
	_, err := h.engine.Create(CreateRequest{
		Op: "copy",
		Items: []Item{{
			From: Endpoint{Side: "remote", SessionID: h.session, Path: "/tmp/a"},
			To:   Endpoint{Side: "remote", SessionID: h.session, Path: "/tmp/b"},
		}},
	})
	if err == nil {
		t.Fatal("expected remote-to-remote to be rejected")
	}
}
