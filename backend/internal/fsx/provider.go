// Package fsx defines the file-system abstraction shared by the local disk and
// remote SFTP sessions. The transfer engine only ever talks to a Provider, which
// is what makes "local -> local", "local -> remote" and "remote -> local" a
// single code path instead of three.
package fsx

import (
	"context"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"
)

// Entry describes one directory item. It is the exact shape the front-end
// renders, so no post-processing is needed in the UI.
type Entry struct {
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	IsDir      bool      `json:"isDir"`
	IsSymlink  bool      `json:"isSymlink"`
	IsHidden   bool      `json:"isHidden"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"modTime"`
	Mode       string    `json:"mode"` // human readable, e.g. drwxr-xr-x
	Kind       string    `json:"kind"` // dir | file | symlink | socket | device | fifo | other
	Owner      string    `json:"owner,omitempty"`
	Group      string    `json:"group,omitempty"`
	LinkTarget string    `json:"linkTarget,omitempty"`
	ReadOnly   bool      `json:"readOnly,omitempty"`
}

// ListResult is a directory listing plus the navigation context the UI needs.
type ListResult struct {
	Path       string  `json:"path"`
	Parent     string  `json:"parent"`
	Entries    []Entry `json:"entries"`
	FreeBytes  *uint64 `json:"freeBytes,omitempty"`
	TotalBytes *uint64 `json:"totalBytes,omitempty"`
}

// Provider is the minimal surface required to browse and transfer files.
// Implementations: local.go (the Mac's own disk) and remote (an SFTP session).
type Provider interface {
	// Stat returns metadata about a single path. Symlinks are followed.
	Stat(ctx context.Context, p string) (Entry, error)
	// List reads a directory.
	List(ctx context.Context, p string, showHidden bool) (*ListResult, error)
	// Open reads a file.
	Open(ctx context.Context, p string) (io.ReadCloser, error)
	// Create truncates or creates a file for writing.
	Create(ctx context.Context, p string, mode fs.FileMode, size int64) (io.WriteCloser, error)
	// MkdirAll creates a directory hierarchy.
	MkdirAll(ctx context.Context, p string, mode fs.FileMode) error
	// Remove deletes a file or directory tree permanently.
	Remove(ctx context.Context, p string) error
	// Rename moves a path inside the same provider.
	Rename(ctx context.Context, from, to string) error
	// Chmod sets permission bits (best effort).
	Chmod(ctx context.Context, p string, mode fs.FileMode) error
	// Chtimes sets access/modification times (best effort).
	Chtimes(ctx context.Context, p string, atime, mtime time.Time) error
	// Home returns the default starting directory.
	Home(ctx context.Context) (string, error)
}

// Clean normalises a POSIX style path. Both macOS and SFTP use "/", which lets
// the whole backend share one path implementation.
func Clean(p string) string {
	if p == "" {
		return "/"
	}
	return path.Clean(p)
}

// Join appends a child name to a directory path.
func Join(base, name string) string {
	if base == "" {
		base = "/"
	}
	return path.Join(base, name)
}

// Parent returns the parent directory, clamped at the root.
func Parent(p string) string {
	p = Clean(p)
	if p == "/" {
		return "/"
	}
	parent := path.Dir(p)
	if parent == "." {
		return "/"
	}
	return parent
}

// Base returns the final element of a path.
func Base(p string) string {
	p = Clean(p)
	if p == "/" {
		return "/"
	}
	return path.Base(p)
}

// IsHiddenName reports whether a name should be hidden by dotfile rules.
func IsHiddenName(name string) bool {
	return strings.HasPrefix(name, ".")
}

// ModeString renders permission bits the way `ls -l` does, without the leading
// file type character being guessed from the name.
func ModeString(m fs.FileMode, isDir bool) string {
	b := make([]byte, 10)
	if isDir {
		b[0] = 'd'
	} else if m&fs.ModeSymlink != 0 {
		b[0] = 'l'
	} else if m&fs.ModeSocket != 0 {
		b[0] = 's'
	} else if m&fs.ModeDevice != 0 {
		b[0] = 'b'
	} else if m&fs.ModeNamedPipe != 0 {
		b[0] = 'p'
	} else {
		b[0] = '-'
	}
	const rwx = "rwxrwxrwx"
	for i := 0; i < 9; i++ {
		if m&(1<<uint(8-i)) != 0 {
			b[i+1] = rwx[i]
		} else {
			b[i+1] = '-'
		}
	}
	// setuid / setgid / sticky
	if m&fs.ModeSetuid != 0 {
		if b[3] == 'x' {
			b[3] = 's'
		} else {
			b[3] = 'S'
		}
	}
	if m&fs.ModeSetgid != 0 {
		if b[6] == 'x' {
			b[6] = 's'
		} else {
			b[6] = 'S'
		}
	}
	if m&fs.ModeSticky != 0 {
		if b[9] == 'x' {
			b[9] = 't'
		} else {
			b[9] = 'T'
		}
	}
	return string(b)
}

// KindOf classifies a file mode into a stable string for the UI.
func KindOf(m fs.FileMode) string {
	switch {
	case m.IsDir():
		return "dir"
	case m&fs.ModeSymlink != 0:
		return "symlink"
	case m&fs.ModeSocket != 0:
		return "socket"
	case m&fs.ModeNamedPipe != 0:
		return "fifo"
	case m&fs.ModeDevice != 0:
		return "device"
	case m.IsRegular():
		return "file"
	default:
		return "other"
	}
}
