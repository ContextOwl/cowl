package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
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
	baseURL   string
	token     string
	workspace string
	config    string
	jsonOut   bool
}

func registerGlobals(fs *flag.FlagSet, g *globals) {
	fs.StringVar(&g.baseURL, "base-url", "", "API base URL (default "+defaultBaseURL+")")
	fs.StringVar(&g.token, "token", "", "agent key (prefer COWL_PAT or 'cowl auth login')")
	fs.StringVar(&g.workspace, "workspace", "", "workspace id, or - for the key's bound workspace")
	fs.StringVar(&g.workspace, "w", "", "shorthand for --workspace")
	fs.BoolVar(&g.jsonOut, "json", false, "print the raw API response as JSON")
	fs.StringVar(&g.config, "config", "", "config file (default COWL_CONFIG or the user config dir)")
}

// App is the per-invocation state: parsed flags resolved against environment
// and config file, plus the HTTP client.
type App struct {
	IO
	g       globals
	fs      *flag.FlagSet
	cfg     Config
	cfgPath string

	baseURL   string
	token     string
	tokenFrom string // "flag" | "env" | "config" | ""
	workspace string

	http *http.Client
}

func (a *App) env(key string) string {
	if a.Env == nil {
		return ""
	}
	return a.Env(key)
}

// resolve computes effective settings: flag > env > config file > default.
func (a *App) resolve() error {
	path, err := a.configPath()
	if err != nil {
		return err
	}
	a.cfgPath = path
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	a.cfg = cfg

	a.baseURL = firstOf(a.g.baseURL, a.env("COWL_BASE_URL"), cfg.BaseURL, defaultBaseURL)
	a.baseURL = strings.TrimRight(a.baseURL, "/")

	switch {
	case a.g.token != "":
		a.token, a.tokenFrom = a.g.token, "flag"
	case a.env("COWL_PAT") != "":
		a.token, a.tokenFrom = a.env("COWL_PAT"), "env"
	case a.env("CONTEXTOWL_PAT") != "":
		a.token, a.tokenFrom = a.env("CONTEXTOWL_PAT"), "env"
	case cfg.Token != "":
		a.token, a.tokenFrom = cfg.Token, "config"
	}

	a.workspace = firstOf(a.g.workspace, a.env("COWL_WORKSPACE"), cfg.Workspace, "-")
	a.http = &http.Client{Timeout: 2 * time.Minute}
	return nil
}

func (a *App) configPath() (string, error) {
	if a.g.config != "" {
		return a.g.config, nil
	}
	if p := a.env("COWL_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate config dir: %w (set COWL_CONFIG)", err)
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
