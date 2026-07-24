package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type captured struct {
	Method string
	Path   string
	Query  string
	Body   string
	Auth   string
}

type fakeAPI struct {
	status int
	body   string
	reqs   []captured
}

func (f *fakeAPI) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		buf.ReadFrom(r.Body)
		f.reqs = append(f.reqs, captured{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery,
			Body: buf.String(), Auth: r.Header.Get("Authorization"),
		})
		status := f.status
		if status == 0 {
			status = 200
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(f.body))
	})
}

type runOpts struct {
	stdin string
	env   map[string]string
	tty   bool
}

// run executes Main hermetically against the fake API and a temp config.
func run(t *testing.T, f *fakeAPI, args []string, opts runOpts) (stdout, stderr string, code int) {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	env := map[string]string{
		"COWL_CONFIG": filepath.Join(t.TempDir(), "config.json"),
	}
	for k, v := range opts.env {
		env[k] = v
	}
	if _, ok := env["COWL_BASE_URL"]; !ok {
		env["COWL_BASE_URL"] = srv.URL
	}
	if _, ok := env["COWL_PAT"]; !ok {
		env["COWL_PAT"] = "cowl_pat_test0token"
	}
	var out, errb bytes.Buffer
	code = Main(args, IO{
		In:  strings.NewReader(opts.stdin),
		Out: &out,
		Err: &errb,
		Env: func(k string) string { return env[k] },
		TTY: opts.tty,
	})
	return out.String(), errb.String(), code
}

func lastReq(t *testing.T, f *fakeAPI) captured {
	t.Helper()
	if len(f.reqs) == 0 {
		t.Fatal("no request reached the API")
	}
	return f.reqs[len(f.reqs)-1]
}

func decodeBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("request body is not a JSON object: %q: %v", body, err)
	}
	return m
}

