//go:build windows

package fsx

import (
	"path"
	"path/filepath"
	"strings"
)

// On Windows two path dialects coexist:
//
//   - local paths are Windows style: "C:\Users\me" or "\\server\share\dir",
//   - SFTP paths are always POSIX style: "/srv/www".
//
// The transfer engine and the RPC handlers call the same helpers for both
// sides, so the helpers pick their separator by inspecting the input: a drive
// letter or a UNC prefix means Windows style, everything else is treated as
// POSIX. That keeps remote paths untouched on Windows and keeps the two
// dialects from being mixed inside one path.

func isWindowsStyle(p string) bool {
	if len(p) >= 2 && p[1] == ':' {
		c := p[0]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return strings.HasPrefix(p, `\\`)
}

// Clean normalises a path using the dialect the path itself belongs to.
func Clean(p string) string {
	if p == "" {
		return "/"
	}
	if isWindowsStyle(p) {
		return filepath.Clean(p)
	}
	return path.Clean(p)
}

// Join appends a child name to a directory path.
func Join(base, name string) string {
	if base == "" {
		base = "/"
	}
	if isWindowsStyle(base) {
		return filepath.Join(base, name)
	}
	return path.Join(base, name)
}

// Parent returns the parent directory, clamped at the root ("C:\", a UNC share
// root or "/" for POSIX paths).
func Parent(p string) string {
	if isWindowsStyle(p) {
		cleaned := filepath.Clean(p)
		parent := filepath.Dir(cleaned)
		if parent == cleaned {
			// Volume root: C:\ or \\server\share is its own parent.
			return cleaned
		}
		return parent
	}
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
	if isWindowsStyle(p) {
		cleaned := filepath.Clean(p)
		if cleaned == filepath.VolumeName(cleaned)+`\` {
			// Drive / share root keeps its full name, like "/" does on POSIX.
			return cleaned
		}
		return filepath.Base(cleaned)
	}
	p = Clean(p)
	if p == "/" {
		return "/"
	}
	return path.Base(p)
}
