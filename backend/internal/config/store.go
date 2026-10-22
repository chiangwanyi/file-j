package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// File names inside the config directory.
const (
	fileConfig    = "config.yaml"
	fileSessions  = "sessions.yaml"
	fileUIState   = "ui-state.yaml"
	fileFavorites = "favorites.yaml"
	fileKnownHost = "known_hosts.yaml"
)

// Store is the single writer for all YAML documents. It is safe for concurrent
// use; every mutating method persists before returning so the on-disk file is
// always the source of truth after a successful call.
type Store struct {
	dir string

	mu        sync.RWMutex
	cfg       Config
	sessions  []Session
	state     UIState
	favorites []Favorite
	known     map[string]KnownHost
}

// Open loads (or seeds) every document under dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	s := &Store{
		dir:   dir,
		cfg:   Defaults(),
		state: defaultUIState(),
		known: map[string]KnownHost{},
	}

	// config.yaml - a broken file is fatal only if it exists and cannot be
	// parsed; we refuse to silently discard a user's settings.
	cfgPath := filepath.Join(dir, fileConfig)
	if err := readYAML(cfgPath, &s.cfg); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", fileConfig, err)
		}
		s.cfg = Defaults()
		if err := s.persistConfigLocked(); err != nil {
			return nil, err
		}
	}
	s.cfg.Normalize()

	var sdoc SessionDoc
	if err := readYAML(filepath.Join(dir, fileSessions), &sdoc); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", fileSessions, err)
		}
		sdoc = SessionDoc{Version: Version}
	}
	s.sessions = sdoc.Sessions
	for i := range s.sessions {
		NormalizeSession(&s.sessions[i])
	}
	if err := readYAML(filepath.Join(dir, fileUIState), &s.state); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", fileUIState, err)
		}
		s.state = defaultUIState()
	}
	s.state = normalizeUIState(s.state)

	// favorites.yaml - a missing file just means "no bookmarks yet"; it is
	// created on the first favorite.
	var fdoc FavoriteDoc
	if err := readYAML(filepath.Join(dir, fileFavorites), &fdoc); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", fileFavorites, err)
		}
		fdoc = FavoriteDoc{Version: Version}
	}
	s.favorites = normalizeFavorites(fdoc.Items)

	var kdoc KnownHostDoc
	if err := readYAML(filepath.Join(dir, fileKnownHost), &kdoc); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read %s: %w", fileKnownHost, err)
		}
		kdoc = KnownHostDoc{Version: Version}
	}
	for _, h := range kdoc.Hosts {
		s.known[strings.ToLower(h.Host)] = h
	}
	return s, nil
}

// Dir returns the directory holding the YAML documents.
func (s *Store) Dir() string { return s.dir }

// Path returns the absolute path of a named document, for logging/UI display.
func (s *Store) Path(name string) string { return filepath.Join(s.dir, name) }

// Config returns a copy of the current settings.
func (s *Store) Config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// SetConfig validates, clamps and persists the full settings document.
func (s *Store) SetConfig(in Config) (Config, error) {
	in.Normalize()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = in
	if err := s.persistConfigLocked(); err != nil {
		return Config{}, err
	}
	return s.cfg, nil
}

// Sessions returns every saved session, most recently used first.
//
// A session that was saved but never connected has a zero LastUsedAt, which
// would push it to the bottom of the list; it falls back to CreatedAt so a
// freshly added connection appears at the top where the user expects it.
func (s *Store) Sessions() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Session, len(s.sessions))
	copy(out, s.sessions)
	sort.SliceStable(out, func(i, j int) bool {
		return sessionStamp(out[i]).After(sessionStamp(out[j]))
	})
	return out
}

// sessionStamp is the time a session is sorted by.
func sessionStamp(s Session) time.Time {
	if !s.LastUsedAt.IsZero() {
		return s.LastUsedAt
	}
	return s.CreatedAt
}

