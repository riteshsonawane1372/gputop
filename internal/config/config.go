// Copyright 2026 The gputop Authors
// SPDX-License-Identifier: Apache-2.0

// Package config defines gputop's configuration file, defaults and
// validation.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/riteshsonawane1372/gputop/internal/paths"
	"go.yaml.in/yaml/v3"
)

// Config is the complete gputop configuration.
type Config struct {
	Refresh    Refresh               `yaml:"refresh"`
	History    History               `yaml:"history"`
	Theme      Theme                 `yaml:"theme"`
	Keys       map[string]StringList `yaml:"keys,omitempty"`
	GPU        GPU                   `yaml:"gpu"`
	Kubernetes Kubernetes            `yaml:"kubernetes"`
	Inference  Inference             `yaml:"inference"`
	Remote     Remote                `yaml:"remote"`
	Server     Server                `yaml:"server"`
	Prometheus Prometheus            `yaml:"prometheus"`
	Logging    Logging               `yaml:"logging"`
	UI         UI                    `yaml:"ui"`
}

// Refresh controls collection scheduling tiers.
type Refresh struct {
	// Interval is the fast tier: utilization, memory, power, temperature.
	Interval Duration `yaml:"interval"`
	// Normal tier: processes, health counters, interconnect links.
	Normal Duration `yaml:"normal"`
	// Slow tier: partitions (MIG), disks, Kubernetes metadata.
	Slow Duration `yaml:"slow"`
	// Inventory tier: device discovery and static information.
	Inventory Duration `yaml:"inventory"`
	// Timeout bounds a single collection pass.
	Timeout Duration `yaml:"timeout"`
}

// History configures the local time-series store.
type History struct {
	Enabled    bool     `yaml:"enabled"`
	Retention  Duration `yaml:"retention"`
	Resolution Duration `yaml:"resolution"`
	// Persist writes history to disk so it survives restarts.
	Persist bool   `yaml:"persist"`
	Path    string `yaml:"path,omitempty"`
	// MaxDiskMB bounds on-disk usage; oldest segments are removed first.
	MaxDiskMB int `yaml:"max_disk_mb"`
}

// Theme selects and customizes the color theme.
type Theme struct {
	Name string `yaml:"name"`
	// Colors override individual palette entries of the selected theme.
	Colors map[string]string `yaml:"colors,omitempty"`
	// Transparent uses the terminal's own background instead of painting.
	Transparent bool `yaml:"transparent"`
}

// GPU configures accelerator providers and derived metrics.
type GPU struct {
	// Providers to enable: "nvidia", "apple", or "auto" (Apple silicon on
	// macOS, NVIDIA elsewhere).
	Providers StringList `yaml:"providers"`
	// IdleThreshold is the utilization (percent) below which a GPU counts
	// as idle.
	IdleThreshold float64 `yaml:"idle_threshold"`
	// IdleAfter is how long a GPU must stay idle before it is flagged.
	IdleAfter Duration `yaml:"idle_after"`
	// OutlierMinDelta is the minimum utilization gap (percentage points)
	// from the cohort median before a GPU is flagged as an outlier.
	OutlierMinDelta float64 `yaml:"outlier_min_delta"`
	NVIDIA          NVIDIA  `yaml:"nvidia"`
}

// NVIDIA holds NVIDIA provider options.
type NVIDIA struct {
	LibraryPaths []string `yaml:"library_paths,omitempty"`
}

// Kubernetes configures Kubernetes awareness.
type Kubernetes struct {
	Enabled AutoBool `yaml:"enabled"`
	// NodeName overrides node detection (defaults to $NODE_NAME, then hostname).
	NodeName string `yaml:"node_name,omitempty"`
	// PodLogsDir is scanned to resolve pod UIDs to names on a node.
	PodLogsDir string `yaml:"pod_logs_dir"`
	// API enables querying the API server with the in-cluster service account.
	API AutoBool `yaml:"api"`
}

