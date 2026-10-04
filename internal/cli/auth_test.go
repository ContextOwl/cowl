package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, cfg Config) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	return path
}

// noEnvKey leaves only the config file as a key source.
func noEnvKey(cfgPath string) map[string]string {
	return map[string]string{"CONTEXTOWL_CONFIG": cfgPath, "CONTEXTOWL_PAT": "", "CONTEXTOWL_BASE_URL": ""}
}

func TestAuthLoginSavesConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	f := &fakeAPI{body: meJSON}
	url := serve(t, f)
	out, errOut, code := runAt(t, url, []string{"auth", "login", "--with-token", "--base-url", url + "/", "-w", "platform"}, runOpts{
		stdin: "cowl_pat_shinynewtoken\n",
		env:   noEnvKey(cfgPath),
		term:  true,
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "logged in to "+url) || !strings.Contains(out, "default workspace: platform") || strings.Contains(out, "shinynewtoken") {
		t.Errorf("output should confirm without leaking the key: %q", out)
	}
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %o, want 600", info.Mode().Perm())
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil || cfg.Token != "cowl_pat_shinynewtoken" || cfg.BaseURL != url || cfg.Workspace != "platform" {
		t.Errorf("config = %+v, %v; want the key, %s and platform", cfg, err, url)
	}
	req := lastReq(t, f)
	if req.Method != "GET" || req.Path != "/api/v1/me" || req.Auth != "Bearer cowl_pat_shinynewtoken" {
		t.Errorf("login must verify the new key with GET /api/v1/me, got %+v", req)
	}

	out, _, code = runAt(t, url, []string{"auth", "login", "--with-token", "--base-url", url}, runOpts{
		stdin: "cowl_pat_shinynewtoken\n",
		env:   noEnvKey(cfgPath),
	})
	if code != 0 || out != meJSON+"\n" {
		t.Errorf("a pipe gets the getMe body: exit=%d out=%q", code, out)
	}
}

func TestAuthLoginUsesTheEnvironmentBaseURL(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	f := &fakeAPI{body: meJSON}
	url := serve(t, f)
	_, errOut, code := runAt(t, url, []string{"auth", "login", "--with-token"}, runOpts{
		stdin: "cowl_pat_shinynewtoken\n",
		env:   map[string]string{"CONTEXTOWL_CONFIG": cfgPath, "CONTEXTOWL_PAT": ""},
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if cfg, _ := loadConfig(cfgPath); cfg.BaseURL != url {
		t.Errorf("login must save the verified base URL, got %q", cfg.BaseURL)
	}
}

func TestAuthLoginWorkspace(t *testing.T) {
	orgWide := strings.Replace(meJSON, `"boundWorkspace":"platform"`, `"boundWorkspace":null`, 1)
	tests := []struct {
		name    string
		me      string
		saved   string
		args    []string
		code    int
		wantWS  string
		wantErr string
	}{
		{"unreachable workspace", meJSON, "", []string{"-w", "other"}, exitUsage, "", "cannot reach workspace other. It can reach: platform"},
		{"dash needs a bound key", orgWide, "", []string{"-w", "-"}, exitUsage, "", "needs a key bound to one workspace"},
		{"dash with a bound key", meJSON, "", []string{"-w", "-"}, 0, "-", ""},
		{"keeps a reachable saved workspace", meJSON, "platform", nil, 0, "platform", ""},
		{"drops an unreachable saved workspace", meJSON, "old", nil, 0, "", "the saved workspace old is not reachable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath := writeConfig(t, Config{Workspace: tt.saved})
			f := &fakeAPI{body: tt.me}
			url := serve(t, f)
			args := append([]string{"auth", "login", "--with-token", "--base-url", url}, tt.args...)
			_, errOut, code := runAt(t, url, args, runOpts{stdin: "cowl_pat_new\n", env: noEnvKey(cfgPath), term: true})
			if code != tt.code {
				t.Fatalf("exit = %d, want %d: %s", code, tt.code, errOut)
			}
			if !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("stderr missing %q:\n%s", tt.wantErr, errOut)
			}
			cfg, _ := loadConfig(cfgPath)
			if tt.code != 0 {
				if cfg.Token != "" {
					t.Error("a failed login must not save the key")
				}
				return
			}
			if cfg.Workspace != tt.wantWS {
				t.Errorf("saved workspace = %q, want %q", cfg.Workspace, tt.wantWS)
			}
		})
	}
}

