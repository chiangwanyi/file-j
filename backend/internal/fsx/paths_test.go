package fsx

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The path helpers are the one place where the two dialects meet: local paths
// follow the host OS, SFTP paths are always POSIX, and the transfer engine
// calls the same helpers for both.
//
// These tests cover what must hold on every platform; the dialect detection
// itself is asserted in paths_windows_test.go, because isWindowsStyle only
// exists in the Windows build.

func TestJoinDialect(t *testing.T) {
	cases := []struct {
		base, name, want string
	}{
		// POSIX (local disk on macOS, and every SFTP session everywhere).
		{"/srv", "www", "/srv/www"},
		{"/", "etc", "/etc"},
		{"", "etc", "/etc"},
	}
	for _, c := range cases {
		if got := Join(c.base, c.name); got != c.want {
			t.Errorf("Join(%q, %q) = %q, want %q", c.base, c.name, got, c.want)
		}
	}
}

func TestRootPathMatchesHostFS(t *testing.T) {
	// Whatever RootPath claims must be a real directory on this machine,
	// otherwise the "go to root" shortcut would land nowhere.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	root := RootPath(home)
	if root == "" {
		t.Fatal("RootPath returned an empty string")
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		t.Fatalf("RootPath(%q) = %q, which is not a usable directory: %v", home, root, err)
	}
	if !isRootPath(root) {
		t.Errorf("isRootPath(%q) = false, want true", root)
	}
	if runtime.GOOS == "windows" {
		// A Windows root always names a volume; a bare "/" would silently
		// resolve to the process' current drive, which is not what the user means.
		if filepath.VolumeName(root) == "" {
			t.Errorf("RootPath = %q has no volume name", root)
		}
	} else if root != "/" {
		t.Errorf("RootPath on %s = %q, want \"/\"", runtime.GOOS, root)
	}
}

func TestNameProblem(t *testing.T) {
	// Control characters and trailing dots/spaces are rejected on both
	// platforms; the reserved Windows device names are covered in
	// paths_windows_test.go because they are a Windows-only rule.
	bad := []string{"a\x01b", "name ", "name."}
	for _, name := range bad {
		if problem := NameProblem(name); problem == "" {
			t.Errorf("NameProblem(%q) = \"\", want a reason on %s", name, runtime.GOOS)
		}
	}
	good := []string{"normal.txt", "新建文件夹", "a-b_c.d"}
	for _, name := range good {
		if problem := NameProblem(name); problem != "" {
			t.Errorf("NameProblem(%q) = %q, want empty on %s", name, problem, runtime.GOOS)
		}
	}
}