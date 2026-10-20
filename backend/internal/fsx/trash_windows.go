//go:build windows

package fsx

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Trash sends a path to the Recycle Bin, matching Explorer semantics. The bool
// result reports whether the item actually landed there; false means the shell
// refused (network volume, bin disabled, …) and the caller must fall back to a
// permanent delete.
//
// Win32 is reached through SHFileOperationW with FOF_ALLOWUNDO — the same call
// Explorer itself uses for a plain delete. The modern replacement (IFileOperation)
// is COM-heavy and buys nothing for a single-item delete.
func (l *Local) Trash(p string) (bool, error) {
	p = l.Resolve(p)
	if isRootPath(p) || p == l.home {
		return false, errors.New("拒绝删除该路径")
	}

	from, err := windows.UTF16PtrFromString(p + "\x00") // double-NUL terminated list
	if err != nil {
		return false, err
	}
	// SHFileOperation expects a double-NUL terminated list of paths; Go's
	// UTF16PtrFromString already appends one NUL, the extra "\x00" above adds
	// the second.
	fo := &shFileOpW{
		wFunc:  foDelete,
		pFrom:  from,
		fFlags: fofAllowUndo | fofNoConfirm | fofSilent | fofNoErrorUI,
	}
	ret, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(fo)))
	if ret != 0 || fo.fAnyOperationsAborted != 0 {
		// Let the caller decide: fall back to a permanent delete.
		return false, nil
	}
	return true, nil
}

const (
	foDelete           = 3
	fofSilent          = 0x0004
	fofNoConfirm       = 0x0010
	fofAllowUndo       = 0x0040
	fofNoErrorUI       = 0x0400
)

// shFileOpW mirrors SHFILEOPSTRUCTW from <shellapi.h>.
type shFileOpW struct {
	hwnd                  windows.Handle
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         unsafe.Pointer
	lpszProgressTitle     *uint16
}

var (
	shell32                = windows.NewLazySystemDLL("shell32.dll")
	procSHFileOperationW   = shell32.NewProc("SHFileOperationW")
)