func TestAuthLoginFailures(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		stdin   string
		status  int
		code    int
		wantErr string
		request bool
	}{
		{"invalid key", nil, "cowl_pat_badtoken\n", 401, exitAuth, "unauthorized", true},
		{"not an agent key", nil, "sk-something-else\n", 200, exitUsage, "agent keys start with cowl_pat_", false},
		{"empty stdin", nil, "", 200, exitUsage, "no key on stdin", false},
		{"key as an argument", []string{"cowl_pat_inargv"}, "", 200, exitUsage, "never as an argument", false},
		{"bad base URL", []string{"--base-url", "ftp://x.example"}, "cowl_pat_x\n", 200, exitUsage, "--base-url must be an absolute http or https URL", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "config.json")
			f := &fakeAPI{status: tt.status, body: `{"error":{"code":"unauthorized","message":"nope","status":401}}`}
			args := append([]string{"auth", "login", "--with-token"}, tt.args...)
			_, errOut, code := run(t, f, args, runOpts{stdin: tt.stdin, env: map[string]string{"CONTEXTOWL_CONFIG": cfgPath, "CONTEXTOWL_PAT": ""}, term: true})
			if code != tt.code || !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("exit=%d stderr=%q, want %d and %q", code, errOut, tt.code, tt.wantErr)
			}
			if strings.Contains(errOut, "inargv") {
				t.Error("an error must not echo a key")
			}
			if got := len(f.requests()) > 0; got != tt.request {
				t.Errorf("request sent = %v, want %v", got, tt.request)
			}
			if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
				t.Error("config must not be written for a failed login")
			}
		})
	}
}

func TestAuthLoginReplacesACorruptConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(cfgPath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fakeAPI{body: meJSON}
	url := serve(t, f)
	_, errOut, code := runAt(t, url, []string{"auth", "login", "--with-token", "--base-url", url}, runOpts{
		stdin: "cowl_pat_new\n", env: noEnvKey(cfgPath), term: true,
	})
	if code != 0 || !strings.Contains(errOut, "the old config file was not valid") {
		t.Fatalf("exit=%d stderr=%q", code, errOut)
	}
	if cfg, err := loadConfig(cfgPath); err != nil || cfg.Token != "cowl_pat_new" {
		t.Errorf("config = %+v, %v", cfg, err)
	}
}

func TestAuthStatus(t *testing.T) {
	f := &fakeAPI{body: meJSON}
	out, _, code := run(t, f, []string{"auth", "status"}, runOpts{term: true})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"cowl_pat_test0t…", "from env CONTEXTOWL_PAT", "Support agent", "viewer", "plan free", "status:", "valid"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "test0token") {
		t.Errorf("status leaked the key:\n%s", out)
	}
	if req := lastReq(t, f); req.Path != "/api/v1/me" {
		t.Errorf("status must use GET /api/v1/me, got %s", req.Path)
	}
	out, _, code = run(t, f, []string{"auth", "status"}, runOpts{})
	if code != 0 || out != meJSON+"\n" {
		t.Errorf("a pipe gets the getMe body: exit=%d out=%q", code, out)
	}
}

