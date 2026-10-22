package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenSeedsDefaults(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	for _, name := range []string{fileConfig} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was not created: %v", name, err)
		}
	}

	cfg := st.Config()
	if cfg.Transfer.Concurrency != 3 {
		t.Errorf("concurrency = %d, want 3", cfg.Transfer.Concurrency)
	}
	if cfg.UI.Theme != "system" {
		t.Errorf("theme = %q", cfg.UI.Theme)
	}
}

func TestConfigRoundTripAndNormalize(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	cfg := st.Config()
	cfg.UI.Theme = "dark"
	cfg.Transfer.Concurrency = 99 // out of range, must clamp
	cfg.Remote.HostKeyPolicy = "bogus"
	if _, err := st.SetConfig(cfg); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got := reopened.Config()
	if got.UI.Theme != "dark" {
		t.Errorf("theme = %q, want dark", got.UI.Theme)
	}
	if got.Transfer.Concurrency != 3 {
		t.Errorf("concurrency = %d, want clamped 3", got.Transfer.Concurrency)
	}
	if got.Remote.HostKeyPolicy != "tofu" {
		t.Errorf("host key policy = %q, want tofu", got.Remote.HostKeyPolicy)
	}
}

func TestSessionsCRUD(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	saved, err := st.SaveSession(Session{Name: "prod", Host: "example.com", User: "deploy", Port: 22})
	if err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if saved.ID == "" {
		t.Fatal("session ID was not generated")
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(reopened.Sessions()) != 1 {
		t.Fatalf("sessions = %d, want 1", len(reopened.Sessions()))
	}

	// Update must not duplicate.
	saved.Name = "prod-2"
	if _, err := reopened.SaveSession(saved); err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(reopened.Sessions()) != 1 {
		t.Fatalf("sessions after update = %d, want 1", len(reopened.Sessions()))
	}
	if reopened.Sessions()[0].Name != "prod-2" {
		t.Errorf("name = %q", reopened.Sessions()[0].Name)
	}

	if err := reopened.DeleteSession(saved.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(reopened.Sessions()) != 0 {
		t.Errorf("sessions after delete = %d", len(reopened.Sessions()))
	}
}

func TestSaveSessionAddsEveryConnection(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Regression: saving a second/third connection used to be indistinguishable
	// from updating the first one, so only one session ever survived.
	ids := map[string]bool{}
	for _, name := range []string{"prod", "staging", "backup"} {
		saved, serr := st.SaveSession(Session{Name: name, Host: name + ".example.com", User: "deploy"})
		if serr != nil {
			t.Fatalf("SaveSession %s: %v", name, serr)
		}
		if saved.ID == "" {
			t.Fatalf("SaveSession %s: id not generated", name)
		}
		if ids[saved.ID] {
			t.Fatalf("SaveSession %s: duplicate id %q", name, saved.ID)
		}
		ids[saved.ID] = true
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	list := reopened.Sessions()
	if len(list) != 3 {
		t.Fatalf("sessions = %d, want 3", len(list))
	}
	if _, err := reopened.SaveSession(Session{Name: "used", Host: "used.example.com", User: "deploy",
		LastUsedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatalf("SaveSession used: %v", err)
	}

	// Ordering: never-connected sessions fall back to CreatedAt, so a freshly
	// added connection is not buried under sessions used long ago.
	freshDir := t.TempDir()
	fresh, err := Open(freshDir)
	if err != nil {
		t.Fatalf("Open fresh: %v", err)
	}
	if _, err := fresh.SaveSession(Session{
		Name: "old", Host: "old.example.com", User: "deploy",
		LastUsedAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("SaveSession old: %v", err)
	}
	if _, err := fresh.SaveSession(Session{Name: "new", Host: "new.example.com", User: "deploy"}); err != nil {
		t.Fatalf("SaveSession new: %v", err)
	}
	if got := fresh.Sessions()[0].Name; got != "new" {
		t.Errorf("first session = %q, want new", got)
	}
}

func TestEmptyHostRejected(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := st.SaveSession(Session{User: "x"}); err == nil {
		t.Fatal("expected error for empty host")
	}
}

func TestUIStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	stt := st.UIState()
	stt.Left.Path = "/tmp"
	stt.SplitRatio = 0.42
	if err := st.SetUIState(stt); err != nil {
		t.Fatalf("SetUIState: %v", err)
	}
	reopened, _ := Open(dir)
	got := reopened.UIState()
	if got.Left.Path != "/tmp" || got.SplitRatio != 0.42 {
		t.Errorf("ui state not persisted: %+v", got)
	}
	// Out-of-range values are clamped, never rejected.
	if err := st.SetUIState(UIState{SplitRatio: 4}); err != nil {
		t.Fatalf("SetUIState clamp: %v", err)
	}
	if st.UIState().SplitRatio != 0.5 {
		t.Errorf("split ratio = %v, want 0.5", st.UIState().SplitRatio)
	}
}

func TestKnownHosts(t *testing.T) {
	dir := t.TempDir()
	st, _ := Open(dir)
	if err := st.PutKnownHost(KnownHost{Host: "Example.COM:22", Algorithm: "ssh-ed25519", Fingerprint: "SHA256:abc"}); err != nil {
		t.Fatalf("PutKnownHost: %v", err)
	}
	got, ok := st.KnownHost("example.com:22")
	if !ok || got.Fingerprint != "SHA256:abc" {
		t.Fatalf("known host = %+v ok=%v", got, ok)
	}
	reopened, _ := Open(dir)
	if _, ok := reopened.KnownHost("EXAMPLE.com:22"); !ok {
		t.Error("known host did not survive reload")
	}
	if err := reopened.DeleteKnownHost("example.com:22"); err != nil {
		t.Fatalf("DeleteKnownHost: %v", err)
	}
	if _, ok := reopened.KnownHost("example.com:22"); ok {
		t.Error("known host still present after delete")
	}
}
