//go:build windows

package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Platform adapter for Windows 11: volume capacity via GetDiskFreeSpaceExW,
// cross-device detection, Explorer's hidden/read-only attributes and the drive
// list shown in the sidebar. macOS keeps os_unix.go.

var (
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procGetDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
)

// diskUsage reports free bytes for the caller and total bytes of the volume
// holding p (the Win32 semantics of Bavail/Bsize on unix).
func diskUsage(p string) (free, total uint64, ok bool) {
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return 0, 0, false
	}
	var avail, totalBytes, totalFree uint64
	r, _, _ := procGetDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&avail)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r == 0 {
		return 0, 0, false
	}
	return avail, totalBytes, true
}

// isCrossDevice reports whether err is the rename-across-volumes failure
// (ERROR_NOT_SAME_DEVICE, the Win32 equivalent of EXDEV).
//
// It has no caller on Windows: trashing goes through the shell API, which
// handles a cross-volume move itself, and the transfer engine's move fast path
// treats *any* rename failure as "fall back to copy + delete". Kept as the
// documented counterpart of isCrossDevice in os_unix.go so a future caller has
// the same name and semantics on both platforms.
func isCrossDevice(err error) bool {
	return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE)
}

// platformEntryFlags applies the Windows rules: an entry is hidden when it is a
// dot file or has FILE_ATTRIBUTE_HIDDEN, and read-only is surfaced explicitly
// because the Unix permission string does not mean much on NTFS.
func platformEntryFlags(name string, info os.FileInfo) (hidden, readOnly bool) {
	hidden = IsHiddenName(name)
	if fa, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		if fa.FileAttributes&windows.FILE_ATTRIBUTE_HIDDEN != 0 {
			hidden = true
		}
		readOnly = fa.FileAttributes&windows.FILE_ATTRIBUTE_READONLY != 0
	}
	return hidden, readOnly
}

// isRootPath reports whether p is a drive root ("C:\") or a UNC share root.
func isRootPath(p string) bool {
	cleaned := filepath.Clean(p)
	return cleaned == filepath.VolumeName(cleaned)+`\` && filepath.VolumeName(cleaned) != ""
}

// quickRoots lists every mounted drive as a sidebar place.
func quickRoots() []map[string]string {
	out := make([]map[string]string, 0, 8)
	for c := 'A'; c <= 'Z'; c++ {
		drive := string(c) + `:\`
		if st, err := os.Stat(drive); err == nil && st.IsDir() {
			out = append(out, map[string]string{"name": drive, "path": drive})
		}
	}
	return out
}

// RootPath returns the top of the file tree p lives in: the volume root for a
// local path ("C:\"), and "/" for a POSIX path (an SFTP session's tree). This is
// the Windows counterpart of RootPath in os_unix.go and is what the "go to root"
// shortcut asks the backend for, instead of hard-coding "/".
func RootPath(p string) string {
	if !isWindowsStyle(p) {
		return "/"
	}
	cleaned := filepath.Clean(p)
	volume := filepath.VolumeName(cleaned)
	if volume == "" {
		return cleaned
	}
	// UNC shares keep their \\server\share form as the root.
	if strings.HasPrefix(volume, `\\`) {
		rest := strings.TrimPrefix(cleaned, volume)
		if !strings.Contains(rest, `\`) {
			return volume + `\`
		}
		return volume + `\`
	}
	return volume + `\`
}

// windowsReservedNames are device names Windows refuses as file/folder names
// regardless of extension (CON.txt is still CON to the kernel).
var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// NameProblem returns a non-empty reason when name cannot be used as a file
// name on Windows. The caller already rejects "/" and NUL.
func NameProblem(name string) string {
	if strings.ContainsAny(name, `\<>:"|?*`) {
		return "名称包含 Windows 不允许的字符 \\ < > : \" | ? *"
	}
	for _, r := range name {
		if r < 0x20 {
			return "名称包含控制字符"
		}
	}
	if strings.HasSuffix(name, " ") || strings.HasSuffix(name, ".") {
		return "名称不能以空格或点结尾"
	}
	stem := name
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	if windowsReservedNames[strings.ToUpper(stem)] {
		return "名称是 Windows 保留设备名"
	}
	return ""
}