func TestRequestShapes(t *testing.T) {
	articleMD := "# Hello\n\ncontent"
	tests := []struct {
		name       string
		args       []string
		stdin      string
		response   string
		wantMethod string
		wantPath   string
		wantQuery  string
		wantBody   map[string]any // nil = no body expected
		wantOut    []string
	}{
		{
			name: "workspaces list", args: []string{"workspaces", "list"},
			response:   `[{"id":"ws_1","name":"Docs","color":"#112233","accessMode":"public","llmsTxt":true}]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces",
			wantOut: []string{"ID", "ws_1", "Docs", "public", "yes"},
		},
		{
			name: "workspaces create", args: []string{"workspaces", "create", "New Docs", "--color", "#aabbcc", "--access-mode", "internal"},
			response:   `{"id":"ws_2","name":"New Docs"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces",
			wantBody: map[string]any{"name": "New Docs", "color": "#aabbcc", "access_mode": "internal"},
			wantOut:  []string{"created workspace ws_2"},
		},
		{
			name: "workspaces update explicit false", args: []string{"workspaces", "update", "ws_2", "--llms-txt=false"},
			response:   `{"id":"ws_2"}`,
			wantMethod: "PATCH", wantPath: "/api/v1/workspaces/ws_2",
			wantBody: map[string]any{"llms_txt": false},
			wantOut:  []string{"updated workspace ws_2"},
		},
		{
			name: "workspaces delete", args: []string{"workspaces", "delete", "ws_2", "--yes"},
			response:   `{"deleted":"ws_2"}`,
			wantMethod: "DELETE", wantPath: "/api/v1/workspaces/ws_2",
			wantOut: []string{"deleted workspace ws_2"},
		},
		{
			name: "articles list", args: []string{"articles", "list", "-w", "ws_1"},
			response:   `[{"slug":"intro","title":"Intro","section":"Guides","nav":"guides","status":"published","encrypted":false}]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/ws_1/articles",
			wantOut: []string{"SLUG", "intro", "published"},
		},
		{
			name: "articles get prints markdown", args: []string{"articles", "get", "intro"},
			response:   `{"slug":"intro","markdown":"# Intro\n\nBody."}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/articles/intro",
			wantOut: []string{"# Intro\n\nBody."},
		},
		{
			name: "articles create from stdin", args: []string{"articles", "create", "--title", "Guide", "--file", "-"},
			stdin:      articleMD,
			response:   `{"slug":"guide"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/articles",
			wantBody: map[string]any{"title": "Guide", "markdown": articleMD},
			wantOut:  []string{"created article guide"},
		},
		{
			name: "articles update sends only set flags", args: []string{"articles", "update", "intro", "--status", "STABLE"},
			response:   `{"slug":"intro"}`,
			wantMethod: "PATCH", wantPath: "/api/v1/workspaces/-/articles/intro",
			wantBody: map[string]any{"status": "STABLE"},
			wantOut:  []string{"updated article intro"},
		},
		{
			name: "articles place", args: []string{"articles", "place", "intro", "--section", "guides"},
			response:   `{"slug":"intro","section":"guides"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/articles/intro/placement",
			wantBody: map[string]any{"section": "guides"},
			wantOut:  []string{"placed article intro in guides"},
		},
		{
			name: "sections create", args: []string{"sections", "create", "Getting Started"},
			response:   `{"key":"getting-started","label":"Getting Started"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/sections",
			wantBody: map[string]any{"label": "Getting Started"},
			wantOut:  []string{"created section getting-started"},
		},
		{
			name: "proposals list with status", args: []string{"proposals", "list", "--status", "approved"},
			response:   `[{"id":4,"objectType":"article","status":"approved","author":"bot","note":"","createdAt":"2026-07-19T10:00:00Z"}]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/proposals", wantQuery: "status=approved",
			wantOut: []string{"ID", "article", "approved"},
		},
		{
			name: "proposals create", args: []string{"proposals", "create", "--slug", "intro", "--markdown", "new text", "--note", "typo fix"},
			response:   `{"id":9,"status":"pending"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/proposals",
			wantBody: map[string]any{"slug": "intro", "markdown": "new text", "note": "typo fix"},
			wantOut:  []string{"created proposal 9 (pending)"},
		},
		{
			name: "changelog list drafts", args: []string{"changelog", "list", "--drafts"},
			response:   `[{"id":1,"title":"v2","tags":["feature"],"status":"draft","publishedAt":null}]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/changelog", wantQuery: "drafts=true",
			wantOut: []string{"v2", "draft", "feature"},
		},
		{
			name: "changelog create", args: []string{"changelog", "create", "--title", "v2 ships", "--markdown", "notes", "--tags", "feature, fix", "--status", "published"},
			response:   `{"id":7,"status":"published"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/changelog",
			wantBody: map[string]any{"title": "v2 ships", "markdown": "notes", "tags": []any{"feature", "fix"}, "status": "published"},
			wantOut:  []string{"created changelog entry 7 (published)"},
		},
		{
			name: "changelog update", args: []string{"changelog", "update", "7", "--title", "v2.0.1"},
			response:   `{"id":7}`,
			wantMethod: "PATCH", wantPath: "/api/v1/workspaces/-/changelog/7",
			wantBody: map[string]any{"title": "v2.0.1"},
			wantOut:  []string{"updated changelog entry 7"},
		},
		{
			name: "changelog delete", args: []string{"changelog", "delete", "7", "--yes"},
			response:   `{"deleted":7}`,
			wantMethod: "DELETE", wantPath: "/api/v1/workspaces/-/changelog/7",
			wantOut: []string{"deleted changelog entry 7"},
		},
		{
			name: "landing get", args: []string{"landing", "get"},
			response:   `{"headline":"Docs"}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/landing",
			wantOut: []string{`"headline": "Docs"`},
		},
		{
			name: "landing autofill", args: []string{"landing", "autofill"},
			response:   `{"headline":"Draft"}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/landing/autofill",
			wantOut: []string{`"headline": "Draft"`},
		},
		{
			name: "landing set from stdin", args: []string{"landing", "set", "--file", "-"},
			stdin:      `{"headline":"New"}`,
			response:   `{"headline":"New"}`,
			wantMethod: "PUT", wantPath: "/api/v1/workspaces/-/landing",
			wantBody: map[string]any{"headline": "New"},
			wantOut:  []string{"landing updated"},
		},
		{
			name: "landing propose wraps body", args: []string{"landing", "propose", "--file", "-", "--note", "please"},
			stdin:      `{"headline":"New"}`,
			response:   `{"id":3,"status":"pending"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/landing/proposals",
			wantBody: map[string]any{"landing": map[string]any{"headline": "New"}, "note": "please"},
			wantOut:  []string{"created landing proposal 3 (pending)"},
		},
		{
			name: "openapi status", args: []string{"openapi", "status"},
			response:   `{"configured":true,"specTitle":"API"}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/openapi",
			wantOut: []string{`"configured": true`},
		},
		{
			name: "openapi attach by url", args: []string{"openapi", "attach", "--url", "https://example.com/spec.json"},
			response:   `{"configured":true}`,
			wantMethod: "PUT", wantPath: "/api/v1/workspaces/-/openapi",
			wantBody: map[string]any{"url": "https://example.com/spec.json"},
			wantOut:  []string{`"configured": true`},
		},
		{
			name: "openapi sync", args: []string{"openapi", "sync"},
			response:   `{"configured":true}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/openapi/sync",
			wantOut: []string{`"configured": true`},
		},
		{
			name: "openapi detach", args: []string{"openapi", "detach", "--yes"},
			response:   `{"detached":true,"removed":4}`,
			wantMethod: "DELETE", wantPath: "/api/v1/workspaces/-/openapi",
			wantOut: []string{"4 generated pages removed"},
		},
		{
			name: "openapi pages", args: []string{"openapi", "pages"},
			response:   `{"sections":[{"key":"api","label":"API"}],"pages":[{"slug":"get-users","title":"List users","method":"GET","section":"API","nav":"api"}]}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/openapi/pages",
			wantOut: []string{"SECTION", "api", "get-users", "GET"},
		},
		{
			name: "openapi create-section", args: []string{"openapi", "create-section", "Payments"},
			response:   `{"key":"api-payments","label":"Payments"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/openapi/sections",
			wantBody: map[string]any{"label": "Payments"},
			wantOut:  []string{"created section api-payments"},
		},
		{
			name: "openapi place with position", args: []string{"openapi", "place", "get-users", "--section", "api", "--position", "0"},
			response:   `{"slug":"get-users"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/openapi/pages/get-users/placement",
			wantBody: map[string]any{"section": "api", "position": float64(0)},
			wantOut:  []string{"placed page get-users in api"},
		},
		{
			name: "openapi detach-page", args: []string{"openapi", "detach-page", "get-users"},
			response:   `{"slug":"get-users"}`,
			wantMethod: "DELETE", wantPath: "/api/v1/workspaces/-/openapi/pages/get-users",
			wantOut: []string{"detached page get-users"},
		},
		{
			name: "memory list filtered", args: []string{"memory", "list", "--prefix", "notes/", "--kind", "project"},
			response:   `[{"path":"notes/a","kind":"project","tags":["x"],"size":42,"updatedAt":"2026-07-19T10:00:00Z"}]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/memory", wantQuery: "kind=project&prefix=notes%2F",
			wantOut: []string{"PATH", "notes/a", "project"},
		},
		{
			name: "memory search", args: []string{"memory", "search", "deploy", "steps", "--limit", "5"},
			response:   `{"semantic":false,"results":[{"path":"notes/deploy","kind":"note","size":10,"updatedAt":"2026-07-19T10:00:00Z"}]}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/memory/search", wantQuery: "limit=5&q=deploy+steps",
			wantOut: []string{"notes/deploy"},
		},
		{
			name: "memory get prints body", args: []string{"memory", "get", "notes/a"},
			response:   `{"path":"notes/a","body":"remember this","kind":"note"}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/memory/note", wantQuery: "path=notes%2Fa",
			wantOut: []string{"remember this"},
		},
		{
			name: "memory write", args: []string{"memory", "write", "notes/a", "--body", "fact", "--kind", "project", "--tags", "x,y", "--links", "notes/b"},
			response:   `{"path":"notes/a"}`,
			wantMethod: "PUT", wantPath: "/api/v1/workspaces/-/memory/note",
			wantBody: map[string]any{"path": "notes/a", "body": "fact", "kind": "project", "tags": []any{"x", "y"}, "links": []any{"notes/b"}},
			wantOut:  []string{"wrote memory note notes/a"},
		},
		{
			name: "memory delete", args: []string{"memory", "delete", "notes/a", "--yes"},
			response:   `{"deleted":"notes/a"}`,
			wantMethod: "DELETE", wantPath: "/api/v1/workspaces/-/memory/note", wantQuery: "path=notes%2Fa",
			wantOut: []string{"deleted memory note notes/a"},
		},
		{
			name: "search full-text", args: []string{"search", "sso", "setup"},
			response:   `[{"type":"article","slug":"sso","title":"SSO","snippet":"SSO setup"}]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/search", wantQuery: "q=sso+setup",
			wantOut: []string{"SLUG", "sso", "SSO setup"},
		},
		{
			name: "api escape hatch", args: []string{"api", "get", "workspaces", "--query", "a=1", "--query", "b=2"},
			response:   `[]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces", wantQuery: "a=1&b=2",
			wantOut: []string{"[]"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeAPI{body: tt.response}
			out, errOut, code := run(t, f, tt.args, runOpts{stdin: tt.stdin})
			if code != 0 {
				t.Fatalf("exit %d, stderr: %s", code, errOut)
			}
			req := lastReq(t, f)
			if req.Method != tt.wantMethod || req.Path != tt.wantPath {
				t.Errorf("request = %s %s, want %s %s", req.Method, req.Path, tt.wantMethod, tt.wantPath)
			}
			if req.Query != tt.wantQuery {
				t.Errorf("query = %q, want %q", req.Query, tt.wantQuery)
			}
			if req.Auth != "Bearer cowl_pat_test0token" {
				t.Errorf("auth header = %q", req.Auth)
			}
			if tt.wantBody != nil {
				got := decodeBody(t, req.Body)
				want, _ := json.Marshal(tt.wantBody)
				gotJSON, _ := json.Marshal(got)
				if string(gotJSON) != string(want) {
					t.Errorf("body = %s, want %s", gotJSON, want)
				}
			} else if req.Body != "" {
				t.Errorf("unexpected request body: %q", req.Body)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("stdout missing %q\n---\n%s", want, out)
				}
			}
		})
	}
}

func TestUsageErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"unknown command", []string{"bogus"}, "unknown command"},
		{"missing subcommand", []string{"articles"}, "needs a subcommand"},
		{"articles create needs title", []string{"articles", "create"}, "--title is required"},
		{"articles update needs a field", []string{"articles", "update", "x"}, "nothing to update"},
		{"articles place needs section", []string{"articles", "place", "x"}, "--section is required"},
		{"file and markdown conflict", []string{"articles", "create", "--title", "t", "--file", "a.md", "--markdown", "x"}, "not both"},
		{"proposals need content", []string{"proposals", "create", "--title", "t"}, "markdown content is required"},
		{"changelog id must be int", []string{"changelog", "update", "abc", "--title", "t"}, "ID must be an integer"},
		{"openapi attach xor", []string{"openapi", "attach"}, "exactly one of --url or --file"},
		{"search needs query", []string{"search"}, "QUERY is required"},
		{"api needs valid method", []string{"api", "FETCH", "workspaces"}, "METHOD must be one of"},
		{"api query needs equals", []string{"api", "GET", "workspaces", "--query", "broken"}, "key=value"},
		{"delete refuses without yes non-tty", []string{"workspaces", "delete", "ws_1"}, "--yes"},
		{"completion needs shell", []string{"completion"}, "bash, zsh, fish"},
		{"workspaces create rejects stray args", []string{"workspaces", "create", "My", "Docs"}, "quote multi-word"},
		{"articles update rejects bad status", []string{"articles", "update", "x", "--status", "published"}, "DRAFT, IN REVIEW, BETA, STABLE, DEPRECATED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeAPI{body: `{}`}
			_, errOut, code := run(t, f, tt.args, runOpts{})
			if code != 2 {
				t.Fatalf("exit = %d, want 2; stderr: %s", code, errOut)
			}
			if !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("stderr missing %q\n---\n%s", tt.wantErr, errOut)
			}
			if len(f.reqs) != 0 {
				t.Errorf("request should not have been sent, got %v", f.reqs)
			}
		})
	}
}

