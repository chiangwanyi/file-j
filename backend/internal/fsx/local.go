package fsx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Local is the Provider for the Mac's own file system. It intentionally uses the
// standard library only - there is nothing an external library would buy us for
// local disk access.
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

// Resolve turns "~", "~/x" and relative input into an absolute cleaned path.
func (l *Local) Resolve(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return l.home
	}
	if p == "~" {
		return l.home
	}
	if strings.HasPrefix(p, "~/") {
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
	return Entry{
		Name:       filepath.Base(p),
		Path:       p,
		IsDir:      info.IsDir(),
		IsSymlink:  isLink,
		IsHidden:   IsHiddenName(filepath.Base(p)),
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
		if !showHidden && IsHiddenName(name) {
			continue
		}
		full := filepath.Join(p, name)
		info, err := it.Info()
		if err != nil {
			// A file racing with the listing is not worth failing the whole call.
			continue
		}
		entry := Entry{
			Name:     name,
			Path:     full,
			IsDir:    info.IsDir(),
			IsHidden: IsHiddenName(name),
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
	var st unix.Statfs_t
	if err := unix.Statfs(p, &st); err == nil {
		free := uint64(st.Bavail) * uint64(st.Bsize)
		total := uint64(st.Blocks) * uint64(st.Bsize)
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

// Trash moves a path into ~/.Trash, matching Finder semantics. The bool result
// reports whether the item actually landed in the Trash; false means the volume
// did not allow a move and the caller must fall back to a permanent delete.
func (l *Local) Trash(p string) (bool, error) {
	p = l.Resolve(p)
	if p == "/" || p == l.home {
		return false, errors.New("拒绝删除该路径")
	}
	trash := filepath.Join(l.home, ".Trash")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return false, err
	}
	target := filepath.Join(trash, filepath.Base(p))
	if _, err := os.Lstat(target); err == nil {
		base := filepath.Base(p)
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		for i := 2; i < 1000; i++ {
			candidate := filepath.Join(trash, stem+" "+strconv.Itoa(i)+ext)
			if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
				target = candidate
				break
			}
		}
	}
	if err := os.Rename(p, target); err != nil {
		if errors.Is(err, unix.EXDEV) {
			// Different volume: no Trash available there.
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Subdirectories is a small helper used by the sidebar to suggest roots.
func (l *Local) QuickPlaces() []map[string]string {
	places := []map[string]string{
		{"name": "主目录", "path": l.home},
		{"name": "桌面", "path": filepath.Join(l.home, "Desktop")},
		{"name": "文稿", "path": filepath.Join(l.home, "Documents")},
		{"name": "下载", "path": filepath.Join(l.home, "Downloads")},
	}
	out := make([]map[string]string, 0, len(places))
	for _, p := range places {
		if st, err := os.Stat(p["path"]); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	out = append(out, map[string]string{"name": "根目录", "path": "/"})
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
