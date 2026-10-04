package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config is the on-disk state written by `cowl auth login`. The file is
// created 0600: it holds the agent key.
type Config struct {
	BaseURL   string `json:"base_url,omitempty"`
	Token     string `json:"token,omitempty"`
	Workspace string `json:"workspace,omitempty"`
}

type globals struct {
	workspace string
	config    string
	jsonOut   bool
}

func registerGlobals(fs *flag.FlagSet, g *globals) {
	fs.StringVar(&g.workspace, "workspace", "", "workspace id, or - for the workspace the key is bound to")
	fs.StringVar(&g.workspace, "w", "", "shorthand for --workspace")
	fs.BoolVar(&g.jsonOut, "json", false, "print the API response as JSON, also on a terminal")
	fs.StringVar(&g.config, "config", "", "config file (default CONTEXTOWL_CONFIG, or contextowl/config.json in the user config dir)")
}

// Sources of a setting, as cowl doctor and the errors name them.
const (
	fromDefault = "default"
	fromConfig  = "config"
)

// App is the per-invocation state: parsed flags resolved against environment
// and config file, plus the HTTP client.
type App struct {
	IO
	g        globals
	fs       *flag.FlagSet
	cfg      Config
	cfgPath  string
	cfgFound bool

	baseURL   string
	baseFrom  string
	token     string
	tokenFrom string
	workspace string

	http *http.Client
}

func (a *App) env(key string) string {
	if a.Env == nil {
		return ""
	}
	return strings.TrimSpace(a.Env(key))
}

// envFirst returns the first set variable of names and its source label.
func (a *App) envFirst(names ...string) (string, string) {
	for _, n := range names {
		if v := a.env(n); v != "" {
			return v, "env " + n
		}
	}
	return "", ""
}

// resolve loads the config file and computes the effective settings:
// environment first, then the config file, then the default.
func (a *App) resolve() error {
	if err := a.loadConfigFile(); err != nil {
		return err
	}
	return a.applySettings()
}

func (a *App) loadConfigFile() error {
	path, err := a.configPath()
	if err != nil {
		return &cliError{code: "config_error", message: err.Error(), exit: exitError, cause: err}
	}
	a.cfgPath = path
	cfg, err := loadConfig(path)
	if err != nil {
		return &cliError{code: "config_error", message: err.Error(), exit: exitError, cause: err}
	}
	_, statErr := os.Stat(path)
	a.cfg, a.cfgFound = cfg, statErr == nil
	return nil
}

func (a *App) applySettings() error {
	base, from := a.envFirst("CONTEXTOWL_BASE_URL", "COWL_BASE_URL")
	if base == "" && a.cfg.BaseURL != "" {
		base, from = a.cfg.BaseURL, fromConfig
	}
	if base == "" {
		base, from = defaultBaseURL, fromDefault
	}
	norm, err := normalizeBaseURL(base)
	if err != nil {
		return &cliError{code: "config_error", message: "the base URL from " + from + " " + err.Error(), exit: exitError}
	}
	a.baseURL, a.baseFrom = norm, from

	a.token, a.tokenFrom = a.envFirst("CONTEXTOWL_PAT", "COWL_PAT")
	if a.token == "" && a.cfg.Token != "" {
		a.token, a.tokenFrom = a.cfg.Token, fromConfig
	}
	ws, _ := a.envFirst("CONTEXTOWL_WORKSPACE", "COWL_WORKSPACE")
	a.workspace = firstOf(a.g.workspace, ws, a.cfg.Workspace, "-")
	a.http = &http.Client{Timeout: 2 * time.Minute}
	return nil
}

// checkKey makes sure a key exists and goes only to a host it belongs to.
// A saved key goes only to the base URL saved with it. A key from the
// environment goes to the base URL from the environment, or to the default
// host.
func (a *App) checkKey() error {
	if a.token == "" {
		return noKeyError()
	}
	if a.tokenFrom == fromConfig {
		saved, err := normalizeBaseURL(firstOf(a.cfg.BaseURL, defaultBaseURL))
		if err != nil || saved != a.baseURL {
			return &cliError{code: "untrusted_host", exit: exitAuth, message: fmt.Sprintf(
				"the saved key belongs to %s, but %s sets the base URL to %s. Unset CONTEXTOWL_BASE_URL and COWL_BASE_URL, or run 'cowl auth login' for that host",
				saved, a.baseFrom, a.baseURL)}
		}
		return nil
	}
	if a.baseFrom == fromConfig && a.baseURL != defaultBaseURL {
		return &cliError{code: "untrusted_host", exit: exitAuth, message: fmt.Sprintf(
			"the key from %s goes only to the default host or to CONTEXTOWL_BASE_URL, but the config file sets the base URL to %s. Set CONTEXTOWL_BASE_URL to the host of this key",
			a.tokenFrom, a.baseURL)}
	}
	return nil
}

// normalizeBaseURL checks that s is an absolute http or https URL and removes
// trailing slashes, so equal hosts compare equal.
func normalizeBaseURL(s string) (string, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("must be an absolute http or https URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("must not contain credentials, a query or a fragment")
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + u.EscapedPath(), nil
}

// baseURLKind classifies a base URL without naming it, for cowl doctor.
func baseURLKind(base string) string {
	if base == defaultBaseURL {
		return "default"
	}
	u, err := url.Parse(base)
	if err != nil {
		return "custom"
	}
	host := u.Hostname()
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return "localhost"
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return "localhost"
	}
	return "custom"
}

func (a *App) configPath() (string, error) {
	if a.g.config != "" {
		return a.g.config, nil
	}
	if p, _ := a.envFirst("CONTEXTOWL_CONFIG", "COWL_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot find the user config dir: %w. Set CONTEXTOWL_CONFIG", err)
	}
	return filepath.Join(dir, "contextowl", "config.json"), nil
}

func loadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

func saveConfig(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	// An existing file keeps its old mode on WriteFile; force 0600.
	return os.Chmod(path, 0o600)
}

func firstOf(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// displayKey renders a token safely: the shared non-secret display prefix,
// never the secret itself.
func displayKey(token string) string {
	if p := apiKeyDisplayPrefix(token); len(p) < len(token) {
		return p + "…"
	}
	if len(token) > 6 {
		return token[:6] + "…"
	}
	return "(set)"
}
