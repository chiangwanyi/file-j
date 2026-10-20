//go:build !windows

package fsx

import "path"

// The POSIX helpers below are the original implementation. On macOS and Linux
// local paths and SFTP paths share the "/" separator, so one implementation
// covers both. Windows gets its own pair in paths_windows.go.

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
