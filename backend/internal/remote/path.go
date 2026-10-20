package remote

import "strings"

// toPosixPath normalises a Windows-style path into the POSIX form the SFTP
// protocol uses on the wire: `C:\Users\me` becomes `/C:/Users/me`.
//
// Two sources produce Windows-style paths even though the protocol is POSIX:
//
//   - an SFTP server hosted on Windows (OpenSSH for Windows, the dev-only
//     demo-sshd) can report native paths from RealPath,
//   - tests hand the provider native temp paths.
//
// POSIX paths pass through untouched, so macOS behaviour is exactly what it
// was. The wire form is what pkg/sftp's Windows server maps back to native
// paths (`/C:/x` -> `C:\x`), so the round trip is lossless.
func toPosixPath(p string) string {
	if p == "" {
		return p
	}
	drive := len(p) >= 2 && p[1] == ':' &&
		((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z')) &&
		(len(p) == 2 || p[2] == '\\' || p[2] == '/')
	if drive {
		return "/" + strings.ReplaceAll(p, `\`, "/")
	}
	return p
}