func TestAPIErrorEnvelope(t *testing.T) {
	f := &fakeAPI{status: 403, body: `{"error":{"code":"permission_denied","message":"article.create is required","status":403}}`}
	_, errOut, code := run(t, f, []string{"articles", "create", "--title", "x"}, runOpts{})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "permission_denied: article.create is required") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestUnauthorizedHint(t *testing.T) {
	f := &fakeAPI{status: 401, body: `{"error":{"code":"unauthorized","message":"a valid access key is required","status":401}}`}
	_, errOut, code := run(t, f, []string{"articles", "list"}, runOpts{})
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "cowl auth login") {
		t.Errorf("stderr missing login hint: %q", errOut)
	}
}

func TestRateLimitRetries(t *testing.T) {
	old := sleepFn
	var slept time.Duration
	sleepFn = func(d time.Duration) { slept = d }
	defer func() { sleepFn = old }()

	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			w.Write([]byte(`{"error":{"code":"rate_limited","message":"slow down","status":429}}`))
			return
		}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	var out, errb bytes.Buffer
	code := Main([]string{"articles", "list"}, IO{
		In: strings.NewReader(""), Out: &out, Err: &errb,
		Env: func(k string) string {
			switch k {
			case "COWL_BASE_URL":
				return srv.URL
			case "COWL_PAT":
				return "cowl_pat_x"
			case "COWL_CONFIG":
				return filepath.Join(t.TempDir(), "c.json")
			}
			return ""
		},
	})
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errb.String())
	}
	if n != 2 {
		t.Errorf("requests = %d, want 2 (one retry)", n)
	}
	if slept != time.Second {
		t.Errorf("slept %v, want 1s", slept)
	}
}