// Inference configures scraping of LLM inference servers (vLLM, SGLang,
// TGI, llama.cpp) for serving metrics such as time to first token.
type Inference struct {
	Enabled bool `yaml:"enabled"`
	// Discover scrapes inference servers recognized among GPU processes.
	Discover bool `yaml:"discover"`
	// Window is the span rates and latency percentiles are computed over.
	Window    Duration            `yaml:"window"`
	Endpoints []InferenceEndpoint `yaml:"endpoints,omitempty"`
}

// InferenceEndpoint is an explicitly configured metrics endpoint.
type InferenceEndpoint struct {
	Name string `yaml:"name"`
	// URL of the server's Prometheus metrics, e.g. http://10.0.0.5:8000/metrics.
	URL string `yaml:"url"`
}

// Remote lists remote gputop agents.
type Remote struct {
	Nodes []RemoteNode `yaml:"nodes,omitempty"`
}

// RemoteNode is one remote gputop agent.
type RemoteNode struct {
	Name string `yaml:"name"`
	// Address is a host, host:port or https:// URL of the agent.
	Address string `yaml:"address"`
	Port    int    `yaml:"port,omitempty"`
	// TokenFile / TokenEnv supply the bearer token. Tokens are never stored
	// in the config file itself.
	TokenFile string `yaml:"token_file,omitempty"`
	TokenEnv  string `yaml:"token_env,omitempty"`
	// CAFile verifies the agent certificate (system roots otherwise).
	CAFile     string `yaml:"ca_file,omitempty"`
	ServerName string `yaml:"server_name,omitempty"`
	// CertFile/KeyFile enable mutual TLS.
	CertFile string `yaml:"cert_file,omitempty"`
	KeyFile  string `yaml:"key_file,omitempty"`
}

// Server configures service (agent) mode.
type Server struct {
	Listen       string `yaml:"listen"`
	TLSCertFile  string `yaml:"tls_cert_file,omitempty"`
	TLSKeyFile   string `yaml:"tls_key_file,omitempty"`
	ClientCAFile string `yaml:"client_ca_file,omitempty"`
	// TokenFile contains the bearer token; TokenSHA256 holds its hex
	// SHA-256 digest so the plaintext token need not exist on the server.
	TokenFile   string `yaml:"token_file,omitempty"`
	TokenSHA256 string `yaml:"token_sha256,omitempty"`
	// ExposeProcesses controls whether process names/users are served.
	ExposeProcesses bool `yaml:"expose_processes"`
}

// Prometheus configures the /metrics endpoint in service mode.
type Prometheus struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

// Logging configures diagnostics logging.
type Logging struct {
	Level string `yaml:"level"`
	File  string `yaml:"file,omitempty"`
}

// UI holds presentation preferences.
type UI struct {
	// DefaultTab is the tab shown on start.
	DefaultTab string `yaml:"default_tab"`
	// TemperatureUnit is "c" or "f".
	TemperatureUnit string `yaml:"temperature_unit"`
	// ShowCommandLines displays full command lines in the process view.
	ShowCommandLines bool `yaml:"show_command_lines"`
	// Mouse enables clicking tabs, rows and column headers, and wheel
	// scrolling. Most terminals still select text with Shift (Option in
	// macOS Terminal/iTerm2) held down.
	Mouse bool `yaml:"mouse"`
}

// DefaultPort is the default service port.
const DefaultPort = 9469

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		Refresh: Refresh{
			Interval:  Duration(time.Second),
			Normal:    Duration(3 * time.Second),
			Slow:      Duration(30 * time.Second),
			Inventory: Duration(5 * time.Minute),
			Timeout:   Duration(5 * time.Second),
		},
		History: History{
			Enabled: true, Retention: Duration(30 * time.Minute), Resolution: Duration(5 * time.Second),
			Persist: true, MaxDiskMB: 256,
		},
		Theme:      Theme{Name: "green"},
		GPU:        GPU{Providers: StringList{"auto"}, IdleThreshold: 5, IdleAfter: Duration(5 * time.Minute), OutlierMinDelta: 20},
		Kubernetes: Kubernetes{Enabled: Auto, PodLogsDir: "/var/log/pods", API: Auto},
		Inference:  Inference{Enabled: true, Discover: true, Window: Duration(time.Minute)},
		Server:     Server{Listen: fmt.Sprintf("127.0.0.1:%d", DefaultPort), ExposeProcesses: true},
		Prometheus: Prometheus{Enabled: true, Path: "/metrics"},
		Logging:    Logging{Level: "warn"},
		UI:         UI{DefaultTab: "overview", TemperatureUnit: "c", Mouse: true},
	}
}

