package remote

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"

	"github.com/jiangwanyi/file-j/backend/internal/fsx"
)

// Conn implements fsx.Provider, so the transfer engine treats a remote session
// exactly like a local folder.
var _ fsx.Provider = (*Conn)(nil)

// Resolve turns "." or "" into the session's home directory. Windows-style
// paths (from a Windows-hosted server or from tests) are normalised to the
// POSIX wire form first; POSIX paths are unaffected.
func (c *Conn) Resolve(p string) string {
	p = toPosixPath(strings.TrimSpace(p))
	if p == "" || p == "." {
		return c.home
	}
	if !strings.HasPrefix(p, "/") {
		return path.Join(c.home, p)
	}
	return path.Clean(p)
}

// Home implements fsx.Provider.
func (c *Conn) Home(context.Context) (string, error) { return c.home, nil }

// Root implements fsx.Provider. An SFTP server always speaks POSIX paths, so the
// root is "/" even when the backend itself runs on Windows (see path.go, which
// translates the server's local dialect back to wire paths).
func (c *Conn) Root(p string) string { return fsx.RootPath(p) }

// Stat implements fsx.Provider.
func (c *Conn) Stat(_ context.Context, p string) (fsx.Entry, error) {
	p = c.Resolve(p)
	li, err := c.SFTP.Lstat(p)
	if err != nil {
		return fsx.Entry{}, err
	}
	info := li
	isLink := li.Mode()&fs.ModeSymlink != 0
	target := ""
	if isLink {
		if t, err := c.SFTP.ReadLink(p); err == nil {
			target = t
		}
		if fi, err := c.SFTP.Stat(p); err == nil {
			info = fi
		}
	}
	return c.entry(p, info, isLink, target), nil
}

// List implements fsx.Provider.
func (c *Conn) List(ctx context.Context, p string, showHidden bool) (*fsx.ListResult, error) {
	p = c.Resolve(p)
	items, err := c.SFTP.ReadDirContext(ctx, p)
	if err != nil {
		return nil, err
	}
	res := &fsx.ListResult{Path: p, Parent: fsx.Parent(p), Entries: make([]fsx.Entry, 0, len(items))}
	for _, info := range items {
		name := info.Name()
		if !showHidden && fsx.IsHiddenName(name) {
			continue
		}
		full := path.Join(p, name)
		entry := c.entry(full, info, info.Mode()&fs.ModeSymlink != 0, "")
		if entry.IsSymlink {
			if t, err := c.SFTP.ReadLink(full); err == nil {
				entry.LinkTarget = t
			}
			// Resolve type/size across the link without failing the listing.
			if fi, err := c.SFTP.Stat(full); err == nil {
				entry.IsDir = fi.IsDir()
				entry.Size = sizeOf(fi)
				entry.Kind = fsx.KindOf(fi.Mode())
				entry.Mode = fsx.ModeString(fi.Mode(), fi.IsDir())
			}
		}
		res.Entries = append(res.Entries, entry)
	}
	if st, err := c.SFTP.StatVFS(p); err == nil {
		free := st.Bavail * uint64(st.Frsize)
		total := st.Blocks * uint64(st.Frsize)
		res.FreeBytes = &free
		res.TotalBytes = &total
	}
	return res, nil
}

// Open implements fsx.Provider.
func (c *Conn) Open(_ context.Context, p string) (io.ReadCloser, error) {
	return c.SFTP.Open(c.Resolve(p))
}

// Create implements fsx.Provider.
func (c *Conn) Create(_ context.Context, p string, mode fs.FileMode, _ int64) (io.WriteCloser, error) {
	f, err := c.SFTP.OpenFile(c.Resolve(p), os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return nil, err
	}
	if mode != 0 {
		_ = c.SFTP.Chmod(c.Resolve(p), mode.Perm())
	}
	return f, nil
}

// MkdirAll implements fsx.Provider.
func (c *Conn) MkdirAll(_ context.Context, p string, mode fs.FileMode) error {
	p = c.Resolve(p)
	if err := c.SFTP.MkdirAll(p); err != nil {
		return err
	}
	if mode != 0 {
		_ = c.SFTP.Chmod(p, mode.Perm())
	}
	return nil
}

// Remove implements fsx.Provider. It is recursive and permanent.
func (c *Conn) Remove(_ context.Context, p string) error {
	return c.SFTP.RemoveAll(c.Resolve(p))
}

// Rename implements fsx.Provider. PosixRename overwrites atomically where the
// server supports it, which is what a file manager needs when replacing.
func (c *Conn) Rename(_ context.Context, from, to string) error {
	from, to = c.Resolve(from), c.Resolve(to)
	if err := c.SFTP.PosixRename(from, to); err != nil {
		return c.SFTP.Rename(from, to)
	}
	return nil
}

// Chmod implements fsx.Provider.
func (c *Conn) Chmod(_ context.Context, p string, mode fs.FileMode) error {
	return c.SFTP.Chmod(c.Resolve(p), mode.Perm())
}

// Chtimes implements fsx.Provider.
func (c *Conn) Chtimes(_ context.Context, p string, atime, mtime time.Time) error {
	return c.SFTP.Chtimes(c.Resolve(p), atime, mtime)
}

// Mkdir implements a single-level directory creation.
func (c *Conn) Mkdir(_ context.Context, p string, mode fs.FileMode) error {
	p = c.Resolve(p)
	if err := c.SFTP.Mkdir(p); err != nil {
		return err
	}
	if mode != 0 {
		_ = c.SFTP.Chmod(p, mode.Perm())
	}
	return nil
}

func (c *Conn) entry(p string, info os.FileInfo, isLink bool, target string) fsx.Entry {
	e := fsx.Entry{
		Name:       info.Name(),
		Path:       p,
		IsDir:      info.IsDir(),
		IsSymlink:  isLink,
		IsHidden:   fsx.IsHiddenName(info.Name()),
		ModTime:    info.ModTime(),
		Mode:       fsx.ModeString(info.Mode(), info.IsDir()),
		Kind:       fsx.KindOf(info.Mode()),
		LinkTarget: target,
	}
	if !info.IsDir() {
		e.Size = info.Size()
	}
	if st, ok := info.Sys().(*sftp.FileStat); ok {
		e.Owner = strconv.FormatUint(uint64(st.UID), 10)
		e.Group = strconv.FormatUint(uint64(st.GID), 10)
	}
	// Read-only when the owner has no write bit.
	e.ReadOnly = info.Mode().Perm()&0o200 == 0
	return e
}

func sizeOf(info os.FileInfo) int64 {
	if info.IsDir() {
		return 0
	}
	return info.Size()
}
