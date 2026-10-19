package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jiangwanyi/file-j/backend/internal/protocol"
)

// newTestApp boots the real backend (handlers included) against a temp config
// directory, so this test covers the wire protocol and the handler wiring that
// the Go unit tests cannot see.
func newTestApp(t *testing.T) (*App, *httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	a, err := New(Options{
		Version:   "test",
		ConfigDir: dir,
		Token:     "test-token",
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(func() {
		srv.Close()
		a.Close()
	})
	return a, srv, dir
}

func dial(t *testing.T, srv *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?token=" + token
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial: %v (status %d)", err, resp.StatusCode)
		}
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

type rpcClient struct {
	t    *testing.T
	conn *websocket.Conn
	seq  int
}

func (c *rpcClient) call(method string, params any, result any) *protocol.Error {
	c.t.Helper()
	c.seq++
	id := "req-" + string(rune('a'+c.seq%26)) + method
	payload, _ := json.Marshal(map[string]any{
		"id": id, "type": "request", "method": method, "params": params,
	})
	if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		c.t.Fatalf("write: %v", err)
	}
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			c.t.Fatalf("read: %v", err)
		}
		var envelope struct {
			ID     string          `json:"id"`
			Type   string          `json:"type"`
			OK     bool            `json:"ok"`
			Result json.RawMessage `json:"result"`
			Error  *protocol.Error `json:"error"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			c.t.Fatalf("decode: %v", err)
		}
		if envelope.Type == "event" {
			continue
		}
		if envelope.ID != id {
			continue
		}
		if !envelope.OK {
			return envelope.Error
		}
		if result != nil && len(envelope.Result) > 0 {
			if err := json.Unmarshal(envelope.Result, result); err != nil {
				c.t.Fatalf("decode result: %v", err)
			}
		}
		return nil
	}
}

func TestRejectsBadToken(t *testing.T) {
	_, srv, _ := newTestApp(t)
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?token=wrong"
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err == nil {
		conn.Close()
		t.Fatal("expected handshake to fail with a wrong token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v, want 401", resp)
	}
}

func TestHandlersEndToEnd(t *testing.T) {
	_, srv, dir := newTestApp(t)
	c := &rpcClient{t: t, conn: dial(t, srv, "test-token")}

	var info struct {
		Version   string `json:"version"`
		ConfigDir string `json:"configDir"`
		Platform  string `json:"platform"`
		GoVersion string `json:"goVersion"`
	}
	if perr := c.call("app.info", nil, &info); perr != nil {
		t.Fatalf("app.info: %v", perr)
	}
	if info.Version != "test" || info.Platform == "" {
		t.Errorf("info = %+v", info)
	}
	if !strings.HasPrefix(info.ConfigDir, dir) {
		t.Errorf("configDir = %q, want under %q", info.ConfigDir, dir)
	}

	var cfg struct {
		Config struct {
			Transfer struct {
				Concurrency int `json:"concurrency"`
			} `json:"transfer"`
		} `json:"config"`
		Paths map[string]string `json:"paths"`
	}
	if perr := c.call("config.get", nil, &cfg); perr != nil {
		t.Fatalf("config.get: %v", perr)
	}
	if cfg.Config.Transfer.Concurrency != 3 || cfg.Paths["config"] == "" {
		t.Errorf("config = %+v", cfg)
	}

	// Files live outside the config dir so the assertions stay readable.
	work := t.TempDir()
	var mkdir struct {
		Path string `json:"path"`
	}
	if perr := c.call("fs.mkdir", map[string]any{"side": "local", "path": work, "name": "folder"}, &mkdir); perr != nil {
		t.Fatalf("fs.mkdir: %v", perr)
	}
	if _, err := os.Stat(mkdir.Path); err != nil {
		t.Fatalf("fs.mkdir did not create the folder: %v", err)
	}

	src := filepath.Join(work, "note.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	var listing struct {
		Path    string `json:"path"`
		Entries []struct {
			Name string `json:"name"`
		} `json:"entries"`
	}
	if perr := c.call("fs.list", map[string]any{"side": "local", "path": work, "showHidden": true}, &listing); perr != nil {
		t.Fatalf("fs.list: %v", perr)
	}
	if listing.Path != work || len(listing.Entries) != 2 {
		t.Errorf("listing = %+v", listing)
	}

	dst := t.TempDir()
	var tasks []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if perr := c.call("transfer.create", map[string]any{
		"op": "copy", "conflict": "overwrite",
		"items": []map[string]any{{
			"from": map[string]string{"side": "local", "path": src},
			"to":   map[string]string{"side": "local", "path": dst},
		}},
	}, &tasks); perr != nil {
		t.Fatalf("transfer.create: %v", perr)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks))
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var list []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Bytes  int64  `json:"bytes"`
		}
		if perr := c.call("transfer.list", nil, &list); perr != nil {
			t.Fatalf("transfer.list: %v", perr)
		}
		for _, task := range list {
			if task.ID == tasks[0].ID && task.Status == "done" {
				if _, err := os.Stat(filepath.Join(dst, "note.txt")); err != nil {
					t.Fatalf("copy did not land: %v", err)
				}
				goto copied
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("transfer did not finish")

copied:
	var deleted struct {
		Deleted  int   `json:"deleted"`
		Failures []any `json:"failures"`
	}
	if perr := c.call("fs.delete", map[string]any{
		"side": "local", "paths": []string{mkdir.Path}, "permanent": true,
	}, &deleted); perr != nil {
		t.Fatalf("fs.delete: %v", perr)
	}
	if deleted.Deleted != 1 {
		t.Errorf("deleted = %d", deleted.Deleted)
	}
	if deleted.Failures == nil {
		t.Error("failures must be [] rather than null so the UI can call .length")
	}

	var saved struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
	}
	if perr := c.call("session.save", map[string]any{
		"session": map[string]any{"name": "prod", "host": "example.com", "port": 22, "user": "deploy"},
	}, &saved); perr != nil {
		t.Fatalf("session.save: %v", perr)
	}
	if saved.Session.ID == "" {
		t.Fatal("session.save returned no id")
	}

	var sessions []struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
		HasCredential bool `json:"hasCredential"`
	}
	if perr := c.call("session.list", nil, &sessions); perr != nil {
		t.Fatalf("session.list: %v", perr)
	}
	if len(sessions) != 1 || sessions[0].Session.ID != saved.Session.ID {
		t.Errorf("sessions = %+v", sessions)
	}

	var uiState struct {
		Left struct {
			Path string `json:"path"`
		} `json:"left"`
	}
	if perr := c.call("ui.state.set", map[string]any{
		"left":       map[string]any{"kind": "local", "path": "/tmp", "sortKey": "name"},
		"right":      map[string]any{"kind": "remote", "path": ".", "sortKey": "name"},
		"splitRatio": 0.4,
	}, nil); perr != nil {
		t.Fatalf("ui.state.set: %v", perr)
	}
	if perr := c.call("ui.state.get", nil, &uiState); perr != nil {
		t.Fatalf("ui.state.get: %v", perr)
	}
	if uiState.Left.Path != "/tmp" {
		t.Errorf("left path = %q", uiState.Left.Path)
	}

	// Unknown methods must produce a protocol error, not a hang or a panic.
	if perr := c.call("does.not.exist", nil, nil); perr == nil {
		t.Error("expected an error for an unknown method")
	} else if perr.Code != protocol.CodeBadRequest {
		t.Errorf("code = %q", perr.Code)
	}
}

func TestEventBroadcast(t *testing.T) {
	_, srv, _ := newTestApp(t)
	c := &rpcClient{t: t, conn: dial(t, srv, "test-token")}

	work := t.TempDir()
	src := filepath.Join(work, "e.txt")
	if err := os.WriteFile(src, []byte(strings.Repeat("x", 4096)), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()

	var tasks []struct {
		ID string `json:"id"`
	}
	if perr := c.call("transfer.create", map[string]any{
		"op": "copy",
		"items": []map[string]any{{
			"from": map[string]string{"side": "local", "path": src},
			"to":   map[string]string{"side": "local", "path": dst},
		}},
	}, &tasks); perr != nil {
		t.Fatalf("transfer.create: %v", perr)
	}

	_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			t.Fatalf("expected a transfer.task event: %v", err)
		}
		var event struct {
			Type    string `json:"type"`
			Event   string `json:"event"`
			Payload struct {
				Task struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"task"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			continue
		}
		if event.Type == "event" && event.Event == "transfer.task" && event.Payload.Task.ID == tasks[0].ID {
			return
		}
	}
}