// Load reads the configuration at path over the defaults. A missing file
// is not an error unless mustExist is set (an explicit --config).
func Load(path string, mustExist bool) (Config, error) {
	cfg := Default()
	if path == "" {
		path = paths.ConfigFile()
	}
	b, err := os.ReadFile(paths.Expand(path))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !mustExist {
			return cfg, cfg.Validate()
		}
		return cfg, fmt.Errorf("reading config: %w", err)
	}
	if err := Parse(b, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// Parse decodes YAML over cfg (strict: unknown keys are errors) and validates.
func Parse(b []byte, cfg *Config) error {
	if len(bytes.TrimSpace(b)) > 0 {
		dec := yaml.NewDecoder(bytes.NewReader(b))
		dec.KnownFields(true)
		if err := dec.Decode(cfg); err != nil {
			return err
		}
	}
	return cfg.Validate()
}

// MaxHistoryPoints bounds the in-memory history ring.
const MaxHistoryPoints = 20000

var themeName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// Validate checks the configuration and returns all problems found.
func (c *Config) Validate() error {
	var errs []string
	bad := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	r := c.Refresh
	if r.Interval.D() < 100*time.Millisecond || r.Interval.D() > time.Minute {
		bad("refresh.interval must be between 100ms and 1m (got %s)", r.Interval.D())
	}
	if r.Normal.D() < r.Interval.D() {
		bad("refresh.normal (%s) must be >= refresh.interval (%s)", r.Normal.D(), r.Interval.D())
	}
	if r.Slow.D() < r.Normal.D() {
		bad("refresh.slow (%s) must be >= refresh.normal (%s)", r.Slow.D(), r.Normal.D())
	}
	if r.Inventory.D() < r.Slow.D() {
		bad("refresh.inventory (%s) must be >= refresh.slow (%s)", r.Inventory.D(), r.Slow.D())
	}
	if r.Timeout.D() <= 0 {
		bad("refresh.timeout must be positive")
	}

	h := c.History
	if h.Enabled {
		if h.Resolution.D() < time.Second {
			bad("history.resolution must be at least 1s (got %s)", h.Resolution.D())
		}
		if h.Retention.D() < time.Minute {
			bad("history.retention must be at least 1m (got %s)", h.Retention.D())
		}
		if h.Resolution.D() > 0 && h.Resolution.D() < h.Retention.D() {
			if pts := int(h.Retention.D() / h.Resolution.D()); pts > MaxHistoryPoints {
				bad("history.retention/resolution = %d points exceeds %d; increase history.resolution (e.g. %s)",
					pts, MaxHistoryPoints, FormatDuration((h.Retention.D()/MaxHistoryPoints+time.Second-1)/time.Second*time.Second))
			}
		}
		if h.MaxDiskMB < 1 {
			bad("history.max_disk_mb must be >= 1")
		}
	}

	if !themeName.MatchString(c.Theme.Name) {
		bad("theme.name %q may only contain letters, digits, '.', '_' and '-'", c.Theme.Name)
	}
	if c.GPU.IdleThreshold < 0 || c.GPU.IdleThreshold > 100 {
		bad("gpu.idle_threshold must be within 0..100")
	}
	if c.GPU.OutlierMinDelta <= 0 || c.GPU.OutlierMinDelta > 100 {
		bad("gpu.outlier_min_delta must be within (0, 100]")
	}
	for _, p := range c.GPU.Providers {
		switch p {
		case "auto", "nvidia", "apple":
		default:
			bad("gpu.providers: unknown provider %q (available: auto, nvidia, apple)", p)
		}
	}

	if c.Inference.Enabled && (c.Inference.Window.D() < 5*time.Second || c.Inference.Window.D() > time.Hour) {
		bad("inference.window must be between 5s and 1h (got %s)", c.Inference.Window.D())
	}
	endpointNames := map[string]bool{}
	for i, ep := range c.Inference.Endpoints {
		where := fmt.Sprintf("inference.endpoints[%d]", i)
		if ep.Name == "" {
			bad("%s.name is required", where)
		} else if endpointNames[ep.Name] {
			bad("%s.name %q is duplicated", where, ep.Name)
		}
		endpointNames[ep.Name] = true
		if u, err := url.Parse(ep.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			bad("%s.url %q must be an http:// or https:// URL", where, ep.URL)
		}
	}

	seen := map[string]bool{}
	for i, n := range c.Remote.Nodes {
		where := fmt.Sprintf("remote.nodes[%d]", i)
		if n.Name == "" {
			bad("%s.name is required", where)
		} else if seen[n.Name] {
			bad("%s.name %q is duplicated", where, n.Name)
		}
		seen[n.Name] = true
		if _, err := n.URL(); err != nil {
			bad("%s.address: %v", where, err)
		}
		if n.TokenFile != "" && n.TokenEnv != "" {
			bad("%s: set only one of token_file and token_env", where)
		}
		if (n.CertFile == "") != (n.KeyFile == "") {
			bad("%s: cert_file and key_file must be set together", where)
		}
	}

	s := c.Server
	if _, _, err := net.SplitHostPort(s.Listen); err != nil {
		bad("server.listen %q: %v", s.Listen, err)
	}
	if (s.TLSCertFile == "") != (s.TLSKeyFile == "") {
		bad("server.tls_cert_file and server.tls_key_file must be set together")
	}
	if s.TokenFile != "" && s.TokenSHA256 != "" {
		bad("server: set only one of token_file and token_sha256")
	}
	if s.TokenSHA256 != "" && !regexp.MustCompile(`^[0-9a-fA-F]{64}$`).MatchString(s.TokenSHA256) {
		bad("server.token_sha256 must be a 64-character hex SHA-256 digest")
	}
	if !strings.HasPrefix(c.Prometheus.Path, "/") {
		bad("prometheus.path must start with '/'")
	}
	switch strings.ToLower(c.Logging.Level) {
	case "debug", "info", "warn", "error":
	default:
		bad("logging.level must be debug, info, warn or error")
	}
	switch strings.ToLower(c.UI.TemperatureUnit) {
	case "c", "f":
	default:
		bad("ui.temperature_unit must be \"c\" or \"f\"")
	}

	if len(errs) > 0 {
		return &ValidationError{Problems: errs}
	}
	return nil
}

// ValidationError lists every configuration problem.
type ValidationError struct{ Problems []string }

func (e *ValidationError) Error() string {
	return "invalid configuration:\n  - " + strings.Join(e.Problems, "\n  - ")
}

// URL returns the agent base URL. Plain http is only permitted for
// loopback addresses (e.g. through an SSH tunnel).
func (n RemoteNode) URL() (*url.URL, error) {
	addr := strings.TrimSpace(n.Address)
	if addr == "" {
		return nil, errors.New("address is required")
	}
	if !strings.Contains(addr, "://") {
		addr = "https://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, errors.New("missing host")
	}
	if u.Port() == "" {
		port := n.Port
		if port == 0 {
			port = DefaultPort
		}
		u.Host = net.JoinHostPort(u.Hostname(), fmt.Sprint(port))
	}
	if u.Scheme == "http" && !IsLoopbackHost(u.Hostname()) {
		return nil, fmt.Errorf("plaintext http is only allowed to loopback addresses; use https for %s", u.Hostname())
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u, nil
}

// IsLoopbackHost reports whether host is localhost or a loopback IP.
func IsLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Marshal renders the configuration as YAML.
func (c Config) Marshal() ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	return buf.Bytes(), enc.Close()
}