func TestAuthStatusFailures(t *testing.T) {
	f := &fakeAPI{body: meJSON}
	out, errOut, code := run(t, f, []string{"auth", "status"}, runOpts{env: map[string]string{"CONTEXTOWL_PAT": ""}, term: true})
	if code != exitAuth || !strings.Contains(out, "key:") || !strings.Contains(errOut, "no_key") {
		t.Errorf("no key: exit=%d out=%q stderr=%q", code, out, errOut)
	}
	f = &fakeAPI{status: 401, body: `{"error":{"code":"unauthorized","message":"a valid access key is required","status":401}}`}
	out, errOut, code = run(t, f, []string{"auth", "status"}, runOpts{term: true})
	if code != exitAuth || !strings.Contains(out, "not valid") || !strings.Contains(errOut, "unauthorized") {
		t.Errorf("rejected key: exit=%d out=%q stderr=%q", code, out, errOut)
	}
}

func TestAuthLogout(t *testing.T) {
	cfgPath := writeConfig(t, Config{Token: "cowl_pat_old", BaseURL: "https://x.example"})
	f := &fakeAPI{}
	out, _, code := run(t, f, []string{"auth", "logout"}, runOpts{env: noEnvKey(cfgPath), term: true})
	if code != 0 || !strings.Contains(out, "logged out") {
		t.Fatalf("exit=%d out=%q", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Token != "" {
		t.Error("token not cleared")
	}
	if cfg.BaseURL != "https://x.example" {
		t.Error("logout must keep other config fields")
	}
	out, _, code = run(t, f, []string{"auth", "logout"}, runOpts{env: map[string]string{"CONTEXTOWL_CONFIG": cfgPath}})
	if code != 0 || out != `{"envKeySet":true,"removed":false}`+"\n" {
		t.Errorf("a pipe gets a JSON receipt: exit=%d out=%q", code, out)
	}
	if len(f.requests()) != 0 {
		t.Error("logout must not call the API")
	}
}

func TestEnvironmentNames(t *testing.T) {
	cfgPath := writeConfig(t, Config{Workspace: "ws_cfg"})
	f := &fakeAPI{body: `[]`}
	url := serve(t, f)
	tests := []struct {
		name     string
		env      map[string]string
		args     []string
		wantAuth string
		wantPath string
	}{
		{"CONTEXTOWL names", map[string]string{"CONTEXTOWL_PAT": "cowl_pat_a", "CONTEXTOWL_WORKSPACE": "ws_a"}, nil, "Bearer cowl_pat_a", "/api/v1/workspaces/ws_a/articles"},
		{"COWL aliases", map[string]string{"CONTEXTOWL_PAT": "", "COWL_PAT": "cowl_pat_b", "CONTEXTOWL_BASE_URL": "", "COWL_BASE_URL": url, "COWL_WORKSPACE": "ws_b"}, nil, "Bearer cowl_pat_b", "/api/v1/workspaces/ws_b/articles"},
		{"CONTEXTOWL wins over COWL", map[string]string{"CONTEXTOWL_PAT": "cowl_pat_a", "COWL_PAT": "cowl_pat_b", "CONTEXTOWL_WORKSPACE": "ws_a", "COWL_WORKSPACE": "ws_b"}, nil, "Bearer cowl_pat_a", "/api/v1/workspaces/ws_a/articles"},
		{"config workspace", map[string]string{"CONTEXTOWL_PAT": "cowl_pat_a"}, nil, "Bearer cowl_pat_a", "/api/v1/workspaces/ws_cfg/articles"},
		{"flag beats the environment", map[string]string{"CONTEXTOWL_PAT": "cowl_pat_a", "CONTEXTOWL_WORKSPACE": "ws_a"}, []string{"-w", "ws_flag"}, "Bearer cowl_pat_a", "/api/v1/workspaces/ws_flag/articles"},
		{"trimmed values", map[string]string{"CONTEXTOWL_PAT": " cowl_pat_a\n", "CONTEXTOWL_BASE_URL": url + "/\n"}, nil, "Bearer cowl_pat_a", "/api/v1/workspaces/ws_cfg/articles"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{"CONTEXTOWL_CONFIG": cfgPath}
			for k, v := range tt.env {
				env[k] = v
			}
			_, errOut, code := runAt(t, url, append([]string{"articles", "list"}, tt.args...), runOpts{env: env})
			if code != 0 {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			if req := lastReq(t, f); req.Auth != tt.wantAuth || req.Path != tt.wantPath {
				t.Errorf("request = %s %s, want %s %s", req.Auth, req.Path, tt.wantAuth, tt.wantPath)
			}
		})
	}
	t.Run("COWL_CONFIG alias", func(t *testing.T) {
		saved := writeConfig(t, Config{Token: "cowl_pat_saved", BaseURL: url})
		_, errOut, code := runAt(t, url, []string{"articles", "list"}, runOpts{env: map[string]string{
			"CONTEXTOWL_CONFIG": "", "COWL_CONFIG": saved, "CONTEXTOWL_PAT": "", "CONTEXTOWL_BASE_URL": "",
		}})
		if code != 0 || lastReq(t, f).Auth != "Bearer cowl_pat_saved" {
			t.Errorf("exit=%d stderr=%s", code, errOut)
		}
	})
}

func TestHostBinding(t *testing.T) {
	f := &fakeAPI{body: `[]`}
	url := serve(t, f)
	tests := []struct {
		name string
		cfg  Config
		env  map[string]string
		ok   bool
	}{
		{"saved key goes to its saved host", Config{Token: "cowl_pat_c", BaseURL: url}, map[string]string{"CONTEXTOWL_PAT": "", "CONTEXTOWL_BASE_URL": ""}, true},
		{"saved key with the same host in the environment", Config{Token: "cowl_pat_c", BaseURL: url}, map[string]string{"CONTEXTOWL_PAT": "", "CONTEXTOWL_BASE_URL": url + "/"}, true},
		{"saved key never goes to another host", Config{Token: "cowl_pat_c", BaseURL: "https://saved.example"}, map[string]string{"CONTEXTOWL_PAT": "", "CONTEXTOWL_BASE_URL": url}, false},
		{"saved key without a saved host belongs to the default host", Config{Token: "cowl_pat_c"}, map[string]string{"CONTEXTOWL_PAT": "", "CONTEXTOWL_BASE_URL": url}, false},
		{"environment key goes to the environment host", Config{BaseURL: "https://saved.example"}, map[string]string{"CONTEXTOWL_PAT": "cowl_pat_e", "CONTEXTOWL_BASE_URL": url}, true},
		{"environment key never goes to the saved host", Config{BaseURL: url}, map[string]string{"CONTEXTOWL_PAT": "cowl_pat_e", "CONTEXTOWL_BASE_URL": ""}, false},
		{"COWL alias base URL counts as the environment", Config{BaseURL: "https://saved.example"}, map[string]string{"CONTEXTOWL_PAT": "", "COWL_PAT": "cowl_pat_e", "CONTEXTOWL_BASE_URL": "", "COWL_BASE_URL": url}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(f.requests())
			env := map[string]string{"CONTEXTOWL_CONFIG": writeConfig(t, tt.cfg)}
			for k, v := range tt.env {
				env[k] = v
			}
			_, errOut, code := runAt(t, url, []string{"articles", "list"}, runOpts{env: env})
			sent := len(f.requests()) > before
			if tt.ok {
				if code != 0 || !sent {
					t.Errorf("exit=%d sent=%v stderr=%s", code, sent, errOut)
				}
				return
			}
			if code != exitAuth || sent {
				t.Errorf("exit=%d sent=%v, want exit 4 and no request", code, sent)
			}
			if got := decodeEnvelope(t, errOut); got.Code != "untrusted_host" {
				t.Errorf("envelope = %+v", got)
			}
		})
	}
}

func TestCheckKeyDefaultHost(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		env  map[string]string
		ok   bool
	}{
		{"environment key alone goes to the default host", Config{}, map[string]string{"CONTEXTOWL_PAT": "cowl_pat_e"}, true},
		{"environment key with the default host saved", Config{BaseURL: defaultBaseURL + "/"}, map[string]string{"CONTEXTOWL_PAT": "cowl_pat_e"}, true},
		{"legacy saved key without a host", Config{Token: "cowl_pat_c"}, nil, true},
		{"saved key for the default host, environment points elsewhere", Config{Token: "cowl_pat_c", BaseURL: defaultBaseURL}, map[string]string{"CONTEXTOWL_BASE_URL": "https://other.example"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{IO: IO{Env: func(k string) string { return tt.env[k] }}, cfg: tt.cfg}
			if err := a.applySettings(); err != nil {
				t.Fatal(err)
			}
			err := a.checkKey()
			if (err == nil) != tt.ok {
				t.Errorf("checkKey() = %v, want ok %v (base %s from %s, key from %s)", err, tt.ok, a.baseURL, a.baseFrom, a.tokenFrom)
			}
		})
	}
}

