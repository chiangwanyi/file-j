// Package config owns every piece of state that survives an application
// restart. Persistence is plain YAML on disk - no database, no binary blob.
//
// Four independent documents are kept so that a corrupt or hand-edited file
// never takes down the others:
//
//	config.yaml       application settings (theme, transfer defaults, remote policy)
//	sessions.yaml     saved SSH/SFTP sessions (never contains secrets)
//	ui-state.yaml     last used folders, sort order, pane geometry
//	known_hosts.yaml  TOFU host key fingerprints
package config

import "time"

// Version is bumped whenever the on-disk schema changes in a way that needs a
// migration. The store upgrades files in place on load.
const Version = 1

// Config is config.yaml.
type Config struct {
	Version  int      `yaml:"version" json:"version"`
	UI       UI       `yaml:"ui" json:"ui"`
	Transfer Transfer `yaml:"transfer" json:"transfer"`
	Remote   Remote   `yaml:"remote" json:"remote"`
	Logging  Logging  `yaml:"logging" json:"logging"`
}

// UI holds presentation preferences. Values are validated on write.
type UI struct {
	Theme             string `yaml:"theme" json:"theme"`     // system | light | dark
	Density           string `yaml:"density" json:"density"` // comfortable | compact
	ShowHiddenFiles   bool   `yaml:"showHiddenFiles" json:"showHiddenFiles"`
	SidebarVisible    bool   `yaml:"sidebarVisible" json:"sidebarVisible"`
	TransferPanelOpen bool   `yaml:"transferPanelOpen" json:"transferPanelOpen"`
	ConfirmDelete     bool   `yaml:"confirmDelete" json:"confirmDelete"`
	DateFormat        string `yaml:"dateFormat" json:"dateFormat"` // iso | localized
}

// Transfer holds queue and copy behaviour defaults.
type Transfer struct {
	Concurrency         int    `yaml:"concurrency" json:"concurrency"` // simultaneous tasks, 1..8
	Conflict            string `yaml:"conflict" json:"conflict"`       // overwrite | skip | keepboth | fail
	PreserveTimes       bool   `yaml:"preserveTimes" json:"preserveTimes"`
	PreservePermissions bool   `yaml:"preservePermissions" json:"preservePermissions"`
	BufferSizeKB        int    `yaml:"bufferSizeKB" json:"bufferSizeKB"` // 16..1024
}

// Remote holds SSH client policy that applies to every session.
type Remote struct {
	ConnectTimeoutSeconds int    `yaml:"connectTimeoutSeconds" json:"connectTimeoutSeconds"`
	KeepAliveSeconds      int    `yaml:"keepAliveSeconds" json:"keepAliveSeconds"`
	HostKeyPolicy         string `yaml:"hostKeyPolicy" json:"hostKeyPolicy"` // strict | tofu | insecure
	MaxPacketSize         int    `yaml:"maxPacketSize" json:"maxPacketSize"` // sftp packet size in bytes
	IdleDisconnectMinutes int    `yaml:"idleDisconnectMinutes" json:"idleDisconnectMinutes"`
}

// Logging controls the rolling text log written next to the YAML files.
type Logging struct {
	Level  string `yaml:"level" json:"level"` // debug | info | warn | error
	ToFile bool   `yaml:"toFile" json:"toFile"`
}

// Session is one saved remote connection in sessions.yaml.
//
// Secrets are never stored here: AuthMethod describes *how* to authenticate and
// CredentialRef points at a macOS Keychain item holding the password or key
// passphrase. An empty CredentialRef means "ask the user every time".
type Session struct {
	ID            string    `yaml:"id" json:"id"`
	Name          string    `yaml:"name" json:"name"`
	Protocol      string    `yaml:"protocol" json:"protocol"` // sftp (ssh implied)
	Host          string    `yaml:"host" json:"host"`
	Port          int       `yaml:"port" json:"port"`
	User          string    `yaml:"user" json:"user"`
	AuthMethod    string    `yaml:"authMethod" json:"authMethod"` // password | key | agent
	KeyPath       string    `yaml:"keyPath,omitempty" json:"keyPath,omitempty"`
	CredentialRef string    `yaml:"credentialRef,omitempty" json:"credentialRef,omitempty"`
	RemotePath    string    `yaml:"remotePath,omitempty" json:"remotePath,omitempty"`
	LocalPath     string    `yaml:"localPath,omitempty" json:"localPath,omitempty"`
	Color         string    `yaml:"color,omitempty" json:"color,omitempty"`
	Group         string    `yaml:"group,omitempty" json:"group,omitempty"`
	Note          string    `yaml:"note,omitempty" json:"note,omitempty"`
	LastUsedAt    time.Time `yaml:"lastUsedAt,omitempty" json:"lastUsedAt,omitempty"`
	CreatedAt     time.Time `yaml:"createdAt,omitempty" json:"createdAt,omitempty"`
}

// SessionDoc is the sessions.yaml document root.
type SessionDoc struct {
	Version  int       `yaml:"version" json:"version"`
	Sessions []Session `yaml:"sessions" json:"sessions"`
}

