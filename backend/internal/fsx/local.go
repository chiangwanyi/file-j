package fsx

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Local is the Provider for this machine's own file system. It intentionally
// uses the standard library plus the small platform adapter in os_unix.go /
// os_windows.go - there is nothing an external library would buy us for local
// disk access.
type Local struct {
	home string
}

// NewLocal creates the local provider.
func NewLocal() (*Local, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, fmt.Errorf("无法确定用户主目录: %w", err)
	}
	return &Local{home: home}, nil
}

// Home implements Provider.
func (l *Local) Home(context.Context) (string, error) { return l.home, nil }

// Root implements Provider.
func (l *Local) Root(p string) string { return RootPath(p) }

// Resolve turns "~", "~/x" (and "~\x" on Windows) and relative input into an
// absolute cleaned path.
func (l *Local) Resolve(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return l.home
	}
	if p == "~" {
		return l.home
	}
	// Both separators are accepted on Windows; on POSIX only "~/" exists and
	// the second condition is compiled to the same check.
	if strings.HasPrefix(p, "~/") || (filepath.Separator != '/' && strings.HasPrefix(p, "~\\")) {
		p = filepath.Join(l.home, p[2:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(l.home, p)
	}
	return filepath.Clean(p)
}

// Stat implements Provider. Symlinks are followed for the metadata but the entry
// still reports that it is a link.
func (l *Local) Stat(_ context.Context, p string) (Entry, error) {
	p = l.Resolve(p)
	li, err := os.Lstat(p)
	if err != nil {
		return Entry{}, err
	}
	info := li
	isLink := li.Mode()&fs.ModeSymlink != 0
	target := ""
	if isLink {
		if t, err := os.Readlink(p); err == nil {
			target = t
		}
		// Follow the link so navigating into a symlinked folder works.
		if fi, err := os.Stat(p); err == nil {
			info = fi
		}
	}
	hidden, readOnly := platformEntryFlags(filepath.Base(p), li)
	return Entry{
		Name:       filepath.Base(p),
		Path:       p,
		IsDir:      info.IsDir(),
		IsSymlink:  isLink,
		IsHidden:   hidden,
		ReadOnly:   readOnly,
		Size:       sizeOf(info),
		ModTime:    info.ModTime(),
		Mode:       ModeString(info.Mode(), info.IsDir()),
		Kind:       KindOf(info.Mode()),
		LinkTarget: target,
	}, nil
}

// List implements Provider.
func (l *Local) List(_ context.Context, p string, showHidden bool) (*ListResult, error) {
	p = l.Resolve(p)
	items, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	res := &ListResult{Path: p, Parent: Parent(p), Entries: make([]Entry, 0, len(items))}
	for _, it := range items {
		name := it.Name()
		info, err := it.Info()
		if err != nil {
			// A file racing with the listing is not worth failing the whole call.
			continue
		}
		hidden, readOnly := platformEntryFlags(name, info)
		if !showHidden && hidden {
			continue
		}
		full := filepath.Join(p, name)
		entry := Entry{
			Name:     name,
			Path:     full,
			IsDir:    info.IsDir(),
			IsHidden: hidden,
			ReadOnly: readOnly,
			Size:     sizeOf(info),
			ModTime:  info.ModTime(),
			Mode:     ModeString(info.Mode(), info.IsDir()),
			Kind:     KindOf(info.Mode()),
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			entry.IsSymlink = true
			if t, err := os.Readlink(full); err == nil {
				entry.LinkTarget = t
			}
			// Report the target's type/size for symlinked files, like Finder does.
			if fi, err := os.Stat(full); err == nil {
				entry.IsDir = fi.IsDir()
				entry.Size = sizeOf(fi)
				entry.Kind = KindOf(fi.Mode())
				entry.Mode = ModeString(fi.Mode(), fi.IsDir())
			}
		}
		res.Entries = append(res.Entries, entry)
	}
	if free, total, ok := diskUsage(p); ok {
		res.FreeBytes = &free
		res.TotalBytes = &total
	}
	return res, nil
}

// Open implements Provider.
func (l *Local) Open(_ context.Context, p string) (io.ReadCloser, error) {
	return os.Open(l.Resolve(p))
}

// Create implements Provider.
func (l *Local) Create(_ context.Context, p string, mode fs.FileMode, _ int64) (io.WriteCloser, error) {
	if mode == 0 {
		mode = 0o644
	}
	return os.OpenFile(l.Resolve(p), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
}

// MkdirAll implements Provider.
func (l *Local) MkdirAll(_ context.Context, p string, mode fs.FileMode) error {
	if mode == 0 {
		mode = 0o755
	}
	return os.MkdirAll(l.Resolve(p), mode)
}

// Remove implements Provider. It is recursive and permanent.
func (l *Local) Remove(_ context.Context, p string) error {
	return os.RemoveAll(l.Resolve(p))
}

// Rename implements Provider.
func (l *Local) Rename(_ context.Context, from, to string) error {
	return os.Rename(l.Resolve(from), l.Resolve(to))
}

// Chmod implements Provider.
func (l *Local) Chmod(_ context.Context, p string, mode fs.FileMode) error {
	return os.Chmod(l.Resolve(p), mode.Perm())
}

// Chtimes implements Provider.
func (l *Local) Chtimes(_ context.Context, p string, atime, mtime time.Time) error {
	return os.Chtimes(l.Resolve(p), atime, mtime)
}

// Trash moves a path into the platform's trash (macOS: ~/.Trash, Windows: the
// Recycle Bin). The bool result reports whether the item actually landed in the
// trash; false means the volume did not allow a move and the caller must fall
// back to a permanent delete. The implementation lives in trash_unix.go /
// trash_windows.go.

// QuickPlaces is a small helper used by the sidebar to suggest roots.
func (l *Local) QuickPlaces() []map[string]string {
	places := []map[string]string{
		{"name": "主目录", "path": l.home},
		{"name": "桌面", "path": filepath.Join(l.home, "Desktop")},
		{"name": "文稿", "path": filepath.Join(l.home, "Documents")},
		{"name": "下载", "path": filepath.Join(l.home, "Downloads")},
	}
	out := make([]map[string]string, 0, len(places)+1)
	for _, p := range places {
		if st, err := os.Stat(p["path"]); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	out = append(out, quickRoots()...)
	return out
}

func sizeOf(info os.FileInfo) int64 {
	if info.IsDir() {
		return 0
	}
	return info.Size()
}

// Ensure Local satisfies the Provider contract at compile time.
var _ Provider = (*Local)(nil)
