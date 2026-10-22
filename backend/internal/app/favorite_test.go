package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jiangwanyi/file-j/backend/internal/config"
	"github.com/jiangwanyi/file-j/backend/internal/protocol"
)

// TestFavoritesOverIPC covers the sidebar's bookmark list end to end: the path
// is resolved and validated in Go, so the UI can send whatever the address bar
// or a folder row contains.
func TestFavoritesOverIPC(t *testing.T) {
	_, srv, dir := newTestApp(t)
	c := &rpcClient{t: t, conn: dial(t, srv, "test-token")}

	var list []config.Favorite
	if perr := c.call("favorite.list", nil, &list); perr != nil {
		t.Fatalf("favorite.list: %v", perr)
	}
	if len(list) != 0 {
		t.Fatalf("favorites = %v, want empty", list)
	}

	folder := t.TempDir()
	var saved config.Favorite
	if perr := c.call("favorite.add", map[string]string{"path": folder, "name": "工作目录"}, &saved); perr != nil {
		t.Fatalf("favorite.add: %v", perr)
	}
	if saved.Path != folder || saved.Name != "工作目录" {
		t.Errorf("favorite = %+v", saved)
	}

	// Adding the same folder again is idempotent (the file table and the pane
	// menu can both hit this when the user double-clicks the entry).
	if perr := c.call("favorite.add", map[string]string{"path": folder}, &saved); perr != nil {
		t.Fatalf("favorite.add duplicate: %v", perr)
	}
	list = nil
	if perr := c.call("favorite.list", nil, &list); perr != nil {
		t.Fatalf("favorite.list: %v", perr)
	}
	if len(list) != 1 || list[0].Name != "工作目录" {
		t.Fatalf("favorites after duplicate add = %v", list)
	}

	// A missing folder and a regular file are both refused: a bookmark that
	// cannot be opened is worse than no bookmark.
	missing := filepath.Join(folder, "nope")
	if perr := c.call("favorite.add", map[string]string{"path": missing}, nil); perr == nil {
		t.Error("favorite.add accepted a missing folder")
	} else if perr.Code != protocol.CodeNotFound {
		t.Errorf("code = %s, want NOT_FOUND", perr.Code)
	}
	file := filepath.Join(folder, "note.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if perr := c.call("favorite.add", map[string]string{"path": file}, nil); perr == nil {
		t.Error("favorite.add accepted a file")
	} else if perr.Code != protocol.CodeBadRequest {
		t.Errorf("code = %s, want BAD_REQUEST", perr.Code)
	}

	// "~" is expanded like everywhere else in the app.
	var home config.Favorite
	if perr := c.call("favorite.add", map[string]string{"path": "~"}, &home); perr != nil {
		t.Fatalf("favorite.add ~: %v", perr)
	}
	if !filepath.IsAbs(home.Path) {
		t.Errorf("home bookmark = %q, want an absolute path", home.Path)
	}

	if perr := c.call("favorite.remove", map[string]string{"path": folder}, nil); perr != nil {
		t.Fatalf("favorite.remove: %v", perr)
	}
	list = nil
	if perr := c.call("favorite.list", nil, &list); perr != nil {
		t.Fatalf("favorite.list: %v", perr)
	}
	if len(list) != 1 || list[0].Path != home.Path {
		t.Errorf("favorites after remove = %v", list)
	}

	// Survives a restart: the store is the source of truth, not memory.
	if _, err := os.Stat(filepath.Join(dir, "favorites.yaml")); err != nil {
		t.Errorf("favorites.yaml was not written: %v", err)
	}
}