// Session looks up one session by ID.
func (s *Store) Session(id string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.sessions {
		if sess.ID == id {
			return sess, true
		}
	}
	return Session{}, false
}

// SaveSession inserts or updates a session. The ID is generated when empty.
func (s *Store) SaveSession(in Session) (Session, error) {
	NormalizeSession(&in)
	if strings.TrimSpace(in.Host) == "" {
		return Session{}, errors.New("主机地址不能为空")
	}
	if strings.TrimSpace(in.User) == "" {
		return Session{}, errors.New("用户名不能为空")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if in.ID == "" {
		in.ID = newID()
		in.CreatedAt = now
	}
	found := false
	for i := range s.sessions {
		if s.sessions[i].ID == in.ID {
			if in.CreatedAt.IsZero() {
				in.CreatedAt = s.sessions[i].CreatedAt
			}
			s.sessions[i] = in
			found = true
			break
		}
	}
	if !found {
		s.sessions = append(s.sessions, in)
	}
	if err := s.persistSessionsLocked(); err != nil {
		return Session{}, err
	}
	return in, nil
}

// TouchSession records a successful connection for sorting purposes.
func (s *Store) TouchSession(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.sessions {
		if s.sessions[i].ID == id {
			s.sessions[i].LastUsedAt = time.Now()
			_ = s.persistSessionsLocked()
			return
		}
	}
}

// DeleteSession removes a session by ID.
func (s *Store) DeleteSession(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.sessions[:0]
	for _, sess := range s.sessions {
		if sess.ID != id {
			kept = append(kept, sess)
		}
	}
	s.sessions = kept
	return s.persistSessionsLocked()
}

// UIState returns the last window/pane state.
func (s *Store) UIState() UIState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// SetUIState persists pane state. It is called on navigation and on quit, so it
// must stay cheap and must never fail loudly.
func (s *Store) SetUIState(in UIState) error {
	in = normalizeUIState(in)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = in
	return writeYAML(filepath.Join(s.dir, fileUIState), in, 0o600)
}

// Favorites returns the bookmarked local folders in the order they were added.
func (s *Store) Favorites() []Favorite {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Favorite, 0, len(s.favorites))
	out = append(out, s.favorites...)
	return out
}

// AddFavorite bookmarks a folder. Saving the same path twice is a no-op that
// returns the existing entry, so the caller does not have to guard against it
// and the sidebar order stays stable.
func (s *Store) AddFavorite(in Favorite) (Favorite, error) {
	in.Path = strings.TrimSpace(in.Path)
	if in.Path == "" {
		return Favorite{}, errors.New("收藏路径不能为空")
	}
	in.Name = strings.TrimSpace(in.Name)

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, fav := range s.favorites {
		if fav.Path == in.Path {
			return fav, nil
		}
	}
	if in.Name == "" {
		in.Name = in.Path
	}
	in.AddedAt = time.Now()
	s.favorites = append(s.favorites, in)
	if err := s.persistFavoritesLocked(); err != nil {
		return Favorite{}, err
	}
	return in, nil
}

// RemoveFavorite drops a bookmark. Removing an unknown path is not an error:
// two windows may race, and the desired end state (not bookmarked) holds.
func (s *Store) RemoveFavorite(path string) error {
	path = strings.TrimSpace(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.favorites[:0]
	for _, fav := range s.favorites {
		if fav.Path != path {
			kept = append(kept, fav)
		}
	}
	if len(kept) == len(s.favorites) {
		return nil
	}
	s.favorites = kept
	return s.persistFavoritesLocked()
}

// KnownHost returns the remembered key for host:port.
func (s *Store) KnownHost(hostport string) (KnownHost, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.known[strings.ToLower(hostport)]
	return h, ok
}

// PutKnownHost records a host key (TOFU) or replaces a rotated one.
func (s *Store) PutKnownHost(h KnownHost) error {
	h.Host = strings.ToLower(h.Host)
	h.AddedAt = time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.known[h.Host] = h
	return s.persistKnownLocked()
}

// DeleteKnownHost forgets a host key.
func (s *Store) DeleteKnownHost(hostport string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.known, strings.ToLower(hostport))
	return s.persistKnownLocked()
}

