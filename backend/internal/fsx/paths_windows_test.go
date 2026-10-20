//go:build windows

package fsx

import "testing"

// Windows is the only platform where local paths can carry a drive letter or a
// UNC prefix, which is exactly what isWindowsStyle has to recognise so that a
// local "C:\dir" never gets treated as a POSIX path (and vice versa).

func TestIsWindowsStyle(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{`C:\Users\me`, true},
		{`c:/Users/me`, true},
		{`Z:\`, true},
		{`\\server\share\dir`, true},
		{"C:relative", true}, // drive-relative still belongs to the Windows side
		{"/srv/www", false},
		{"/", false},
		{".", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isWindowsStyle(c.path); got != c.want {
			t.Errorf("isWindowsStyle(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestWindowsPathHelpers(t *testing.T) {
	joinCases := []struct{ base, name, want string }{
		{`C:\Users`, "me", `C:\Users\me`},
		{`C:\`, "Users", `C:\Users`},
		// A POSIX base must stay POSIX: that is an SFTP path.
		{"/srv", "www", "/srv/www"},
	}
	for _, c := range joinCases {
		if got := Join(c.base, c.name); got != c.want {
			t.Errorf("Join(%q, %q) = %q, want %q", c.base, c.name, got, c.want)
		}
	}

	parentCases := []struct{ path, want string }{
		{`C:\Users\me\Documents`, `C:\Users\me`},
		{`C:\Users`, `C:\`},
		{`C:\`, `C:\`}, // volume root is its own parent
		{"/srv/www", "/srv"},
	}
	for _, c := range parentCases {
		if got := Parent(c.path); got != c.want {
			t.Errorf("Parent(%q) = %q, want %q", c.path, got, c.want)
		}
	}

	baseCases := []struct{ path, want string }{
		{`C:\Users\me`, "me"},
		{`C:\`, `C:\`},
		{"/srv/www", "www"},
	}
	for _, c := range baseCases {
		if got := Base(c.path); got != c.want {
			t.Errorf("Base(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestNameProblemWindowsRules(t *testing.T) {
	// Characters NTFS rejects, plus the device names that stay reserved even
	// with an extension ("CON.txt" is still CON to the kernel).
	bad := []string{
		`a<b`, `a>b`, `a:b`, `a"b`, `a|b`, `a?b`, `a*b`,
		"CON", "con.txt", "NUL", "COM1", "LPT9",
	}
	for _, name := range bad {
		if problem := NameProblem(name); problem == "" {
			t.Errorf("NameProblem(%q) = \"\", want a reason on Windows", name)
		}
	}
}

func TestIsRootPathRejectsNonRoot(t *testing.T) {
	if isRootPath(`C:\Users`) {
		t.Error(`isRootPath("C:\\Users") = true, want false`)
	}
	if !isRootPath(`C:\`) {
		t.Error(`isRootPath("C:\\") = false, want true`)
	}
}