func TestSemanticSearchFallbackNote(t *testing.T) {
	f := &fakeAPI{body: `{"semantic":false,"results":[{"type":"article","slug":"a","title":"A","snippet":"x"}]}`}
	out, errOut, code := run(t, f, []string{"search", "q", "--semantic", "--limit", "5"}, runOpts{})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	req := lastReq(t, f)
	if !strings.Contains(req.Query, "semantic=true") || !strings.Contains(req.Query, "limit=5") {
		t.Errorf("query = %q", req.Query)
	}
	if !strings.Contains(errOut, "semantic ranking unavailable") {
		t.Errorf("missing fallback note, stderr: %q", errOut)
	}
	if !strings.Contains(out, "a") {
		t.Errorf("results table missing row: %s", out)
	}
}

func TestJSONFlagPassthrough(t *testing.T) {
	f := &fakeAPI{body: `[{"slug":"intro","title":"Intro","section":"G","nav":"g","status":"draft","encrypted":false}]`}
	out, _, code := run(t, f, []string{"articles", "list", "--json"}, runOpts{})
	if code != 0 {
		t.Fatal("exit != 0")
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, out)
	}
	if parsed[0]["slug"] != "intro" {
		t.Errorf("unexpected JSON: %s", out)
	}
}

func TestConfigPrecedence(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := saveConfig(cfgPath, Config{Token: "cowl_pat_fromconfig", Workspace: "ws_cfg"}); err != nil {
		t.Fatal(err)
	}

	f := &fakeAPI{body: `[]`}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()

	env := map[string]string{"COWL_CONFIG": cfgPath, "COWL_BASE_URL": srv.URL}
	var out, errb bytes.Buffer
	code := Main([]string{"articles", "list"}, IO{
		In: strings.NewReader(""), Out: &out, Err: &errb,
		Env: func(k string) string { return env[k] },
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	req := lastReq(t, f)
	if req.Auth != "Bearer cowl_pat_fromconfig" {
		t.Errorf("config token not used: %q", req.Auth)
	}
	if req.Path != "/api/v1/workspaces/ws_cfg/articles" {
		t.Errorf("config workspace not used: %q", req.Path)
	}

	// Env beats config; flag beats env.
	env["COWL_PAT"] = "cowl_pat_fromenv"
	env["COWL_WORKSPACE"] = "ws_env"
	Main([]string{"articles", "list"}, IO{In: strings.NewReader(""), Out: &out, Err: &errb, Env: func(k string) string { return env[k] }})
	if req = lastReq(t, f); req.Auth != "Bearer cowl_pat_fromenv" || req.Path != "/api/v1/workspaces/ws_env/articles" {
		t.Errorf("env should beat config: %+v", req)
	}
	Main([]string{"articles", "list", "--token", "cowl_pat_fromflag", "-w", "ws_flag"}, IO{In: strings.NewReader(""), Out: &out, Err: &errb, Env: func(k string) string { return env[k] }})
	if req = lastReq(t, f); req.Auth != "Bearer cowl_pat_fromflag" || req.Path != "/api/v1/workspaces/ws_flag/articles" {
		t.Errorf("flag should beat env: %+v", req)
	}
}

func TestAuthLoginSavesConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	f := &fakeAPI{body: `[]`}
	out, errOut, code := run(t, f, []string{"auth", "login", "--with-token"}, runOpts{
		stdin: "cowl_pat_shinynewtoken\n",
		env:   map[string]string{"COWL_CONFIG": cfgPath, "COWL_PAT": "", "CONTEXTOWL_PAT": ""},
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "logged in") || strings.Contains(out, "shinynewtoken") {
		t.Errorf("output should confirm without leaking the token: %q", out)
	}
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %o, want 600", info.Mode().Perm())
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil || cfg.Token != "cowl_pat_shinynewtoken" {
		t.Errorf("token not persisted: %+v, %v", cfg, err)
	}
	if cfg.BaseURL == "" || strings.Contains(cfg.BaseURL, "contextowl.co") {
		t.Errorf("login must persist the verified base URL, got %q", cfg.BaseURL)
	}
	// The login probe must have carried the new token.
	if req := lastReq(t, f); req.Auth != "Bearer cowl_pat_shinynewtoken" {
		t.Errorf("probe used %q", req.Auth)
	}
}

func TestAuthLoginRejectsInvalidToken(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	f := &fakeAPI{status: 401, body: `{"error":{"code":"unauthorized","message":"nope","status":401}}`}
	_, errOut, code := run(t, f, []string{"auth", "login", "--with-token"}, runOpts{
		stdin: "cowl_pat_badtoken\n",
		env:   map[string]string{"COWL_CONFIG": cfgPath, "COWL_PAT": "", "CONTEXTOWL_PAT": ""},
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1; %s", code, errOut)
	}
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Error("config must not be written for an invalid token")
	}
}

func TestAuthLoginRejectsNonPATToken(t *testing.T) {
	f := &fakeAPI{body: `[]`}
	_, errOut, code := run(t, f, []string{"auth", "login", "--with-token"}, runOpts{
		stdin: "sk-something-else\n",
		env:   map[string]string{"COWL_PAT": "", "CONTEXTOWL_PAT": ""},
	})
	if code != 1 || !strings.Contains(errOut, "cowl_pat_") {
		t.Errorf("exit=%d stderr=%q", code, errOut)
	}
	if len(f.reqs) != 0 {
		t.Error("no probe should be sent for a malformed token")
	}
}

func TestAuthStatus(t *testing.T) {
	f := &fakeAPI{body: `[{"id":"ws_1"},{"id":"ws_2"}]`}
	out, _, code := run(t, f, []string{"auth", "status"}, runOpts{})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"cowl_pat_test0t…", "from env", "2 workspaces visible"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "test0token") {
		t.Errorf("status leaked the token:\n%s", out)
	}
}

func TestAuthStatusUnauthenticated(t *testing.T) {
	f := &fakeAPI{body: `[]`}
	_, errOut, code := run(t, f, []string{"auth", "status"}, runOpts{
		env: map[string]string{"COWL_PAT": "", "CONTEXTOWL_PAT": ""},
	})
	if code != 1 || !strings.Contains(errOut, "not authenticated") {
		t.Errorf("exit=%d stderr=%q", code, errOut)
	}
}

func TestAuthLogout(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if err := saveConfig(cfgPath, Config{Token: "cowl_pat_old", BaseURL: "https://x"}); err != nil {
		t.Fatal(err)
	}
	f := &fakeAPI{body: `[]`}
	out, _, code := run(t, f, []string{"auth", "logout"}, runOpts{
		env: map[string]string{"COWL_CONFIG": cfgPath, "COWL_PAT": "", "CONTEXTOWL_PAT": ""},
	})
	if code != 0 || !strings.Contains(out, "logged out") {
		t.Fatalf("exit=%d out=%q", code, out)
	}
	cfg, _ := loadConfig(cfgPath)
	if cfg.Token != "" {
		t.Error("token not cleared")
	}
	if cfg.BaseURL != "https://x" {
		t.Error("logout must keep other config fields")
	}
}

func TestConfirmPromptTTY(t *testing.T) {
	f := &fakeAPI{body: `{"deleted":7}`}
	// Answering "n" aborts without a request.
	_, errOut, code := run(t, f, []string{"changelog", "delete", "7"}, runOpts{stdin: "n\n", tty: true})
	if code != 1 || !strings.Contains(errOut, "aborted") {
		t.Errorf("exit=%d stderr=%q", code, errOut)
	}
	if len(f.reqs) != 0 {
		t.Error("aborted delete must not send a request")
	}
	// Answering "y" proceeds.
	_, _, code = run(t, f, []string{"changelog", "delete", "7"}, runOpts{stdin: "y\n", tty: true})
	if code != 0 || len(f.reqs) != 1 {
		t.Errorf("confirmed delete should send the request: exit=%d reqs=%d", code, len(f.reqs))
	}
}

func TestEncryptedArticleError(t *testing.T) {
	f := &fakeAPI{status: 422, body: `{"error":{"code":"encrypted","message":"this article is end-to-end encrypted; its content is not available over the API","status":422}}`}
	_, errOut, code := run(t, f, []string{"articles", "get", "secret"}, runOpts{})
	if code != 1 || !strings.Contains(errOut, "encrypted") {
		t.Errorf("exit=%d stderr=%q", code, errOut)
	}
}

func TestVersionAndCompletion(t *testing.T) {
	f := &fakeAPI{}
	out, _, code := run(t, f, []string{"version"}, runOpts{})
	if code != 0 || !strings.Contains(out, "cowl") {
		t.Errorf("version: exit=%d out=%q", code, out)
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		out, _, code = run(t, f, []string{"completion", shell}, runOpts{})
		if code != 0 || !strings.Contains(out, "cowl") || !strings.Contains(out, "articles") {
			t.Errorf("completion %s: exit=%d", shell, code)
		}
	}
	if len(f.reqs) != 0 {
		t.Error("local commands must not call the API")
	}
}

func TestBaseURLTrailingSlashTrimmed(t *testing.T) {
	f := &fakeAPI{body: `[]`}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	var out, errb bytes.Buffer
	code := Main([]string{"workspaces", "list", "--base-url", srv.URL + "/"}, IO{
		In: strings.NewReader(""), Out: &out, Err: &errb,
		Env: func(k string) string {
			if k == "COWL_PAT" {
				return "cowl_pat_x"
			}
			if k == "COWL_CONFIG" {
				return filepath.Join(t.TempDir(), "c.json")
			}
			return ""
		},
	})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if req := lastReq(t, f); req.Path != "/api/v1/workspaces" {
		t.Errorf("path = %q (double slash?)", req.Path)
	}
}

func TestSnippetText(t *testing.T) {
	got := snippetText("match  and\n\nmore")
	if got != "match and more" {
		t.Errorf("snippetText = %q", got)
	}
	long := strings.Repeat("é", 100)
	if got := snippetText(long); len([]rune(got)) != 81 { // 80 + ellipsis
		t.Errorf("truncation wrong: %d runes", len([]rune(got)))
	}
}

func TestDoubleDashTerminator(t *testing.T) {
	f := &fakeAPI{body: `[{"type":"article","slug":"a","title":"A","snippet":""}]`}
	_, errOut, code := run(t, f, []string{"search", "--", "foo", "--semantic"}, runOpts{})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	req := lastReq(t, f)
	if req.Query != "q=foo+--semantic" {
		t.Errorf("query = %q, want the flags-like words treated as the query", req.Query)
	}

	f2 := &fakeAPI{body: `{"key":"k","label":"-Label"}`}
	_, errOut, code = run(t, f2, []string{"sections", "create", "--", "-Label"}, runOpts{})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := decodeBody(t, lastReq(t, f2).Body)["label"]; got != "-Label" {
		t.Errorf("label = %v", got)
	}
}

func TestLocalCommandsSurviveBrokenConfig(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"version"}, {"completion", "bash"}} {
		var out, errb bytes.Buffer
		code := Main(args, IO{
			In: strings.NewReader(""), Out: &out, Err: &errb,
			Env: func(k string) string {
				if k == "COWL_CONFIG" {
					return bad
				}
				return ""
			},
		})
		if code != 0 {
			t.Errorf("%v with corrupt config: exit %d, stderr %s", args, code, errb.String())
		}
	}
	// Non-local commands still surface the config error.
	var out, errb bytes.Buffer
	code := Main([]string{"articles", "list"}, IO{
		In: strings.NewReader(""), Out: &out, Err: &errb,
		Env: func(k string) string {
			if k == "COWL_CONFIG" {
				return bad
			}
			return ""
		},
	})
	if code != 1 || !strings.Contains(errb.String(), "parse config") {
		t.Errorf("corrupt config not surfaced: exit %d, stderr %s", code, errb.String())
	}
}