func TestBaseURLValidation(t *testing.T) {
	for in, want := range map[string]string{
		"https://Docs.Example.com/": "https://docs.example.com",
		"http://localhost:8080//":   "http://localhost:8080",
		"https://example.com/base/": "https://example.com/base",
	} {
		if got, err := normalizeBaseURL(in); err != nil || got != want {
			t.Errorf("normalizeBaseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "example.com", "ftp://example.com", "https://user:pw@example.com", "https://example.com/?a=1", "https://example.com/#x"} {
		if _, err := normalizeBaseURL(bad); err == nil {
			t.Errorf("normalizeBaseURL(%q) must fail", bad)
		}
	}
	f := &fakeAPI{}
	_, errOut, code := runAt(t, "not a url", []string{"articles", "list"}, runOpts{})
	if got := decodeEnvelope(t, errOut); code != exitError || got.Code != "config_error" || len(f.requests()) != 0 {
		t.Errorf("exit=%d envelope=%+v", code, got)
	}
}

func TestLocalCommandsSurviveBrokenConfig(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &fakeAPI{body: meJSON}
	for _, args := range [][]string{{"version"}, {"completion", "bash"}, {"doctor"}} {
		_, errOut, code := run(t, f, args, runOpts{env: map[string]string{"CONTEXTOWL_CONFIG": bad}})
		if code != 0 {
			t.Errorf("%v with corrupt config: exit %d, stderr %s", args, code, errOut)
		}
	}
	_, errOut, code := run(t, f, []string{"articles", "list"}, runOpts{env: map[string]string{"CONTEXTOWL_CONFIG": bad}, term: true})
	if code != exitError || !strings.Contains(errOut, "config_error: parse config") {
		t.Errorf("corrupt config not surfaced: exit %d, stderr %s", code, errOut)
	}
}

func doctorJSON(t *testing.T, out string) doctorReport {
	t.Helper()
	var rep doctorReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("doctor output is not JSON: %v\n%s", err, out)
	}
	return rep
}

func TestDoctor(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		f := &fakeAPI{body: meJSON}
		out, errOut, code := run(t, f, []string{"doctor"}, runOpts{})
		if code != 0 {
			t.Fatalf("exit %d: %s", code, errOut)
		}
		rep := doctorJSON(t, out)
		if rep.API.Result != "ok" || rep.Token != "env CONTEXTOWL_PAT" || rep.BaseURL != "localhost" || rep.Role != "viewer" ||
			rep.Plan != "free" || rep.KeyScope != "workspace" || rep.Workspaces == nil || *rep.Workspaces != 1 {
			t.Errorf("report = %+v", rep)
		}
		if !strings.Contains(strings.Join(rep.Notes, " "), "workspace.create") {
			t.Errorf("notes must name the permission the plan blocks: %v", rep.Notes)
		}
		for _, secret := range []string{"cowl_pat_test0", "Support agent", "ContextOwl Developers", "Developer Docs", "developers", "platform", "127.0.0.1", "config.json"} {
			if strings.Contains(out, secret) {
				t.Errorf("doctor printed %q:\n%s", secret, out)
			}
		}
		term, _, code := run(t, f, []string{"doctor"}, runOpts{term: true})
		if code != 0 || !strings.Contains(term, "api          ok, HTTP 200") || !strings.Contains(term, "\nnotes\n- ") {
			t.Errorf("terminal report:\n%s", term)
		}
	})
	t.Run("no key", func(t *testing.T) {
		f := &fakeAPI{}
		out, _, code := run(t, f, []string{"doctor"}, runOpts{env: map[string]string{"CONTEXTOWL_PAT": "", "CONTEXTOWL_BASE_URL": ""}})
		rep := doctorJSON(t, out)
		if code != 0 || rep.Token != "none" || rep.BaseURL != "default" || rep.Config != "none" || rep.API.Result != "skipped" ||
			!strings.Contains(rep.Notes[0], "cowl auth login") || len(f.requests()) != 0 {
			t.Errorf("exit=%d report=%+v", code, rep)
		}
	})
	t.Run("rejected key", func(t *testing.T) {
		f := &fakeAPI{status: 401, body: `{"error":{"code":"unauthorized","message":"no","status":401}}`}
		out, _, code := run(t, f, []string{"doctor"}, runOpts{})
		rep := doctorJSON(t, out)
		if code != 0 || rep.API.Result != "http_error" || rep.API.Status != 401 || rep.API.Code != "unauthorized" ||
			!strings.Contains(rep.Notes[0], "Admin > Settings > API") {
			t.Errorf("exit=%d report=%+v", code, rep)
		}
	})
	t.Run("old server", func(t *testing.T) {
		f := &fakeAPI{status: 404, body: `{"error":{"code":"not_found","message":"no route","status":404}}`}
		out, _, _ := run(t, f, []string{"doctor"}, runOpts{})
		if rep := doctorJSON(t, out); !strings.Contains(rep.Notes[0], "GET /api/v1/me") {
			t.Errorf("report = %+v", rep)
		}
	})
	t.Run("host mismatch", func(t *testing.T) {
		f := &fakeAPI{}
		cfgPath := writeConfig(t, Config{Token: "cowl_pat_c", BaseURL: "https://saved.example"})
		out, _, code := run(t, f, []string{"doctor"}, runOpts{env: map[string]string{"CONTEXTOWL_CONFIG": cfgPath, "CONTEXTOWL_PAT": ""}})
		rep := doctorJSON(t, out)
		if code != 0 || rep.Token != "config" || rep.API.Result != "skipped" || len(f.requests()) != 0 || strings.Contains(out, "saved.example") {
			t.Errorf("exit=%d report=%+v", code, rep)
		}
	})
	t.Run("network down", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		out, _, code := runAt(t, url, []string{"doctor"}, runOpts{})
		rep := doctorJSON(t, out)
		if code != 0 || rep.API.Result != "network_error" || rep.API.Code != "refused" || strings.Contains(out, url) {
			t.Errorf("exit=%d report=%+v", code, rep)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		old := doctorTimeout
		doctorTimeout = 50 * time.Millisecond
		defer func() { doctorTimeout = old }()
		f := &fakeAPI{fn: func(r *http.Request) (int, string) {
			time.Sleep(300 * time.Millisecond)
			return 200, meJSON
		}}
		out, _, code := run(t, f, []string{"doctor"}, runOpts{})
		if rep := doctorJSON(t, out); code != 0 || rep.API.Result != "network_error" || rep.API.Code != "timeout" {
			t.Errorf("exit=%d report=%+v", code, rep)
		}
	})
	t.Run("corrupt config", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(bad, []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		f := &fakeAPI{body: meJSON}
		out, _, code := run(t, f, []string{"doctor"}, runOpts{env: map[string]string{"CONTEXTOWL_CONFIG": bad}})
		rep := doctorJSON(t, out)
		if code != 0 || rep.Config != "invalid" || rep.API.Result != "ok" || strings.Contains(out, bad) {
			t.Errorf("exit=%d report=%+v", code, rep)
		}
	})
}
