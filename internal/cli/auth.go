package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

func (a *App) getMe() (json.RawMessage, meInfo, error) {
	raw, err := a.request("GET", "/api/v1/me", nil, nil)
	if err != nil {
		return nil, meInfo{}, err
	}
	me, err := decodeMe(raw)
	return raw, me, err
}

func readToken(a *App, withToken bool) (string, error) {
	if !withToken && a.TTY {
		if f, ok := a.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			fmt.Fprint(a.Err, "Paste agent key (input hidden): ")
			b, err := term.ReadPassword(int(f.Fd()))
			fmt.Fprintln(a.Err)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(b)), nil
		}
	}
	// --with-token, or no terminal: read one line from stdin.
	line, err := bufio.NewReader(a.In).ReadString('\n')
	if err != nil && line == "" {
		if errors.Is(err, io.EOF) {
			return "", usageError("no key on stdin: pipe it in, as in 'cowl auth login --with-token < key.txt'")
		}
		return "", fmt.Errorf("read the key from stdin: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func (a *App) workspaceFlagSet() bool {
	return a.flagWasSet("w") || a.flagWasSet("workspace")
}

// loginWorkspace picks the default workspace to save. -w must name a
// workspace the key can reach. Without -w, a saved workspace stays only when
// the new key can still reach it.
func loginWorkspace(a *App, me meInfo) (ws, note string, err error) {
	if a.workspaceFlagSet() {
		switch {
		case a.g.workspace == "":
			return "", "", nil
		case a.g.workspace == "-" && me.BoundWorkspace == nil:
			return "", "", usageError("-w - needs a key bound to one workspace, and this key is org-wide. Pass a workspace id: " + strings.Join(me.workspaceIDs(), ", "))
		case a.g.workspace != "-" && !me.reaches(a.g.workspace):
			return "", "", usageError("this key cannot reach workspace " + a.g.workspace + ". It can reach: " + dash(strings.Join(me.workspaceIDs(), ", ")))
		}
		return a.g.workspace, "", nil
	}
	saved := a.cfg.Workspace
	if saved == "" || saved == "-" || me.reaches(saved) {
		return saved, "", nil
	}
	return "", "the saved workspace " + saved + " is not reachable with this key, so cowl removed it", nil
}

func cmdAuthLogin() *Command {
	var withToken bool
	var baseURL string
	return &Command{
		Group: "auth", Name: "login", OpIDs: []string{"getMe"}, Local: true,
		Summary: "Verify an agent key and save it with its base URL",
		Usage:   "cowl auth login [--with-token] [--base-url URL] [-w WORKSPACE]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&withToken, "with-token", false, "read the key from stdin instead of a hidden prompt")
			fs.StringVar(&baseURL, "base-url", "", "API base URL to save with the key (default CONTEXTOWL_BASE_URL, the saved URL, or "+defaultBaseURL+")")
		},
		Run: func(a *App, args []string) error {
			if len(args) != 0 {
				return usageError("auth login takes no arguments. Pass the key on stdin with --with-token, never as an argument")
			}
			replaced := false
			if err := a.loadConfigFile(); err != nil {
				if a.cfgPath == "" {
					return err
				}
				replaced = true
			}
			if err := a.applySettings(); err != nil {
				return err
			}
			if a.flagWasSet("base-url") {
				norm, err := normalizeBaseURL(baseURL)
				if err != nil {
					return usageError("--base-url " + err.Error())
				}
				a.baseURL, a.baseFrom = norm, "flag"
			}
			token, err := readToken(a, withToken)
			if err != nil {
				return err
			}
			if token == "" {
				return usageError("no key given")
			}
			if !strings.HasPrefix(token, apiKeyPrefix) {
				return usageError("that is not an agent key: agent keys start with " + apiKeyPrefix)
			}
			a.token, a.tokenFrom = token, "login"
			raw, me, err := a.getMe()
			if err != nil {
				return err
			}
			ws, note, err := loginWorkspace(a, me)
			if err != nil {
				return err
			}
			cfg := a.cfg
			cfg.Token, cfg.BaseURL, cfg.Workspace = token, a.baseURL, ws
			if err := saveConfig(a.cfgPath, cfg); err != nil {
				return err
			}
			return a.emit(raw, func() error {
				fmt.Fprintf(a.Out, "logged in to %s with key %q (%s), saved to %s\n",
					a.baseURL, me.Key.Name, keyLabel(me, token), a.cfgPath)
				if ws != "" {
					fmt.Fprintf(a.Out, "default workspace: %s\n", ws)
				}
				if note != "" {
					fmt.Fprintf(a.Err, "note: %s\n", note)
				}
				if replaced {
					fmt.Fprintln(a.Err, "note: the old config file was not valid, so cowl replaced it")
				}
				return nil
			})
		},
	}
}

