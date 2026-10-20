//go:build !windows

package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Trash moves a path into ~/.Trash, matching Finder semantics. The bool result
// reports whether the item actually landed in the Trash; false means the volume
// did not allow a move and the caller must fall back to a permanent delete.
func (l *Local) Trash(p string) (bool, error) {
	p = l.Resolve(p)
	if isRootPath(p) || p == l.home {
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
		if isCrossDevice(err) {
			// Different volume: no Trash available there.
			return false, nil
		}
		return false, err
	}
	return true, nil
}
