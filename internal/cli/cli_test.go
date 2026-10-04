package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "cowl_pat_test0token"

type captured struct {
	Method      string
	Path        string
	Query       string
	Body        string
	Auth        string
	ContentType string
	UserAgent   string
}

type fakeResp struct {
	status int
	body   string
}

// fakeAPI stands in for the ContextOwl REST API as CONTRACT.md describes it.
// routes answers "METHOD /path"; fn answers anything when set; every other
// request gets status and body.
type fakeAPI struct {
	mu     sync.Mutex
	status int
	body   string
	routes map[string]fakeResp
	fn     func(r *http.Request) (int, string)
	reqs   []captured
}

func (f *fakeAPI) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.reqs = append(f.reqs, captured{
			Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Body: string(body),
			Auth: r.Header.Get("Authorization"), ContentType: r.Header.Get("Content-Type"), UserAgent: r.Header.Get("User-Agent"),
		})
		status, out := f.status, f.body
		if resp, ok := f.routes[r.Method+" "+r.URL.Path]; ok {
			status, out = resp.status, resp.body
		}
		fn := f.fn
		f.mu.Unlock()
		if fn != nil {
			status, out = fn(r)
		}
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, out)
	})
}

func (f *fakeAPI) requests() []captured {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]captured(nil), f.reqs...)
}

type runOpts struct {
	stdin  string
	env    map[string]string
	tty    bool // stdin is a terminal
	term   bool // stdout and stderr are terminals
	outTTY bool
	errTTY bool
}

func serve(t *testing.T, f *fakeAPI) string {
	t.Helper()
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return srv.URL
}

// run executes Main hermetically against the fake API and a temp config.
// An env value of "" unsets that variable.
func run(t *testing.T, f *fakeAPI, args []string, opts runOpts) (stdout, stderr string, code int) {
	t.Helper()
	return runAt(t, serve(t, f), args, opts)
}

func runAt(t *testing.T, baseURL string, args []string, opts runOpts) (stdout, stderr string, code int) {
	t.Helper()
	env := map[string]string{
		"CONTEXTOWL_CONFIG":   filepath.Join(t.TempDir(), "config.json"),
		"CONTEXTOWL_BASE_URL": baseURL,
		"CONTEXTOWL_PAT":      testToken,
	}
	for k, v := range opts.env {
		env[k] = v
	}
	var out, errb bytes.Buffer
	code = Main(args, IO{
		In:     strings.NewReader(opts.stdin),
		Out:    &out,
		Err:    &errb,
		Env:    func(k string) string { return env[k] },
		TTY:    opts.tty,
		OutTTY: opts.term || opts.outTTY,
		ErrTTY: opts.term || opts.errTTY,
	})
	return out.String(), errb.String(), code
}

func lastReq(t *testing.T, f *fakeAPI) captured {
	t.Helper()
	reqs := f.requests()
	if len(reqs) == 0 {
		t.Fatal("no request reached the API")
	}
	return reqs[len(reqs)-1]
}

func decodeBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("request body is not a JSON object: %q: %v", body, err)
	}
	return m
}

func compactJSON(t *testing.T, s string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		t.Fatalf("not JSON: %q: %v", s, err)
	}
	return buf.String()
}

func uploadedFile(t *testing.T, req captured) (string, []byte) {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(req.ContentType)
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("content type = %q, want multipart/form-data (%v)", req.ContentType, err)
	}
	mr := multipart.NewReader(strings.NewReader(req.Body), params["boundary"])
	part, err := mr.NextPart()
	if err != nil {
		t.Fatalf("read multipart file: %v", err)
	}
	if part.FormName() != "file" {
		t.Fatalf("form field = %q, want file", part.FormName())
	}
	content, err := io.ReadAll(part)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	return part.FileName(), content
}

const (
	articleJSON = `{"slug":"api-keys","title":"Agent Keys","section":"Reference","kicker":"","status":"STABLE","author":"core",` +
		`"markdown":"# Agent Keys\n\nKeys start with cowl_pat_.\n\n## Rotation\n\nCreate a replacement key.\n","nav":"reference","navLabel":"Agent Keys",` +
		`"visibility":"public","locked":false,"encrypted":false,"archived":false,"updatedAt":"2026-09-30T14:02:11Z","createdAt":"2026-01-01T00:00:00Z",` +
		`"revision":"8f3a2c1b9d0e","url":"https://developer.contextowl.co/docs/platform/api-keys",` +
		`"outline":[{"level":2,"text":"Create a key","anchor":"create-a-key"},{"level":2,"text":"Rotation","anchor":"rotation"}]}`
	sectionJSON = `{"slug":"api-keys","title":"Agent Keys","section":"Reference","status":"STABLE","visibility":"public","nav":"reference",` +
		`"markdown":"## Rotation\n\nCreate a replacement key.\n","updatedAt":"2026-09-30T14:02:11Z","revision":"8f3a2c1b9d0e",` +
		`"url":"https://developer.contextowl.co/docs/platform/api-keys","sectionAnchor":"rotation",` +
		`"outline":[{"level":2,"text":"Create a key","anchor":"create-a-key"},{"level":2,"text":"Rotation","anchor":"rotation"}]}`
	changelogJSON = `{"id":42,"title":"Release 2.4","markdown":"Keys now expire after 90 days.","tags":["new","fixed"],"status":"published",` +
		`"publishedAt":"2026-09-20T10:00:00Z","author":"core","updatedAt":"2026-09-21T08:00:00Z","createdAt":"2026-09-19T08:00:00Z",` +
		`"url":"https://developer.contextowl.co/docs/platform/changelog#e42"}`
	meJSON = `{"key":{"name":"Support agent","prefix":"cowl_pat_test0t","expiresAt":"2026-11-03T09:12:00Z"},` +
		`"org":{"id":"developers","name":"ContextOwl Developers","plan":"free"},"role":"viewer","boundWorkspace":"platform",` +
		`"workspaces":[{"id":"platform","name":"Developer Docs","accessMode":"public"}],` +
		`"permissions":["article.propose","article.read","search"],"blocked":[{"permission":"workspace.create","reason":"plan"}]}`
	editsJSON = `[{"old":"30 days","new":"90 days"}]`
)