func keyLabel(me meInfo, token string) string {
	if me.Key.Prefix != "" {
		return me.Key.Prefix + "…"
	}
	return displayKey(token)
}

func cmdAuthStatus() *Command {
	return &Command{
		Group: "auth", Name: "status", OpIDs: []string{"getMe"}, NoKey: true,
		Summary: "Show the key and base URL in use, and check the key with the server",
		Usage:   "cowl auth status",
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			local := [][2]string{
				{"base url:", a.baseURL + " (from " + a.baseFrom + ")"},
				{"config:", a.cfgPath},
			}
			if a.token == "" {
				if !a.jsonOut() {
					a.fields(append(local, [2]string{"key:", "none"}))
				}
				return noKeyError()
			}
			local = append(local, [2]string{"key:", displayKey(a.token) + " (from " + a.tokenFrom + ")"})
			err := a.checkKey()
			var raw json.RawMessage
			var me meInfo
			if err == nil {
				raw, me, err = a.getMe()
			}
			if err != nil {
				if !a.jsonOut() {
					a.fields(append(local, [2]string{"status:", "not valid"}))
				}
				return err
			}
			return a.emit(raw, func() error {
				a.fields(append(append(local, meFields(me)...), [2]string{"status:", "valid"}))
				return nil
			})
		},
	}
}

// meFields describes a key for a person at a terminal.
func meFields(me meInfo) [][2]string {
	expires := "never"
	if me.Key.ExpiresAt != nil {
		expires = timeLabel(*me.Key.ExpiresAt)
	}
	scope := "all workspaces the role allows (org-wide key)"
	if me.BoundWorkspace != nil {
		scope = *me.BoundWorkspace + " (bound key)"
	}
	blocked := make([]string, 0, len(me.Blocked))
	for _, b := range me.Blocked {
		blocked = append(blocked, b.Permission+" ("+b.Reason+")")
	}
	return [][2]string{
		{"name:", fmt.Sprintf("%q", me.Key.Name)},
		{"expires:", expires},
		{"org:", me.Org.Name + " (" + me.Org.ID + ", plan " + me.Org.Plan + ")"},
		{"role:", me.Role},
		{"workspace:", scope},
		{"permissions:", dash(strings.Join(me.Permissions, ", "))},
		{"blocked:", dash(strings.Join(blocked, ", "))},
	}
}

func cmdAuthLogout() *Command {
	return &Command{
		Group: "auth", Name: "logout", NoKey: true,
		Summary: "Remove the saved key from the config file",
		Usage:   "cowl auth logout",
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			removed := a.cfg.Token != ""
			if removed {
				cfg := a.cfg
				cfg.Token = ""
				if err := saveConfig(a.cfgPath, cfg); err != nil {
					return err
				}
			}
			envKey, _ := a.envFirst("CONTEXTOWL_PAT", "COWL_PAT")
			if a.jsonOut() {
				return a.printValue(map[string]bool{"removed": removed, "envKeySet": envKey != ""})
			}
			if removed {
				fmt.Fprintf(a.Out, "logged out (removed the key from %s)\n", a.cfgPath)
			} else {
				fmt.Fprintln(a.Out, "no saved key")
			}
			if envKey != "" {
				fmt.Fprintln(a.Err, "note: CONTEXTOWL_PAT or COWL_PAT is still set in your environment")
			}
			return nil
		},
	}
}

func cmdWhoami() *Command {
	return &Command{
		Name: "whoami", OpIDs: []string{"getMe"},
		Summary: "Show the key, its org, role, workspaces and effective permissions",
		Usage:   "cowl whoami",
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			raw, me, err := a.getMe()
			if err != nil {
				return err
			}
			return a.emit(raw, func() error {
				a.fields(append([][2]string{{"key:", keyLabel(me, a.token)}}, meFields(me)...))
				fmt.Fprintln(a.Out)
				rows := make([][]string, 0, len(me.Workspaces))
				for _, w := range me.Workspaces {
					rows = append(rows, []string{w.ID, w.Name, w.AccessMode})
				}
				a.table([]string{"WORKSPACE", "NAME", "ACCESS"}, rows)
				return nil
			})
		},
	}
}