// PaneState remembers one pane across restarts.
type PaneState struct {
	Kind      string `yaml:"kind" json:"kind"` // local | remote
	Path      string `yaml:"path" json:"path"`
	SessionID string `yaml:"sessionId,omitempty" json:"sessionId,omitempty"`
	SortKey   string `yaml:"sortKey" json:"sortKey"`
	SortDesc  bool   `yaml:"sortDesc" json:"sortDesc"`
}

// UIState is ui-state.yaml.
type UIState struct {
	Version    int       `yaml:"version" json:"version"`
	Left       PaneState `yaml:"left" json:"left"`
	Right      PaneState `yaml:"right" json:"right"`
	SplitRatio float64   `yaml:"splitRatio" json:"splitRatio"` // 0.25..0.75
	ActivePane string    `yaml:"activePane" json:"activePane"`
}

// KnownHost is one remembered host key in known_hosts.yaml.
type KnownHost struct {
	Host        string    `yaml:"host" json:"host"` // host:port
	Algorithm   string    `yaml:"algorithm" json:"algorithm"`
	Fingerprint string    `yaml:"fingerprint" json:"fingerprint"` // SHA256:...
	AddedAt     time.Time `yaml:"addedAt" json:"addedAt"`
}

// KnownHostDoc is the known_hosts.yaml document root.
type KnownHostDoc struct {
	Version int         `yaml:"version" json:"version"`
	Hosts   []KnownHost `yaml:"hosts" json:"hosts"`
}

// Defaults returns the shipped configuration. Every field is explicit so a
// freshly created config.yaml doubles as documentation.
func Defaults() Config {
	return Config{
		Version: Version,
		UI: UI{
			Theme:             "system",
			Density:           "comfortable",
			ShowHiddenFiles:   false,
			SidebarVisible:    true,
			TransferPanelOpen: true,
			ConfirmDelete:     true,
			DateFormat:        "localized",
		},
		Transfer: Transfer{
			Concurrency:         3,
			Conflict:            "overwrite",
			PreserveTimes:       true,
			PreservePermissions: true,
			BufferSizeKB:        128,
		},
		Remote: Remote{
			ConnectTimeoutSeconds: 15,
			KeepAliveSeconds:      30,
			HostKeyPolicy:         "tofu",
			MaxPacketSize:         32768,
			IdleDisconnectMinutes: 15,
		},
	}
}

// Normalize clamps every value into a supported range. Called after loading a
// file so a hand-edited YAML can never put the app into an unusable state.
func (c *Config) Normalize() {
	def := Defaults()

	switch c.UI.Theme {
	case "system", "light", "dark":
	default:
		c.UI.Theme = def.UI.Theme
	}
	if c.UI.Density != "compact" {
		c.UI.Density = "comfortable"
	}
	if c.UI.DateFormat != "iso" {
		c.UI.DateFormat = "localized"
	}

	if c.Transfer.Concurrency < 1 || c.Transfer.Concurrency > 8 {
		c.Transfer.Concurrency = def.Transfer.Concurrency
	}
	switch c.Transfer.Conflict {
	case "overwrite", "skip", "keepboth", "fail":
	default:
		c.Transfer.Conflict = def.Transfer.Conflict
	}
	if c.Transfer.BufferSizeKB < 16 || c.Transfer.BufferSizeKB > 1024 {
		c.Transfer.BufferSizeKB = def.Transfer.BufferSizeKB
	}

	if c.Remote.ConnectTimeoutSeconds < 3 || c.Remote.ConnectTimeoutSeconds > 120 {
		c.Remote.ConnectTimeoutSeconds = def.Remote.ConnectTimeoutSeconds
	}
	if c.Remote.KeepAliveSeconds < 0 || c.Remote.KeepAliveSeconds > 600 {
		c.Remote.KeepAliveSeconds = def.Remote.KeepAliveSeconds
	}
	switch c.Remote.HostKeyPolicy {
	case "strict", "tofu", "insecure":
	default:
		c.Remote.HostKeyPolicy = def.Remote.HostKeyPolicy
	}
	if c.Remote.MaxPacketSize < 4096 || c.Remote.MaxPacketSize > 262144 {
		c.Remote.MaxPacketSize = def.Remote.MaxPacketSize
	}
	if c.Remote.IdleDisconnectMinutes < 0 {
		c.Remote.IdleDisconnectMinutes = def.Remote.IdleDisconnectMinutes
	}
	c.Version = Version
}

// NormalizeSession fills in defaults for a session coming from the UI.
func NormalizeSession(s *Session) {
	if s.Protocol == "" {
		s.Protocol = "sftp"
	}
	if s.Port == 0 {
		s.Port = 22
	}
	switch s.AuthMethod {
	case "password", "key", "agent":
	default:
		s.AuthMethod = "password"
	}
	if s.Name == "" {
		s.Name = s.User + "@" + s.Host
	}
	if s.RemotePath == "" {
		s.RemotePath = "."
	}
	if s.Color == "" {
		s.Color = "blue"
	}
}
