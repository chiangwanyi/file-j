//go:build darwin

package app

import (
	"os/exec"

	"github.com/jiangwanyi/file-j/backend/internal/protocol"
)

// macOS adapter: Launch Services via `open(1)`.

// openPath opens a file with its default application, or a folder in Finder.
func openPath(p string) error {
	return exec.Command("open", p).Start()
}

// revealPath shows the item in Finder with its parent folder selected.
func revealPath(p string) error {
	if err := exec.Command("open", "-R", p).Start(); err != nil {
		// "open -R" fails when the path itself is a folder on some versions;
		// fall back to opening the folder directly.
		if err2 := exec.Command("open", p).Start(); err2 != nil {
			return protocol.Wrapf(protocol.CodeInternal, "无法在 Finder 中显示", err.Error())
		}
	}
	return nil
}