func TestCommands(t *testing.T) {
	articleMD := "# Hello\n\ncontent"
	tests := []struct {
		name       string
		args       []string
		stdin      string
		status     int
		response   string
		wantMethod string
		wantPath   string
		wantQuery  string
		wantBody   map[string]any // nil = no body expected
		wantTerm   []string       // stdout on a terminal
		content    bool           // a content command prints the same text in a pipe
	}{
		{
			name: "workspaces list", args: []string{"workspaces", "list"},
			response:   `[{"id":"ws_1","name":"Docs","color":"#112233","accessMode":"public","listed":true,"llmsTxt":true}]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces",
			wantTerm: []string{"ID", "LISTED", "ws_1", "Docs", "public", "yes"},
		},
		{
			name: "workspaces create", args: []string{"workspaces", "create", "New Docs", "--color", "#aabbcc", "--access-mode", "internal", "--listed"},
			status: 201, response: `{"id":"ws_2","name":"New Docs","color":"#aabbcc","accessMode":"internal","listed":true,"llmsTxt":true}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces",
			wantBody: map[string]any{"name": "New Docs", "color": "#aabbcc", "access_mode": "internal", "listed": true},
			wantTerm: []string{`created workspace ws_2 ("New Docs")`},
		},
		{
			name: "workspaces update explicit false", args: []string{"workspaces", "update", "ws_2", "--llms-txt=false", "--listed=false"},
			response:   `{"id":"ws_2","name":"New Docs","listed":false,"llmsTxt":false}`,
			wantMethod: "PATCH", wantPath: "/api/v1/workspaces/ws_2",
			wantBody: map[string]any{"llms_txt": false, "listed": false},
			wantTerm: []string{"updated workspace ws_2"},
		},
		{
			name: "workspaces delete", args: []string{"workspaces", "delete", "ws_2", "--yes"},
			response:   `{"deleted":"ws_2"}`,
			wantMethod: "DELETE", wantPath: "/api/v1/workspaces/ws_2",
			wantTerm: []string{"deleted workspace ws_2"},
		},
		{
			name: "articles list", args: []string{"articles", "list", "-w", "ws_1"},
			response: `[{"slug":"intro","title":"Intro","section":"Guides","nav":"guides","status":"STABLE","encrypted":false,"visibility":"public",` +
				`"updatedAt":"2026-09-30T14:02:11Z","url":"https://docs.example.com/docs/ws_1/intro"}]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/ws_1/articles",
			wantTerm: []string{"SLUG", "UPDATED", "URL", "intro", "STABLE", "2026-09-30 14:02", "https://docs.example.com/docs/ws_1/intro"},
		},
		{
			name: "articles list filters", args: []string{"articles", "list", "--status", "draft, in review", "--nav", "none", "--updated-since", "7d", "--published-only"},
			response:   `[]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/articles",
			wantQuery: "nav=none&published_only=true&status=DRAFT%2CIN+REVIEW&updated_since=7d",
			wantTerm:  []string{"SLUG", "UPDATED"},
		},
		{
			name: "articles get prints front matter", args: []string{"articles", "get", "api-keys"},
			response:   articleJSON,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/articles/api-keys",
			wantTerm: []string{"---\nslug: api-keys\n", `title: "Agent Keys"`, "revision: 8f3a2c1b9d0e", `anchors: ["create-a-key", "rotation"]`, "---\n# Agent Keys\n"},
			content:  true,
		},
		{
			name: "articles get one section", args: []string{"articles", "get", "api-keys", "--section", "rotation"},
			response:   sectionJSON,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/articles/api-keys", wantQuery: "section=rotation",
			wantTerm: []string{"section_anchor: rotation", "---\n## Rotation\n"},
			content:  true,
		},
		{
			name: "articles create", args: []string{"articles", "create", "--title", "Guide", "--slug", "guide", "--section-key", "guides", "--file", "-"},
			stdin:  articleMD,
			status: 201, response: `{"slug":"guide","status":"DRAFT","nav":"guides","url":"https://docs.example.com/docs/ws/guide","revision":"0a1b2c3d4e5f"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/articles",
			wantBody: map[string]any{"title": "Guide", "slug": "guide", "section_key": "guides", "markdown": articleMD},
			wantTerm: []string{"created article guide (DRAFT, section guides, revision 0a1b2c3d4e5f) https://docs.example.com/docs/ws/guide"},
		},
		{
			name: "articles update status places the article", args: []string{"articles", "update", "intro", "--status", "STABLE"},
			response: `{"slug":"intro","status":"STABLE","nav":"guides","updatedAt":"2026-10-01T10:00:00Z","url":"https://docs.example.com/docs/ws/intro",` +
				`"revision":"111111111111","previousRevision":"111111111111","changed":["status"],"placed":true}`,
			wantMethod: "PATCH", wantPath: "/api/v1/workspaces/-/articles/intro",
			wantBody: map[string]any{"status": "STABLE"},
			wantTerm: []string{"updated article intro (changed status, placed in section guides) revision 111111111111 https://docs.example.com/docs/ws/intro"},
		},
		{
			name: "articles update with edits and base revision", args: []string{"articles", "update", "intro", "--edits", "-", "--base-revision", "8F3A2C1B9D0E"},
			stdin: editsJSON,
			response: `{"slug":"intro","status":"STABLE","nav":"guides","updatedAt":"2026-10-01T10:00:00Z","url":"https://docs.example.com/docs/ws/intro",` +
				`"revision":"222222222222","previousRevision":"8f3a2c1b9d0e","changed":["markdown"]}`,
			wantMethod: "PATCH", wantPath: "/api/v1/workspaces/-/articles/intro",
			wantBody: map[string]any{"edits": []any{map[string]any{"old": "30 days", "new": "90 days"}}, "base_revision": "8f3a2c1b9d0e"},
			wantTerm: []string{"updated article intro (changed markdown) revision 222222222222"},
		},
		{
			name: "articles update full body with allow shrink", args: []string{"articles", "update", "intro", "--markdown", "short", "--allow-shrink"},
			response:   `{"slug":"intro","status":"STABLE","revision":"333333333333","previousRevision":"222222222222","changed":["markdown"]}`,
			wantMethod: "PATCH", wantPath: "/api/v1/workspaces/-/articles/intro",
			wantBody: map[string]any{"markdown": "short", "allow_shrink": true},
			wantTerm: []string{"updated article intro (changed markdown) revision 333333333333"},
		},
		{
			name: "articles update visibility unchanged", args: []string{"articles", "update", "intro", "--visibility", "internal"},
			response:   `{"slug":"intro","status":"STABLE","revision":"333333333333","previousRevision":"333333333333","changed":[]}`,
			wantMethod: "PATCH", wantPath: "/api/v1/workspaces/-/articles/intro",
			wantBody: map[string]any{"visibility": "internal"},
			wantTerm: []string{"article intro is unchanged (revision 333333333333)"},
		},
		{
			name: "articles place with position", args: []string{"articles", "place", "intro", "--section", "guides", "--position", "2"},
			response:   `{"slug":"intro","section":"guides"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/articles/intro/placement",
			wantBody: map[string]any{"section": "guides", "position": float64(2)},
			wantTerm: []string{"placed article intro in guides at position 2"},
		},
		{
			name: "sections list", args: []string{"sections", "list"},
			response:   `[{"key":"guides","label":"Guides","visibility":"public","articleCount":4},{"key":"ops","label":"Operations","visibility":"internal","articleCount":0}]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/sections",
			wantTerm: []string{"KEY", "ARTICLES", "guides", "Guides", "4", "internal"},
		},
		{
			name: "sections create new", args: []string{"sections", "create", "Getting Started"},
			status: 201, response: `{"key":"getting-started","label":"Getting Started","created":true}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/sections",
			wantBody: map[string]any{"label": "Getting Started"},
			wantTerm: []string{`created section getting-started ("Getting Started")`},
		},
		{
			name: "sections create existing", args: []string{"sections", "create", "getting started"},
			status: 200, response: `{"key":"getting-started","label":"Getting Started","created":false}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/sections",
			wantBody: map[string]any{"label": "getting started"},
			wantTerm: []string{`section getting-started ("Getting Started") already exists`},
		},
		{
			name: "proposals list", args: []string{"proposals", "list", "--status", "approved"},
			response: `[{"id":4,"objectType":"article","status":"approved","slug":"api-keys","title":"Agent Keys","note":"typo","reviewNote":"",` +
				`"author":"bot","createdAt":"2026-07-19T10:00:00Z","reviewedAt":"2026-07-20T10:00:00Z","stale":false}]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/proposals", wantQuery: "status=approved",
			wantTerm: []string{"ID", "SLUG", "STALE", "api-keys", "approved", "typo"},
		},
		{
			name: "proposals get", args: []string{"proposals", "get", "12"},
			response: `[{"id":12,"objectType":"article","status":"pending","slug":"api-keys","title":"Agent Keys","note":"Fix the rotation steps.",` +
				`"reviewNote":"","author":"bot","createdAt":"2026-10-01T10:00:00Z","reviewedAt":null,"stale":true,` +
				`"markdown":"# Agent Keys\n\nNew text.\n","baseRevision":"8f3a2c1b9d0e","currentRevision":"999999999999"}]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/proposals", wantQuery: "id=12&status=all",
			wantTerm: []string{"base revision:", "8f3a2c1b9d0e", "999999999999", "stale:", "yes", "Fix the rotation steps.", "\n# Agent Keys\n\nNew text.\n"},
		},
		{
			name: "proposals create with edits", args: []string{"proposals", "create", "--slug", "api-keys", "--edits", "-", "--base-revision", "8f3a2c1b9d0e", "--note", "typo fix"},
			stdin:  editsJSON,
			status: 201, response: `{"id":9,"status":"pending","slug":"api-keys","reviewUrl":"https://contextowl.co/admin/proposals?id=9","baseRevision":"8f3a2c1b9d0e"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/proposals",
			wantBody: map[string]any{"edits": []any{map[string]any{"old": "30 days", "new": "90 days"}}, "base_revision": "8f3a2c1b9d0e", "slug": "api-keys", "note": "typo fix"},
			wantTerm: []string{"created proposal 9 (pending) for api-keys https://contextowl.co/admin/proposals?id=9"},
		},
		{
			name: "proposals create new article", args: []string{"proposals", "create", "--title", "Rotate keys", "--markdown", "# Rotate", "--section-key", "reference", "--note", "new page", "--allow-shrink"},
			status: 201, response: `{"id":10,"status":"pending","slug":"","reviewUrl":"https://contextowl.co/admin/proposals?id=10","baseRevision":""}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/proposals",
			wantBody: map[string]any{"markdown": "# Rotate", "allow_shrink": true, "title": "Rotate keys", "note": "new page", "section_key": "reference"},
			wantTerm: []string{"created proposal 10 (pending) https://contextowl.co/admin/proposals?id=10"},
		},
		{
			name: "changelog list paged", args: []string{"changelog", "list", "--limit", "20", "--offset", "40", "--since", "30d"},
			response:   "[" + changelogJSON + "]",
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/changelog", wantQuery: "limit=20&offset=40&since=30d",
			wantTerm: []string{"ID", "URL", "42", "Release 2.4", "new,fixed", "2026-09-20 10:00", "changelog#e42"},
		},
		{
			name: "changelog list drafts", args: []string{"changelog", "list", "--drafts"},
			response:   `[{"id":1,"title":"v2","tags":["new"],"status":"draft","publishedAt":null,"url":"https://docs.example.com/docs/ws/changelog#e1"}]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/changelog", wantQuery: "drafts=true",
			wantTerm: []string{"v2", "draft", "new"},
		},
		{
			name: "changelog get", args: []string{"changelog", "get", "42"},
			response:   changelogJSON,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/changelog/42",
			wantTerm: []string{"---\nid: 42\n", `title: "Release 2.4"`, `tags: ["new", "fixed"]`, "published_at: 2026-09-20T10:00:00Z", "url: https://developer.contextowl.co/docs/platform/changelog#e42", "---\nKeys now expire after 90 days.\n"},
			content:  true,
		},
		{
			name: "changelog create sends tags as given", args: []string{"changelog", "create", "--title", "v2 ships", "--markdown", "notes", "--tags", "added, fix", "--status", "published"},
			status: 201, response: `{"id":7,"title":"v2 ships","tags":["new","fixed"],"status":"published","publishedAt":"2026-10-01T10:00:00Z","url":"https://docs.example.com/docs/ws/changelog#e7"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/changelog",
			wantBody: map[string]any{"title": "v2 ships", "markdown": "notes", "tags": []any{"added", "fix"}, "status": "published"},
			wantTerm: []string{"created changelog entry 7 (published) https://docs.example.com/docs/ws/changelog#e7"},
		},
		{
			name: "changelog update", args: []string{"changelog", "update", "7", "--title", "v2.0.1"},
			response:   `{"id":7,"title":"v2.0.1","status":"published","url":"https://docs.example.com/docs/ws/changelog#e7"}`,
			wantMethod: "PATCH", wantPath: "/api/v1/workspaces/-/changelog/7",
			wantBody: map[string]any{"title": "v2.0.1"},
			wantTerm: []string{"updated changelog entry 7 (published) https://docs.example.com/docs/ws/changelog#e7"},
		},
		{
			name: "changelog delete", args: []string{"changelog", "delete", "7", "--yes"},
			response:   `{"deleted":7}`,
			wantMethod: "DELETE", wantPath: "/api/v1/workspaces/-/changelog/7",
			wantTerm: []string{"deleted changelog entry 7"},
		},
		{
			name: "landing get", args: []string{"landing", "get"},
			response:   `{"headline":"Docs"}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/landing",
			wantTerm: []string{`"headline": "Docs"`},
		},
		{
			name: "landing autofill", args: []string{"landing", "autofill"},
			response:   `{"headline":"Draft"}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/landing/autofill",
			wantTerm: []string{`"headline": "Draft"`},
		},
		{
			name: "landing set from stdin", args: []string{"landing", "set", "--file", "-"},
			stdin:      `{"headline":"New"}`,
			response:   `{"headline":"New"}`,
			wantMethod: "PUT", wantPath: "/api/v1/workspaces/-/landing",
			wantBody: map[string]any{"headline": "New"},
			wantTerm: []string{"landing updated"},
		},
		{
			name: "landing propose wraps body", args: []string{"landing", "propose", "--file", "-", "--note", "please"},
			stdin:  `{"headline":"New"}`,
			status: 201, response: `{"id":3,"status":"pending"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/landing/proposals",
			wantBody: map[string]any{"landing": map[string]any{"headline": "New"}, "note": "please"},
			wantTerm: []string{"created landing proposal 3 (pending)"},
		},
		{
			name: "openapi status", args: []string{"openapi", "status"},
			response:   `{"configured":true,"specTitle":"API","specFormat":"yaml","specBytes":42}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/openapi",
			wantTerm: []string{`"configured": true`, `"specFormat": "yaml"`},
		},
		{
			name: "openapi spec prints the stored bytes", args: []string{"openapi", "spec"},
			response:   "openapi: 3.1.0\ninfo:\n  title: API\n",
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/openapi/spec",
			wantTerm: []string{"openapi: 3.1.0\ninfo:\n  title: API\n"},
			content:  true,
		},
		{
			name: "openapi attach by url", args: []string{"openapi", "attach", "--url", "https://example.com/spec.json"},
			response:   `{"configured":true,"unchanged":true}`,
			wantMethod: "PUT", wantPath: "/api/v1/workspaces/-/openapi",
			wantBody: map[string]any{"url": "https://example.com/spec.json"},
			wantTerm: []string{`"unchanged": true`},
		},
		{
			name: "openapi sync", args: []string{"openapi", "sync"},
			response:   `{"configured":true}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/openapi/sync",
			wantTerm: []string{`"configured": true`},
		},
		{
			name: "openapi detach", args: []string{"openapi", "detach", "--yes"},
			response:   `{"detached":true,"removed":4}`,
			wantMethod: "DELETE", wantPath: "/api/v1/workspaces/-/openapi",
			wantTerm: []string{"deleted 4 generated pages"},
		},
		{
			name: "openapi pages", args: []string{"openapi", "pages"},
			response:   `{"sections":[{"key":"api","label":"API"}],"pages":[{"slug":"get-users","title":"List users","method":"GET","section":"API","nav":"api"}]}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/openapi/pages",
			wantTerm: []string{"SECTION", "api", "get-users", "GET"},
		},
		{
			name: "openapi create-section", args: []string{"openapi", "create-section", "Payments"},
			status: 201, response: `{"key":"api-payments","label":"Payments","created":true}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/openapi/sections",
			wantBody: map[string]any{"label": "Payments"},
			wantTerm: []string{"created section api-payments"},
		},
		{
			name: "openapi place with position", args: []string{"openapi", "place", "get-users", "--section", "api", "--position", "0"},
			response:   `{"slug":"get-users"}`,
			wantMethod: "POST", wantPath: "/api/v1/workspaces/-/openapi/pages/get-users/placement",
			wantBody: map[string]any{"section": "api", "position": float64(0)},
			wantTerm: []string{"placed page get-users in api at position 0"},
		},
		{
			name: "openapi detach-page", args: []string{"openapi", "detach-page", "get-users"},
			response:   `{"slug":"get-users"}`,
			wantMethod: "DELETE", wantPath: "/api/v1/workspaces/-/openapi/pages/get-users",
			wantTerm: []string{"detached page get-users", "later syncs skip it"},
		},
		{
			name: "search full-text", args: []string{"search", "sso", "setup"},
			response: `{"semantic":false,"results":[` +
				`{"type":"article","slug":"sso","title":"SSO","section":"Guides","sectionKey":"guides","status":"STABLE","updatedAt":"2026-09-30T14:02:11Z","url":"https://docs.example.com/docs/ws/sso","snippet":"SSO setup"},` +
				`{"type":"changelog","id":42,"title":"Release 2.4","status":"published","publishedAt":"2026-09-20T10:00:00Z","url":"https://docs.example.com/docs/ws/changelog#e42","snippet":"SSO now"}]}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/search", wantQuery: "limit=10&q=sso+setup",
			wantTerm: []string{"SLUG/ID", "STATUS", "URL", "sso", "STABLE", "SSO setup", "https://docs.example.com/docs/ws/sso", "42", "changelog"},
		},
		{
			name: "search semantic published only", args: []string{"search", "keys expire", "--semantic", "--limit", "5", "--published-only"},
			response:   `{"semantic":true,"results":[{"type":"article","slug":"api-keys","title":"Agent Keys","status":"STABLE","url":"https://docs.example.com/docs/ws/api-keys","snippet":"expire","score":0.912}]}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/search", wantQuery: "limit=5&published_only=true&q=keys+expire&semantic=true",
			wantTerm: []string{"SCORE", "0.912", "api-keys"},
		},
		{
			name: "search without results prints suggestions", args: []string{"search", "api kyes"},
			response:   `{"semantic":false,"results":[],"suggestions":[{"slug":"api-keys","title":"Agent Keys","url":"https://docs.example.com/docs/ws/api-keys"}]}`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces/-/search", wantQuery: "limit=10&q=api+kyes",
			wantTerm: []string{`no results for "api kyes"`, "close titles", "api-keys", "Agent Keys"},
		},
		{
			name: "whoami", args: []string{"whoami"},
			response:   meJSON,
			wantMethod: "GET", wantPath: "/api/v1/me",
			wantTerm: []string{"cowl_pat_test0t…", "Support agent", "role:", "viewer", "platform (bound key)", "workspace.create (plan)", "WORKSPACE", "Developer Docs"},
		},
		{
			name: "api escape hatch", args: []string{"api", "get", "workspaces", "--query", "a=1", "--query", "b=2"},
			response:   `[]`,
			wantMethod: "GET", wantPath: "/api/v1/workspaces", wantQuery: "a=1&b=2",
			wantTerm: []string{"[]"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeAPI{status: tt.status, body: tt.response}
			termOut, errOut, code := run(t, f, tt.args, runOpts{stdin: tt.stdin, term: true})
			if code != 0 {
				t.Fatalf("terminal: exit %d, stderr: %s", code, errOut)
			}
			req := lastReq(t, f)
			if req.Method != tt.wantMethod || req.Path != tt.wantPath {
				t.Errorf("request = %s %s, want %s %s", req.Method, req.Path, tt.wantMethod, tt.wantPath)
			}
			if req.Query != tt.wantQuery {
				t.Errorf("query = %q, want %q", req.Query, tt.wantQuery)
			}
			if req.Auth != "Bearer "+testToken {
				t.Errorf("auth header = %q", req.Auth)
			}
			if !strings.HasPrefix(req.UserAgent, "cowl/") {
				t.Errorf("user agent = %q", req.UserAgent)
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
			for _, want := range tt.wantTerm {
				if !strings.Contains(termOut, want) {
					t.Errorf("terminal stdout missing %q\n---\n%s", want, termOut)
				}
			}

			pipeOut, errOut, code := run(t, f, tt.args, runOpts{stdin: tt.stdin})
			if code != 0 {
				t.Fatalf("pipe: exit %d, stderr: %s", code, errOut)
			}
			if tt.content {
				if pipeOut != termOut {
					t.Errorf("content command must print the same text in a pipe\npipe:\n%s\nterminal:\n%s", pipeOut, termOut)
				}
				return
			}
			if want := compactJSON(t, tt.response) + "\n"; pipeOut != want {
				t.Errorf("pipe stdout = %q, want the compact response %q", pipeOut, want)
			}
		})
	}
}

func TestUsageErrors(t *testing.T) {
	editsFile := filepath.Join(t.TempDir(), "edits.json")
	if err := os.WriteFile(editsFile, []byte(editsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		args    []string
		wantErr string
		stdin   string
	}{
		{"unknown command", []string{"bogus"}, "unknown command", ""},
		{"missing subcommand", []string{"articles"}, "needs a subcommand", ""},
		{"memory is gone", []string{"memory", "list"}, "unknown command: memory", ""},
		{"token flag is gone", []string{"articles", "list", "--token", "cowl_pat_x"}, "flag provided but not defined: -token", ""},
		{"base-url only on login", []string{"articles", "list", "--base-url", "https://x.example"}, "flag provided but not defined: -base-url", ""},
		{"articles list takes no args", []string{"articles", "list", "platform"}, "unexpected argument: platform", ""},
		{"articles list rejects bad status", []string{"articles", "list", "--status", "DRAFT,PUBLISHED"}, "got PUBLISHED", ""},
		{"articles get needs a slug", []string{"articles", "get"}, "at least one SLUG", ""},
		{"articles get section needs one slug", []string{"articles", "get", "a", "b", "--section", "x"}, "--section works with one SLUG only", ""},
		{"articles create needs title", []string{"articles", "create"}, "--title is required", ""},
		{"articles update needs a field", []string{"articles", "update", "x"}, "nothing to update", ""},
		{"articles update base revision alone", []string{"articles", "update", "x", "--base-revision", "8f3a2c1b9d0e"}, "nothing to update", ""},
		{"articles update rejects invalid visibility", []string{"articles", "update", "x", "--visibility", "team"}, "--visibility must be public, internal, or private", ""},
		{"articles update rejects bad status", []string{"articles", "update", "x", "--status", "published"}, "DRAFT, IN REVIEW, BETA, STABLE, DEPRECATED", ""},
		{"articles update edits and markdown", []string{"articles", "update", "x", "--edits", editsFile, "--markdown", "y"}, "not both", ""},
		{"articles update edits not JSON", []string{"articles", "update", "x", "--edits", "-"}, "--edits must be a JSON array", "nope"},
		{"articles update edits unknown field", []string{"articles", "update", "x", "--edits", "-"}, "unknown field", `[{"old":"a","neww":"b"}]`},
		{"articles update edits empty", []string{"articles", "update", "x", "--edits", "-"}, "1 to 20 edits", `[]`},
		{"articles update edits too many", []string{"articles", "update", "x", "--edits", "-"}, "1 to 20 edits", "[" + strings.Repeat(`{"old":"a","new":"b"},`, 20) + `{"old":"a","new":"b"}]`},
		{"articles update edit without old", []string{"articles", "update", "x", "--edits", "-"}, "edits[0].old must be 1 to 4000", `[{"old":"","new":"b"}]`},
		{"articles update bad revision", []string{"articles", "update", "x", "--markdown", "y", "--base-revision", "xyz"}, "12 to 64 hex", ""},
		{"articles update missing file", []string{"articles", "update", "x", "--file", "/no/such/file.md"}, "cannot read /no/such/file.md", ""},
		{"articles place needs section", []string{"articles", "place", "x"}, "--section is required", ""},
		{"articles place negative position", []string{"articles", "place", "x", "--section", "g", "--position", "-1"}, "--position must be 0 or more", ""},
		{"file and markdown conflict", []string{"articles", "create", "--title", "t", "--file", "a.md", "--markdown", "x"}, "not both", ""},
		{"proposals need a target", []string{"proposals", "create", "--markdown", "x"}, "pass --slug", ""},
		{"proposals need content", []string{"proposals", "create", "--title", "t"}, "content is required", ""},
		{"proposal edits need a slug", []string{"proposals", "create", "--title", "t", "--edits", editsFile}, "--edits needs --slug", ""},
		{"proposal section key is for new articles", []string{"proposals", "create", "--slug", "s", "--markdown", "x", "--section-key", "k"}, "--section-key is for a new article only", ""},
		{"proposals get needs an integer", []string{"proposals", "get", "abc"}, "ID must be a positive integer", ""},
		{"changelog id must be int", []string{"changelog", "update", "abc", "--title", "t"}, "ID must be a positive integer", ""},
		{"changelog get needs one id", []string{"changelog", "get"}, "exactly one ID", ""},
		{"changelog list limit range", []string{"changelog", "list", "--limit", "101"}, "--limit must be 1 to 100", ""},
		{"changelog list offset range", []string{"changelog", "list", "--offset", "-1"}, "--offset must be 0 or more", ""},
		{"openapi attach xor", []string{"openapi", "attach"}, "exactly one of --url or --file", ""},
		{"search needs query", []string{"search"}, "QUERY is required", ""},
		{"search limit range", []string{"search", "x", "--limit", "51"}, "--limit must be 1 to 50", ""},
		{"api needs valid method", []string{"api", "FETCH", "workspaces"}, "METHOD must be one of", ""},
		{"api query needs equals", []string{"api", "GET", "workspaces", "--query", "broken"}, "key=value", ""},
		{"delete refuses without yes non-tty", []string{"workspaces", "delete", "ws_1"}, "pass --yes", ""},
		{"completion needs shell", []string{"completion"}, "bash, zsh, fish", ""},
		{"workspaces create rejects stray args", []string{"workspaces", "create", "My", "Docs"}, "quote multi-word", ""},
		{"uploads image needs file", []string{"uploads", "image"}, "exactly one FILE", ""},
		{"landing set needs valid JSON", []string{"landing", "set", "--file", "-"}, "is not valid JSON", "{"},
		{"whoami takes no args", []string{"whoami", "x"}, "unexpected argument", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeAPI{body: `{}`}
			_, errOut, code := run(t, f, tt.args, runOpts{stdin: tt.stdin, term: true})
			if code != exitUsage {
				t.Fatalf("exit = %d, want 2; stderr: %s", code, errOut)
			}
			if !strings.Contains(errOut, tt.wantErr) {
				t.Errorf("stderr missing %q\n---\n%s", tt.wantErr, errOut)
			}
			if reqs := f.requests(); len(reqs) != 0 {
				t.Errorf("request should not have been sent, got %v", reqs)
			}
		})
	}
}

func TestArticlesGetSeveralSlugs(t *testing.T) {
	second := strings.Replace(articleJSON, `"slug":"api-keys"`, `"slug":"webhooks"`, 1)
	f := &fakeAPI{routes: map[string]fakeResp{
		"GET /api/v1/workspaces/-/articles/api-keys": {body: articleJSON},
		"GET /api/v1/workspaces/-/articles/webhooks": {body: second},
	}}
	out, errOut, code := run(t, f, []string{"articles", "get", "api-keys", "webhooks"}, runOpts{})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	docs := strings.Split(out, "\n\n---\n")
	if len(docs) != 2 || !strings.HasPrefix(out, "---\nslug: api-keys\n") || !strings.HasPrefix(docs[1], "slug: webhooks\n") {
		t.Errorf("want two documents separated by a blank line, got:\n%s", out)
	}

	out, errOut, code = run(t, f, []string{"articles", "get", "api-keys", "webhooks", "--json"}, runOpts{})
	if code != 0 {
		t.Fatalf("--json exit %d: %s", code, errOut)
	}
	var arr []map[string]any
	if err := json.Unmarshal([]byte(out), &arr); err != nil || len(arr) != 2 || arr[1]["slug"] != "webhooks" {
		t.Errorf("--json with several slugs must print an array: %v\n%s", err, out)
	}
	if strings.Count(out, "\n") != 1 {
		t.Errorf("JSON in a pipe must be one line:\n%s", out)
	}
}

func TestArticlesGetStopsAtFirstError(t *testing.T) {
	f := &fakeAPI{
		status: 404, body: `{"error":{"code":"not_found","message":"no article missing","status":404,"details":{"suggestions":[{"slug":"api-keys","title":"Agent Keys"}]}}}`,
		routes: map[string]fakeResp{"GET /api/v1/workspaces/-/articles/api-keys": {body: articleJSON}},
	}
	out, errOut, code := run(t, f, []string{"articles", "get", "api-keys", "missing"}, runOpts{term: true})
	if code != exitNotFound {
		t.Fatalf("exit = %d, want 3; %s", code, errOut)
	}
	if out != "" {
		t.Errorf("a failed read must print no documents, got:\n%s", out)
	}
	if !strings.Contains(errOut, "did you mean: api-keys") {
		t.Errorf("terminal error must show the suggestions:\n%s", errOut)
	}
}

func TestChangelogListAll(t *testing.T) {
	const total = 250
	f := &fakeAPI{fn: func(r *http.Request) (int, string) {
		q := r.URL.Query()
		limit, offset := 50, 0
		if v, err := strconv.Atoi(q.Get("limit")); err == nil {
			limit = v
		}
		if v, err := strconv.Atoi(q.Get("offset")); err == nil {
			offset = v
		}
		if limit < 1 || limit > 100 {
			return 400, `{"error":{"code":"invalid_request","message":"limit must be 1 to 100","status":400}}`
		}
		var items []string
		for i := offset; i < offset+limit && i < total; i++ {
			items = append(items, `{"id":`+itoa(total-i)+`,"title":"Release","tags":[],"status":"published","url":"u"}`)
		}
		return 200, "[" + strings.Join(items, ",") + "]"
	}}
	out, errOut, code := run(t, f, []string{"changelog", "list", "--all", "--since", "2026-01-01"}, runOpts{})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) != total {
		t.Fatalf("want %d entries, got %d (%v)", total, len(rows), err)
	}
	want := []string{
		"limit=100&offset=0&since=2026-01-01",
		"limit=100&offset=100&since=2026-01-01",
		"limit=100&offset=200&since=2026-01-01",
	}
	var paged []string
	for _, r := range f.requests() {
		paged = append(paged, r.Query)
	}
	if strings.Join(paged, " ") != strings.Join(want, " ") {
		t.Errorf("pages = %v, want %v", paged, want)
	}

	out, errOut, code = run(t, f, []string{"changelog", "list", "--all", "--limit", "60"}, runOpts{term: true})
	if code != 0 {
		t.Fatalf("terminal exit %d: %s", code, errOut)
	}
	if rows := strings.Count(out, "\n") - 1; rows != total {
		t.Errorf("terminal table has %d rows, want %d", rows, total)
	}
	if last := lastReq(t, f); last.Query != "limit=60&offset=240" {
		t.Errorf("--limit sets the page size, last page query = %q", last.Query)
	}
}

func TestChangelogListAllStopsWhenOffsetIsIgnored(t *testing.T) {
	var page []string
	for i := 1; i <= 100; i++ {
		page = append(page, `{"id":`+itoa(i)+`}`)
	}
	f := &fakeAPI{body: "[" + strings.Join(page, ",") + "]"}
	_, errOut, code := run(t, f, []string{"changelog", "list", "--all"}, runOpts{})
	if code != exitError || !strings.Contains(errOut, `"code":"invalid_response"`) {
		t.Errorf("exit=%d stderr=%s", code, errOut)
	}
	if n := len(f.requests()); n != 2 {
		t.Errorf("requests = %d, want 2", n)
	}
}

func TestUploadsImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "architecture.png")
	want := []byte("image bytes")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	resp := `{"url":"/uploads/default/prod/image.png","absoluteUrl":"https://docs.example.com/uploads/default/prod/image.png"}`
	f := &fakeAPI{status: http.StatusCreated, body: resp}
	out, errOut, code := run(t, f, []string{"uploads", "image", path, "-w", "prod"}, runOpts{term: true})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	req := lastReq(t, f)
	if req.Method != http.MethodPost || req.Path != "/api/v1/workspaces/prod/uploads" {
		t.Errorf("request = %s %s", req.Method, req.Path)
	}
	if req.Auth != "Bearer "+testToken {
		t.Errorf("auth header = %q", req.Auth)
	}
	name, content := uploadedFile(t, req)
	if name != "architecture.png" || !bytes.Equal(content, want) {
		t.Errorf("uploaded file = %q %q, want architecture.png %q", name, content, want)
	}
	if out != "/uploads/default/prod/image.png\n" {
		t.Errorf("terminal stdout = %q", out)
	}
	out, _, code = run(t, f, []string{"uploads", "image", path, "-w", "prod"}, runOpts{})
	if code != 0 || out != resp+"\n" {
		t.Errorf("pipe: exit=%d stdout=%q", code, out)
	}
}

func TestJSONOutputModes(t *testing.T) {
	resp := `[{"slug":"intro","title":"Intro","section":"G","nav":"g","status":"DRAFT","encrypted":false}]`
	f := &fakeAPI{body: resp}
	out, _, code := run(t, f, []string{"articles", "list", "--json"}, runOpts{term: true})
	if code != 0 {
		t.Fatal("exit != 0")
	}
	if !strings.Contains(out, "\n  {\n") {
		t.Errorf("--json on a terminal prints indented JSON:\n%s", out)
	}
	var parsed []map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil || parsed[0]["slug"] != "intro" {
		t.Errorf("--json output is not the response: %v\n%s", err, out)
	}
	out, _, _ = run(t, f, []string{"articles", "list"}, runOpts{})
	if out != resp+"\n" {
		t.Errorf("a pipe gets one compact line: %q", out)
	}
	out, _, _ = run(t, f, []string{"articles", "list"}, runOpts{outTTY: true})
	if !strings.HasPrefix(out, "SLUG") {
		t.Errorf("a terminal gets a table:\n%s", out)
	}
}

func TestSemanticSearchFallbackNote(t *testing.T) {
	f := &fakeAPI{body: `{"semantic":false,"results":[{"type":"article","slug":"a","title":"A","status":"STABLE","url":"u","snippet":"x"}]}`}
	out, errOut, code := run(t, f, []string{"search", "q", "--semantic"}, runOpts{term: true})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "semantic ranking is not available") {
		t.Errorf("missing fallback note, stderr: %q", errOut)
	}
	if strings.Contains(out, "SCORE") {
		t.Errorf("a full-text fallback has no SCORE column:\n%s", out)
	}
	_, errOut, _ = run(t, f, []string{"search", "q", "--semantic"}, runOpts{})
	if errOut != "" {
		t.Errorf("agents read semantic:false from the JSON; stderr must stay empty: %q", errOut)
	}
}