func TestAuthStatusPlanGateDetail(t *testing.T) {
	f := &fakeAPI{status: 402, body: `{"error":{"code":"upgrade_required","message":"your plan does not include this endpoint","status":402}}`}
	out, _, code := run(t, f, []string{"auth", "status"}, runOpts{})
	if code != 0 {
		t.Fatalf("plan-gated key is still valid; exit %d", code)
	}
	if !strings.Contains(out, "needs a paid plan") {
		t.Errorf("status missing plan detail:\n%s", out)
	}
}

func TestUpdateClearsMarkdownWithExplicitEmpty(t *testing.T) {
	f := &fakeAPI{body: `{"slug":"intro"}`}
	_, errOut, code := run(t, f, []string{"articles", "update", "intro", "--markdown", ""}, runOpts{})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	body := decodeBody(t, lastReq(t, f).Body)
	if v, ok := body["markdown"]; !ok || v != "" {
		t.Errorf("explicit empty --markdown must PATCH markdown:\"\", body = %v", body)
	}
}

func TestSearchLimitAppliesToFullText(t *testing.T) {
	f := &fakeAPI{body: `[{"slug":"a","title":"A"},{"slug":"b","title":"B"},{"slug":"c","title":"C"}]`}
	out, _, code := run(t, f, []string{"search", "x", "--limit", "2"}, runOpts{})
	if code != 0 {
		t.Fatal("exit != 0")
	}
	if strings.Contains(out, "c") && strings.Count(out, "\n") > 3 {
		t.Errorf("limit 2 not applied to full-text results:\n%s", out)
	}
	if !strings.Contains(out, "a") || !strings.Contains(out, "b") {
		t.Errorf("first two rows missing:\n%s", out)
	}
}

func TestGroupHelpFlag(t *testing.T) {
	f := &fakeAPI{}
	out, _, code := run(t, f, []string{"articles", "--help"}, runOpts{})
	if code != 0 {
		t.Fatalf("cowl articles --help should print help, exit %d", code)
	}
	if !strings.Contains(out, "cowl articles create") {
		t.Errorf("group help missing usage lines:\n%s", out)
	}
	if len(f.reqs) != 0 {
		t.Error("help must not call the API")
	}
}

func TestRetryDelayForms(t *testing.T) {
	if d := retryDelay("2"); d != 2*time.Second {
		t.Errorf("seconds form = %v", d)
	}
	if d := retryDelay("60"); d != 5*time.Second {
		t.Errorf("cap = %v", d)
	}
	future := time.Now().Add(3 * time.Second).UTC().Format(http.TimeFormat)
	if d := retryDelay(future); d < time.Second || d > 5*time.Second {
		t.Errorf("http-date form = %v", d)
	}
	if d := retryDelay("garbage"); d != time.Second {
		t.Errorf("fallback = %v", d)
	}
}
