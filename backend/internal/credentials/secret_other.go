//go:build !darwin && !windows

package credentials

// Fallback adapter: no OS secret store, memory only. Secrets work for the
// current run and must be entered again after a restart.

func osStoreAvailable() bool { return false }

func osStoreSet(_, _, _ string) error { return nil }

func osStoreGet(_, _ string) (string, bool) { return "", false }

func osStoreDelete(_, _ string) error { return nil }