func TestRateLimitRetries(t *testing.T) {
	old := sleepFn
	var slept time.Duration
	sleepFn = func(d time.Duration) { slept = d }
	defer func() { sleepFn = old }()

	var n atomic.Int32
	f := &fakeAPI{fn: func(r *http.Request) (int, string) {
		if n.Add(1) == 1 {
			return 429, `{"error":{"code":"rate_limited","message":"slow down","status":429}}`
		}
		return 200, `[]`
	}}
	_, errOut, code := run(t, f, []string{"articles", "list"}, runOpts{})
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errOut)
	}
	if got := n.Load(); got != 2 {
		t.Errorf("requests = %d, want 2 (one retry)", got)
	}
	if slept != time.Second {
		t.Errorf("slept %v, want 1s", slept)
	}
}

func TestUpdateClearsMarkdownWithExplicitEmpty(t *testing.T) {
	f := &fakeAPI{body: `{"slug":"intro","changed":["markdown"]}`}
	_, errOut, code := run(t, f, []string{"articles", "update", "intro", "--markdown", ""}, runOpts{})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	body := decodeBody(t, lastReq(t, f).Body)
	if v, ok := body["markdown"]; !ok || v != "" {
		t.Errorf("explicit empty --markdown must PATCH markdown:\"\", body = %v", body)
	}
}