// KnownHosts lists remembered keys sorted by host.
func (s *Store) KnownHosts() []KnownHost {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]KnownHost, 0, len(s.known))
	for _, h := range s.known {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Host < out[j].Host })
	return out
}

func (s *Store) persistConfigLocked() error {
	return writeYAML(filepath.Join(s.dir, fileConfig), s.cfg, 0o600)
}

func (s *Store) persistSessionsLocked() error {
	if s.sessions == nil {
		s.sessions = []Session{}
	}
	doc := SessionDoc{Version: Version, Sessions: s.sessions}
	return writeYAML(filepath.Join(s.dir, fileSessions), doc, 0o600)
}

func (s *Store) persistFavoritesLocked() error {
	if s.favorites == nil {
		s.favorites = []Favorite{}
	}
	doc := FavoriteDoc{Version: Version, Items: s.favorites}
	return writeYAML(filepath.Join(s.dir, fileFavorites), doc, 0o600)
}

func (s *Store) persistKnownLocked() error {
	hosts := make([]KnownHost, 0, len(s.known))
	for _, h := range s.known {
		hosts = append(hosts, h)
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].Host < hosts[j].Host })
	doc := KnownHostDoc{Version: Version, Hosts: hosts}
	return writeYAML(filepath.Join(s.dir, fileKnownHost), doc, 0o600)
}

func defaultUIState() UIState {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/"
	}
	return UIState{
		Version: Version,
		Left: PaneState{
			Kind: "local", Path: home, SortKey: "name", SortDesc: false,
		},
		Right: PaneState{
			Kind: "remote", Path: ".", SortKey: "name", SortDesc: false,
		},
		SplitRatio: 0.5,
		ActivePane: "left",
	}
}

func normalizeUIState(in UIState) UIState {
	if in.Left.Kind != "local" && in.Left.Kind != "remote" {
		in.Left.Kind = "local"
	}
	if in.Right.Kind != "local" && in.Right.Kind != "remote" {
		in.Right.Kind = "remote"
	}
	if in.Left.Path == "" {
		in.Left.Path = "/"
	}
	if in.Right.Path == "" {
		in.Right.Path = "."
	}
	if in.SplitRatio < 0.25 || in.SplitRatio > 0.75 {
		in.SplitRatio = 0.5
	}
	if in.ActivePane != "right" {
		in.ActivePane = "left"
	}
	in.Version = Version
	return in
}

// normalizeFavorites drops empty and duplicated entries so a hand-edited file
// cannot make the sidebar render the same folder twice.
func normalizeFavorites(items []Favorite) []Favorite {
	out := make([]Favorite, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, fav := range items {
		fav.Path = strings.TrimSpace(fav.Path)
		if fav.Path == "" || seen[fav.Path] {
			continue
		}
		if fav.Name == "" {
			fav.Name = fav.Path
		}
		seen[fav.Path] = true
		out = append(out, fav)
	}
	return out
}

// writeYAML marshals atomically: write to a sibling temp file, fsync, rename.
// If the rename is refused (some volume formats and endpoint-security agents
// block it) we fall back to an in-place write so settings still persist.
func writeYAML(path string, v any, perm os.FileMode) error {
	data, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("open %s: %w", tmp, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		// Last resort: truncate and rewrite the target in place.
		if werr := os.WriteFile(path, data, perm); werr != nil {
			os.Remove(tmp)
			return fmt.Errorf("rename %s: %w (in-place write: %v)", tmp, err, werr)
		}
		os.Remove(tmp)
	}
	return nil
}

func readYAML(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := yaml.Unmarshal(data, v); err != nil {
		return err
	}
	return nil
}
