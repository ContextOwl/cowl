package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"golang.org/x/term"
)

// probeToken checks a token against the API without needing any specific
// permission: 200, 403, or 402 means the key authenticated; 401 means it did
// not. detail explains a permission or plan gate for `auth status`.
func (a *App) probeToken() (workspaces int, readable bool, detail string, err error) {
	raw, err := a.request("GET", "/api/v1/workspaces", nil, nil)
	if err == nil {
		var list []json.RawMessage
		if jerr := json.Unmarshal(raw, &list); jerr == nil {
			return len(list), true, "", nil
		}
		return 0, true, "", nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusForbidden || apiErr.StatusCode == http.StatusPaymentRequired) {
		detail = "workspace.read not granted"
		if apiErr.Code == "upgrade_required" {
			detail = "workspace listing needs a paid plan"
		}
		return 0, false, detail, nil // authenticated either way
	}
	return 0, false, "", err
}

func readToken(a *App, withToken bool) (string, error) {
	if a.g.token != "" {
		return a.g.token, nil
	}
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
		return "", fmt.Errorf("read token from stdin: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func cmdAuthLogin() *Command {
	var withToken bool
	return &Command{
		Group: "auth", Name: "login",
		Summary: "Verify an agent key and store it in the config file",
		Usage:   "cowl auth login [--with-token < token.txt] [--base-url URL]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&withToken, "with-token", false, "read the token from stdin instead of prompting")
		},
		Run: func(a *App, args []string) error {
			token, err := readToken(a, withToken)
			if err != nil {
				return err
			}
			if token == "" {
				return usageError("no token provided")
			}
			if !strings.HasPrefix(token, apiKeyPrefix) {
				return fmt.Errorf("that does not look like an agent key (expected the %s prefix)", apiKeyPrefix)
			}
			a.token = token
			if _, _, _, err := a.probeToken(); err != nil {
				return fmt.Errorf("token verification failed: %w", err)
			}
			cfg := a.cfg
			cfg.Token = token
			// Persist the host the token was verified against, however it was
			// chosen (flag or env): a later bare invocation must not send this
			// token to the default host.
			cfg.BaseURL = a.baseURL
			if err := saveConfig(a.cfgPath, cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "logged in to %s as %s (config: %s)\n", a.baseURL, displayKey(token), a.cfgPath)
			return nil
		},
	}
}

func cmdAuthStatus() *Command {
	return &Command{
		Group: "auth", Name: "status",
		Summary: "Show which token and base URL are in effect, and verify the token",
		Usage:   "cowl auth status",
		Run: func(a *App, args []string) error {
			fmt.Fprintf(a.Out, "base url:  %s\n", a.baseURL)
			fmt.Fprintf(a.Out, "config:    %s\n", a.cfgPath)
			if a.token == "" {
				fmt.Fprintln(a.Out, "token:     none")
				return fmt.Errorf("not authenticated: run 'cowl auth login' or set COWL_PAT")
			}
			fmt.Fprintf(a.Out, "token:     %s (from %s)\n", displayKey(a.token), a.tokenFrom)
			count, readable, detail, err := a.probeToken()
			if err != nil {
				return fmt.Errorf("token check failed: %w", err)
			}
			if readable {
				fmt.Fprintf(a.Out, "status:    valid (%d workspaces visible)\n", count)
			} else {
				fmt.Fprintf(a.Out, "status:    valid (%s)\n", detail)
			}
			return nil
		},
	}
}

func cmdAuthLogout() *Command {
	return &Command{
		Group: "auth", Name: "logout",
		Summary: "Remove the stored token from the config file",
		Usage:   "cowl auth logout",
		Run: func(a *App, args []string) error {
			if a.cfg.Token == "" {
				fmt.Fprintln(a.Out, "no stored token")
				return nil
			}
			cfg := a.cfg
			cfg.Token = ""
			if err := saveConfig(a.cfgPath, cfg); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "logged out (removed token from %s)\n", a.cfgPath)
			if a.env("COWL_PAT") != "" || a.env("CONTEXTOWL_PAT") != "" {
				fmt.Fprintln(a.Err, "note: COWL_PAT/CONTEXTOWL_PAT is still set in your environment")
			}
			return nil
		},
	}
}