func TestConfirmPromptTTY(t *testing.T) {
	f := &fakeAPI{body: `{"deleted":7}`}
	_, errOut, code := run(t, f, []string{"changelog", "delete", "7"}, runOpts{stdin: "n\n", tty: true})
	if code != exitError || !strings.Contains(errOut, `"code":"aborted"`) {
		t.Errorf("exit=%d stderr=%q", code, errOut)
	}
	if len(f.requests()) != 0 {
		t.Error("aborted delete must not send a request")
	}
	_, _, code = run(t, f, []string{"changelog", "delete", "7"}, runOpts{stdin: "y\n", tty: true})
	if code != 0 || len(f.requests()) != 1 {
		t.Errorf("confirmed delete should send the request: exit=%d reqs=%d", code, len(f.requests()))
	}
}

func TestOpenAPIDetachPromptNamesTheDeletion(t *testing.T) {
	f := &fakeAPI{body: `{"detached":true,"removed":4}`}
	_, errOut, _ := run(t, f, []string{"openapi", "detach"}, runOpts{stdin: "n\n", tty: true, term: true})
	if !strings.Contains(errOut, "and delete its generated pages?") {
		t.Errorf("the prompt must say that detach deletes the generated pages: %q", errOut)
	}
	out, _, _ := run(t, f, []string{"help", "openapi"}, runOpts{})
	if !strings.Contains(out, "Detach the OpenAPI spec and delete the pages it generated") {
		t.Errorf("help must say that detach deletes the generated pages:\n%s", out)
	}
}

