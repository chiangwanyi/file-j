//go:build !windows

package fsx

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// Platform adapter for macOS / Linux: volume capacity, cross-device detection,
// hidden-file rules and the roots offered in the sidebar. Windows has its own
// implementation in os_windows.go.

// diskUsage reports free bytes for the caller and total bytes of the volume
// holding p.
func diskUsage(p string) (free, total uint64, ok bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(p, &st); err != nil {
		return 0, 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), uint64(st.Blocks) * uint64(st.Bsize), true
}

// isCrossDevice reports whether err is the rename-across-volumes failure.
func isCrossDevice(err error) bool {
	return errors.Is(err, unix.EXDEV)
}

// platformEntryFlags applies the macOS hidden rule (dot files only). Read-only
// is not surfaced separately because the permission string already shows it.
func platformEntryFlags(name string, _ os.FileInfo) (hidden, readOnly bool) {
	return IsHiddenName(name), false
}

// isRootPath reports whether p is a file-system root.
func isRootPath(p string) bool { return p == "/" }

// quickRoots lists the volumes shown at the bottom of the sidebar places list.
func quickRoots() []map[string]string {
	return []map[string]string{{"name": "根目录", "path": "/"}}
}

// RootPath returns the top of the file tree the given path lives in. On POSIX
// there is exactly one, so it is always "/".
func RootPath(p string) string { return "/" }

// NameProblem returns a non-empty reason when name cannot be used as a file
// name on this platform. POSIX accepts everything except "/" and NUL, which
// the caller already checks.
func NameProblem(string) string { return "" }