func TestVersionAndCompletion(t *testing.T) {
	f := &fakeAPI{}
	out, _, code := run(t, f, []string{"version"}, runOpts{})
	if code != 0 || !strings.HasPrefix(out, "cowl ") {
		t.Errorf("version: exit=%d out=%q", code, out)
	}
	out, _, code = run(t, f, []string{"version", "--json"}, runOpts{})
	var v map[string]string
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil || v["version"] == "" || v["os"] == "" {
		t.Errorf("version --json: exit=%d out=%q", code, out)
	}
	for _, shell := range []string{"bash", "zsh", "fish"} {
		out, _, code = run(t, f, []string{"completion", shell}, runOpts{})
		if code != 0 || !strings.Contains(out, "cowl") || !strings.Contains(out, "articles") || strings.Contains(out, "memory") {
			t.Errorf("completion %s: exit=%d", shell, code)
		}
	}
	if len(f.requests()) != 0 {
		t.Error("local commands must not call the API")
	}
}

func TestVersionFromBuildInfo(t *testing.T) {
	old, oldVersion := readBuildInfo, Version
	defer func() { readBuildInfo, Version = old, oldVersion }()
	Version = "dev"
	tests := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{"go install records the module version", &debug.BuildInfo{Main: debug.Module{Version: "v1.4.0"}}, "v1.4.0"},
		{"local build uses the revision", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef0123"}, {Key: "vcs.modified", Value: "true"},
		}}, "dev-0123456789ab-dirty"},
		{"no build info", nil, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			readBuildInfo = func() (*debug.BuildInfo, bool) { return tt.info, tt.info != nil }
			if got := version(); got != tt.want {
				t.Errorf("version() = %q, want %q", got, tt.want)
			}
		})
	}
	Version = "v2.0.0"
	if got := version(); got != "v2.0.0" {
		t.Errorf("ldflags version must win, got %q", got)
	}
}

func TestSnippetText(t *testing.T) {
	got := snippetText("\ue000match\ue001  and\n\nmore")
	if got != "match and more" {
		t.Errorf("snippetText = %q", got)
	}
	long := strings.Repeat("é", 100)
	if got := snippetText(long); len([]rune(got)) != 81 { // 80 + ellipsis
		t.Errorf("truncation wrong: %d runes", len([]rune(got)))
	}
}

func TestDoubleDashTerminator(t *testing.T) {
	f := &fakeAPI{body: `{"semantic":false,"results":[]}`}
	_, errOut, code := run(t, f, []string{"search", "--", "foo", "--semantic"}, runOpts{})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if req := lastReq(t, f); req.Query != "limit=10&q=foo+--semantic" {
		t.Errorf("query = %q, want the flags-like words treated as the query", req.Query)
	}

	f2 := &fakeAPI{body: `{"key":"k","label":"-Label","created":true}`}
	_, errOut, code = run(t, f2, []string{"sections", "create", "--", "-Label"}, runOpts{})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := decodeBody(t, lastReq(t, f2).Body)["label"]; got != "-Label" {
		t.Errorf("label = %v", got)
	}
}

func TestHelp(t *testing.T) {
	f := &fakeAPI{}
	out, _, code := run(t, f, []string{"articles", "--help"}, runOpts{})
	if code != 0 || !strings.Contains(out, "cowl articles create") {
		t.Errorf("group help: exit %d\n%s", code, out)
	}
	out, _, code = run(t, f, []string{"articles", "update", "--help"}, runOpts{})
	if code != 0 || !strings.Contains(out, "-base-revision") || !strings.Contains(out, "-allow-shrink") {
		t.Errorf("command help goes to stdout: exit %d\n%s", code, out)
	}
	out, _, code = run(t, f, []string{"help"}, runOpts{})
	for _, want := range []string{"CONTEXTOWL_PAT", "exit codes", "whoami", "doctor"} {
		if code != 0 || !strings.Contains(out, want) {
			t.Errorf("root help missing %q:\n%s", want, out)
		}
	}
	for _, gone := range []string{"memory", "--token", "--base-url", "—"} {
		if strings.Contains(out, gone) {
			t.Errorf("root help still mentions %q", gone)
		}
	}
	if len(f.requests()) != 0 {
		t.Error("help must not call the API")
	}
}

func TestRESTOpIDs(t *testing.T) {
	want := []string{
		"attachOpenAPI", "autofillLanding", "createArticle", "createChangelog", "createOpenAPISection", "createSection",
		"createWorkspace", "deleteChangelog", "deleteWorkspace", "detachOpenAPI", "detachOpenAPIPage", "getArticle",
		"getChangelog", "getLanding", "getMe", "getOpenAPISpec", "getOpenAPIStatus", "listArticles", "listChangelog",
		"listOpenAPIPages", "listProposals", "listSections", "listWorkspaces", "placeArticle", "placeOpenAPIPage",
		"proposeArticleEdit", "proposeLandingEdit", "searchDocs", "setLanding", "syncOpenAPI", "updateArticle",
		"updateChangelog", "updateWorkspace", "uploadImage",
	}
	if got := RESTOpIDs(); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("RESTOpIDs() =\n%v\nwant\n%v", got, want)
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

func itoa(n int) string { return strconv.Itoa(n) }
