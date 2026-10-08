package cli

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const reviewURL = "https://contextowl.co/admin/proposals?ws=platform&id=42"

// pendingBody is the 202 body of a write that the org reviews, as the server
// sends it: & stays as it is in the review URL.
func pendingBody(objectType, target, summary, outcome string) string {
	return `{"pendingReview":true,"message":"An editor must approve this change before readers see it. Give the user the reviewUrl.",` +
		`"proposal":{"id":42,"objectType":"` + objectType + `","status":"pending","target":"` + target + `",` +
		`"summary":"` + summary + `","reviewUrl":"` + reviewURL + `","baseRevision":"8f3a2c1b9d0e","outcome":"` + outcome + `"}}`
}

// reviewedWrite is a write command that the org can review. args leave out
// --note, and note is the value that the test passes with --note.
type reviewedWrite struct {
	name                            string
	args                            []string
	stdin                           string
	note                            string
	objectType, target, summary     string
	wantMethod, wantPath, wantQuery string
	wantBody                        map[string]any // nil: no body
}

func (w reviewedWrite) withNote() []string {
	return append(append([]string(nil), w.args...), "--note", w.note)
}

// reviewedWrites are the 11 commands whose REST operation answers 202 when the
// org reviews the change. wantQuery and wantBody hold the note.
var reviewedWrites = []reviewedWrite{
	{name: "articles update", args: []string{"articles", "update", "intro", "--status", "STABLE"}, note: "Ready to ship.",
		objectType: "article", target: "article:17", summary: "Publish intro: DRAFT to STABLE",
		wantMethod: "PATCH", wantPath: "/api/v1/workspaces/-/articles/intro",
		wantBody: map[string]any{"status": "STABLE", "note": "Ready to ship."}},
	{name: "articles place", args: []string{"articles", "place", "intro", "--section", "guides"}, note: "Group the guides.",
		objectType: "placement", target: "placement:17", summary: "Move intro from reference to guides",
		wantMethod: "POST", wantPath: "/api/v1/workspaces/-/articles/intro/placement",
		wantBody: map[string]any{"section": "guides", "note": "Group the guides."}},
	{name: "landing set", args: []string{"landing", "set", "--file", "-"}, stdin: `{"headline":"New"}`, note: "New hero.",
		objectType: "landing", target: "landing", summary: "Change the landing page",
		wantMethod: "PUT", wantPath: "/api/v1/workspaces/-/landing", wantQuery: "note=New+hero.",
		wantBody: map[string]any{"headline": "New"}},
	{name: "changelog create", args: []string{"changelog", "create", "--title", "Release 3.1", "--markdown", "notes", "--status", "published"}, note: "Release day.",
		objectType: "changelog", target: "changelog-new:4f1c", summary: "Publish changelog entry: Release 3.1",
		wantMethod: "POST", wantPath: "/api/v1/workspaces/-/changelog",
		wantBody: map[string]any{"title": "Release 3.1", "markdown": "notes", "status": "published", "note": "Release day."}},
	{name: "changelog update", args: []string{"changelog", "update", "7", "--title", "Release 3.0 final"}, note: "Fix the title.",
		objectType: "changelog", target: "changelog:7", summary: "Change changelog entry: Release 3.0 final",
		wantMethod: "PATCH", wantPath: "/api/v1/workspaces/-/changelog/7",
		wantBody: map[string]any{"title": "Release 3.0 final", "note": "Fix the title."}},
	{name: "changelog delete", args: []string{"changelog", "delete", "7", "--yes"}, note: "A duplicate entry.",
		objectType: "changelog", target: "changelog:7", summary: "Delete changelog entry: Release 3.0",
		wantMethod: "DELETE", wantPath: "/api/v1/workspaces/-/changelog/7", wantQuery: "note=A+duplicate+entry."},
	{name: "openapi attach", args: []string{"openapi", "attach", "--url", "https://example.com/spec.json"}, note: "Version 2 of the API.",
		objectType: "openapi", target: "openapi", summary: "Attach the API reference: 5 new, 0 changed, 0 removed",
		wantMethod: "PUT", wantPath: "/api/v1/workspaces/-/openapi",
		wantBody: map[string]any{"url": "https://example.com/spec.json", "note": "Version 2 of the API."}},
	{name: "openapi sync", args: []string{"openapi", "sync"}, note: "New endpoints.",
		objectType: "openapi", target: "openapi", summary: "Sync the API reference: 2 new, 1 changed, 0 removed",
		wantMethod: "POST", wantPath: "/api/v1/workspaces/-/openapi/sync", wantQuery: "note=New+endpoints."},
	{name: "openapi detach", args: []string{"openapi", "detach", "--yes"}, note: "The API is gone.",
		objectType: "openapi", target: "openapi", summary: "Detach the API reference: 4 removed",
		wantMethod: "DELETE", wantPath: "/api/v1/workspaces/-/openapi", wantQuery: "note=The+API+is+gone."},
	{name: "openapi place", args: []string{"openapi", "place", "get-users", "--section", "api"}, note: "Group the user endpoints.",
		objectType: "placement", target: "placement:21", summary: "Move get-users from no section to api",
		wantMethod: "POST", wantPath: "/api/v1/workspaces/-/openapi/pages/get-users/placement",
		wantBody: map[string]any{"section": "api", "note": "Group the user endpoints."}},
	{name: "workspaces update", args: []string{"workspaces", "update", "platform", "--access-mode", "public"}, note: "Open the docs.",
		objectType: "workspace", target: "workspace", summary: "Change workspace settings: access mode",
		wantMethod: "PATCH", wantPath: "/api/v1/workspaces/platform",
		wantBody: map[string]any{"access_mode": "public", "note": "Open the docs."}},
}

// TestWritesThatWaitForReview: each reviewed write sends --note and prints
// the proposal and its review link on a 202. In a pipe and with --json, it
// prints the REST body. Each case exits 0.
func TestWritesThatWaitForReview(t *testing.T) {
	for _, tt := range reviewedWrites {
		t.Run(tt.name, func(t *testing.T) {
			body := pendingBody(tt.objectType, tt.target, tt.summary, "created")
			f := &fakeAPI{status: http.StatusAccepted, body: body}
			out, errOut, code := run(t, f, tt.withNote(), runOpts{stdin: tt.stdin, term: true})
			if want := "proposal 42 waits for review: " + reviewURL + "\n  " + tt.summary + "\n"; code != 0 || out != want || errOut != "" {
				t.Fatalf("terminal: exit %d\nstdout %q\nwant   %q\nstderr %q", code, out, want, errOut)
			}
			req := lastReq(t, f)
			if req.Method != tt.wantMethod || req.Path != tt.wantPath || req.Query != tt.wantQuery {
				t.Errorf("request = %s %s?%s, want %s %s?%s", req.Method, req.Path, req.Query, tt.wantMethod, tt.wantPath, tt.wantQuery)
			}
			if tt.wantBody == nil {
				if req.Body != "" {
					t.Errorf("unexpected request body: %q", req.Body)
				}
			} else {
				got, _ := json.Marshal(decodeBody(t, req.Body))
				if want, _ := json.Marshal(tt.wantBody); string(got) != string(want) {
					t.Errorf("body = %s, want %s", got, want)
				}
			}
			for _, mode := range []struct {
				name string
				args []string
				opts runOpts
			}{
				{"pipe", tt.withNote(), runOpts{stdin: tt.stdin}},
				{"--json", append(tt.withNote(), "--json"), runOpts{stdin: tt.stdin, term: true}},
			} {
				out, errOut, code := run(t, f, mode.args, mode.opts)
				if want := compactJSON(t, body) + "\n"; code != 0 || out != want || errOut != "" {
					t.Errorf("%s: exit %d, stdout %q, stderr %q, want the REST body", mode.name, code, out, errOut)
				}
			}
		})
	}
}

// TestWriteNoteOnlyWhenSet: an older server rejects an unknown body field, so
// a write without --note, or with a blank one, sends no note.
func TestWriteNoteOnlyWhenSet(t *testing.T) {
	for _, tt := range reviewedWrites {
		for _, extra := range [][]string{nil, {"--note", "  "}} {
			t.Run(tt.name+strings.Join(extra, " "), func(t *testing.T) {
				f := &fakeAPI{body: `{}`}
				args := append(append([]string(nil), tt.args...), extra...)
				if _, errOut, code := run(t, f, args, runOpts{stdin: tt.stdin}); code != 0 {
					t.Fatalf("exit %d: %s", code, errOut)
				}
				req := lastReq(t, f)
				if q, _ := url.ParseQuery(req.Query); q.Has("note") {
					t.Errorf("query = %q, want no note", req.Query)
				}
				if req.Body != "" {
					if _, ok := decodeBody(t, req.Body)["note"]; ok {
						t.Errorf("body = %s, want no note", req.Body)
					}
				}
			})
		}
	}
}

// TestPendingReviewReceipts: the receipt says whether the write filed a new
// proposal or changed the working copy of the key.
func TestPendingReviewReceipts(t *testing.T) {
	const summary = "Change intro: text"
	tests := []struct {
		name     string
		body     string
		wantOut  string
		wantNote string
	}{
		{name: "created", body: pendingBody("article", "article:17", summary, "created"),
			wantOut: "proposal 42 waits for review: " + reviewURL + "\n  " + summary + "\n"},
		{name: "updated", body: pendingBody("article", "article:17", summary, "updated"),
			wantOut: "proposal 42 now holds this change too and waits for review: " + reviewURL + "\n  " + summary + "\n"},
		{name: "unchanged", body: pendingBody("article", "article:17", summary, "unchanged"),
			wantOut: "proposal 42 already holds this change and waits for review: " + reviewURL + "\n  " + summary + "\n"},
		{name: "rebased", body: pendingBody("article", "article:17", summary, "rebased"),
			wantOut:  "proposal 42 now holds only this change and waits for review: " + reviewURL + "\n  " + summary + "\n",
			wantNote: "note: the live content changed after the earlier change of this key, so proposal 42 no longer holds the earlier change.\n"},
		{name: "outcome from a newer server", body: pendingBody("article", "article:17", summary, "merged"),
			wantOut: "proposal 42 waits for review: " + reviewURL + "\n  " + summary + "\n"},
		{name: "summary with line breaks", body: pendingBody("article", "article:17", `Change intro:\n\ttext`, "created"),
			wantOut: "proposal 42 waits for review: " + reviewURL + "\n  Change intro: text\n"},
		{name: "no summary and no review URL", body: `{"pendingReview":true,"proposal":{"id":42,"objectType":"landing","status":"pending","outcome":"created"}}`,
			wantOut: "proposal 42 waits for review\n"},
		{name: "202 without a proposal", body: `{"accepted":true}`,
			wantOut: "{\n  \"accepted\": true\n}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeAPI{status: http.StatusAccepted, body: tt.body}
			args := []string{"articles", "update", "intro", "--markdown", "New text."}
			out, errOut, code := run(t, f, args, runOpts{term: true})
			if code != 0 || out != tt.wantOut || errOut != tt.wantNote {
				t.Errorf("exit %d\nstdout %q\nwant   %q\nstderr %q\nwant   %q", code, out, tt.wantOut, errOut, tt.wantNote)
			}
			out, errOut, code = run(t, f, args, runOpts{})
			if want := compactJSON(t, tt.body) + "\n"; code != 0 || out != want || errOut != "" {
				t.Errorf("pipe: exit %d, stdout %q, stderr %q, want the REST body and no note", code, out, errOut)
			}
		})
	}
}

// TestArticlesUpdateWithdrawnProposal: an update that withdrew the pending
// proposal of the key says so on a terminal. An older server never sends
// withdrawnProposal.
func TestArticlesUpdateWithdrawnProposal(t *testing.T) {
	const live = `{"slug":"intro","status":"STABLE","revision":"333333333333","previousRevision":"333333333333","changed":[]`
	const draft = `{"slug":"intro","status":"DRAFT","url":"https://docs.example.com/docs/ws/intro","revision":"444444444444","previousRevision":"333333333333","changed":["markdown"]`
	tests := []struct {
		name     string
		body     string
		wantOut  string
		wantNote string
	}{
		{name: "draft", body: draft + `,"withdrawnProposal":{"id":12,"reason":"draft"}}`,
			wantOut: "updated article intro (changed markdown) revision 444444444444 https://docs.example.com/docs/ws/intro\n",
			wantNote: "note: this change withdrew proposal 12, because it changes only a draft, which saved directly. " +
				"If that proposal held a publish request, run the update again with --status to file it.\n"},
		{name: "live", body: live + `,"withdrawnProposal":{"id":12,"reason":"live"}}`,
			wantOut:  "article intro is unchanged (revision 333333333333)\n",
			wantNote: "note: this change withdrew proposal 12, because the change now matches the live article.\n"},
		{name: "reason from a newer server", body: live + `,"withdrawnProposal":{"id":12,"reason":"expired"}}`,
			wantOut:  "article intro is unchanged (revision 333333333333)\n",
			wantNote: "note: this change withdrew proposal 12.\n"},
		{name: "older server", body: live + `}`,
			wantOut: "article intro is unchanged (revision 333333333333)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeAPI{body: tt.body}
			args := []string{"articles", "update", "intro", "--markdown", "Text."}
			out, errOut, code := run(t, f, args, runOpts{term: true})
			if code != 0 || out != tt.wantOut || errOut != tt.wantNote {
				t.Errorf("exit %d\nstdout %q\nwant   %q\nstderr %q\nwant   %q", code, out, tt.wantOut, errOut, tt.wantNote)
			}
			out, errOut, code = run(t, f, args, runOpts{})
			if want := compactJSON(t, tt.body) + "\n"; code != 0 || out != want || errOut != "" {
				t.Errorf("pipe: exit %d, stdout %q, stderr %q, want the REST body and no note", code, out, errOut)
			}
		})
	}
}

// TestReviewRequired: a write that has no proposal form fails with
// review_required and exits 4.
func TestReviewRequired(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		message    string
		wantMethod string
		wantPath   string
	}{
		{"workspaces delete", []string{"workspaces", "delete", "platform", "--yes"},
			"this organization reviews changes from agent keys, so only a publishing key can delete a workspace.", "DELETE", "/api/v1/workspaces/platform"},
		{"workspaces create public", []string{"workspaces", "create", "Docs", "--access-mode", "public"},
			"this organization reviews changes from agent keys, so this key cannot create a public or listed workspace.", "POST", "/api/v1/workspaces"},
		{"workspaces create listed", []string{"workspaces", "create", "Docs", "--listed"},
			"this organization reviews changes from agent keys, so this key cannot create a public or listed workspace.", "POST", "/api/v1/workspaces"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"error":{"code":"review_required","message":"` + tt.message + `","status":403}}`
			f := &fakeAPI{status: http.StatusForbidden, body: body}
			out, errOut, code := run(t, f, tt.args, runOpts{})
			if code != exitAuth || out != "" || errOut != body+"\n" {
				t.Errorf("pipe: exit %d, stdout %q, stderr %q, want exit 4 and the envelope", code, out, errOut)
			}
			if req := lastReq(t, f); req.Method != tt.wantMethod || req.Path != tt.wantPath {
				t.Errorf("request = %s %s, want %s %s", req.Method, req.Path, tt.wantMethod, tt.wantPath)
			}
			_, errOut, code = run(t, f, tt.args, runOpts{term: true})
			want := "cowl: review_required: " + tt.message + "\n" +
				"  Ask an admin to make this change. An admin can also approve this key as a publishing key in Admin > Settings > API.\n"
			if code != exitAuth || errOut != want {
				t.Errorf("terminal: exit %d\nstderr %q\nwant   %q", code, errOut, want)
			}
		})
	}
}

// TestAPIPrintsPendingReview: the escape hatch prints a 202 body as it is and
// exits 0.
func TestAPIPrintsPendingReview(t *testing.T) {
	body := pendingBody("article", "article:17", "Publish intro: DRAFT to STABLE", "created")
	f := &fakeAPI{status: http.StatusAccepted, body: body}
	args := []string{"api", "PATCH", "workspaces/-/articles/intro", "--input", "-"}
	out, errOut, code := run(t, f, args, runOpts{stdin: `{"status":"STABLE"}`})
	if want := compactJSON(t, body) + "\n"; code != 0 || out != want || errOut != "" {
		t.Errorf("pipe: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	out, _, code = run(t, f, args, runOpts{stdin: `{"status":"STABLE"}`, term: true})
	if code != 0 || !strings.Contains(out, "\"pendingReview\": true,\n") || !strings.Contains(out, `"reviewUrl": "`+reviewURL+`"`) {
		t.Errorf("terminal: exit %d, want the indented body:\n%s", code, out)
	}
}

// meWrites is meJSON from a server that sends writes and publishingKey.
func meWrites(writes string, publishingKey bool) string {
	pk := "false"
	if publishingKey {
		pk = "true"
	}
	return strings.TrimSuffix(meJSON, "}") + `,"writes":"` + writes + `","publishingKey":` + pk + `}`
}

// outLine returns the line of out that starts with prefix, with its
// whitespace collapsed, or "" when out has no such line.
func outLine(out, prefix string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.Join(strings.Fields(line), " ")
		}
	}
	return ""
}

// TestKeyWrites: whoami, auth status and auth login say how the changes of
// the key to live content apply. An older server sends no writes, and then
// the commands print as before.
func TestKeyWrites(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantWrites     string
		wantPublishing string
	}{
		{"review", meWrites("review", false), "writes: review, changes to live content wait for an editor", ""},
		{"publishing key", meWrites("direct", true), "writes: direct, changes to live content apply at once", "publishing key: yes"},
		{"org without review", meWrites("direct", false), "writes: direct, changes to live content apply at once", ""},
		{"read key", meWrites("none", false), "writes: none, the key cannot change content that readers see", ""},
		{"value from a newer server", meWrites("scheduled", false), "writes: scheduled", ""},
		{"older server", meJSON, "", ""},
	}
	for _, tt := range tests {
		for _, args := range [][]string{{"whoami"}, {"auth", "status"}} {
			t.Run(tt.name+"/"+strings.Join(args, " "), func(t *testing.T) {
				f := &fakeAPI{body: tt.body}
				out, errOut, code := run(t, f, args, runOpts{term: true})
				if code != 0 {
					t.Fatalf("exit %d: %s", code, errOut)
				}
				if got := outLine(out, "writes:"); got != tt.wantWrites {
					t.Errorf("writes line = %q, want %q\n%s", got, tt.wantWrites, out)
				}
				if got := outLine(out, "publishing key:"); got != tt.wantPublishing {
					t.Errorf("publishing key line = %q, want %q\n%s", got, tt.wantPublishing, out)
				}
				out, _, code = run(t, f, args, runOpts{})
				if code != 0 || out != tt.body+"\n" {
					t.Errorf("a pipe gets the getMe body as it is: exit=%d out=%q", code, out)
				}
			})
		}
		t.Run(tt.name+"/auth login", func(t *testing.T) {
			f := &fakeAPI{body: tt.body}
			base := serve(t, f)
			env := map[string]string{"CONTEXTOWL_PAT": "", "CONTEXTOWL_BASE_URL": ""}
			out, errOut, code := runAt(t, base, []string{"auth", "login", "--with-token", "--base-url", base}, runOpts{stdin: "cowl_pat_new\n", env: env, term: true})
			if code != 0 {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			if got := outLine(out, "writes:"); got != tt.wantWrites {
				t.Errorf("writes line = %q, want %q\n%s", got, tt.wantWrites, out)
			}
			if got := outLine(out, "publishing key:"); got != tt.wantPublishing {
				t.Errorf("publishing key line = %q, want %q\n%s", got, tt.wantPublishing, out)
			}
		})
	}
}

// TestDoctorWrites: doctor reports writes when the server sends it, and adds
// a note when the changes of the key wait for review.
func TestDoctorWrites(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		want     string
		wantNote bool
	}{
		{"review", meWrites("review", false), "review", true},
		{"direct", meWrites("direct", true), "direct", false},
		{"older server", meJSON, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeAPI{body: tt.body}
			out, errOut, code := run(t, f, []string{"doctor"}, runOpts{})
			if code != 0 {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			rep := doctorJSON(t, out)
			if rep.Writes != tt.want || strings.Contains(out, `"writes"`) != (tt.want != "") {
				t.Errorf("writes = %q, want %q:\n%s", rep.Writes, tt.want, out)
			}
			hasNote := false
			for _, n := range rep.Notes {
				hasNote = hasNote || n == reviewNote
			}
			if hasNote != tt.wantNote {
				t.Errorf("notes = %q, want the review note: %v", rep.Notes, tt.wantNote)
			}
			for _, secret := range []string{"Support agent", "ContextOwl Developers", "platform"} {
				if strings.Contains(out, secret) {
					t.Errorf("doctor printed %q:\n%s", secret, out)
				}
			}
			term, _, _ := run(t, f, []string{"doctor"}, runOpts{term: true})
			want := ""
			if tt.want != "" {
				want = "writes " + tt.want
			}
			if got := outLine(term, "writes"); got != want {
				t.Errorf("writes line = %q, want %q\n%s", got, want, term)
			}
		})
	}
}

// TestProposalsUnderReview: the proposal list shows the summary of each kind
// and the withdrawn proposals. One proposal shows its target and summary, and
// its stale line names the kind of target.
func TestProposalsUnderReview(t *testing.T) {
	rows := `[` +
		`{"id":51,"objectType":"changelog","status":"pending","target":"changelog:7","slug":"","title":"Release 3.0","summary":"Delete changelog entry: Release 3.0","note":"","reviewNote":"","author":"bot","createdAt":"2026-10-07T10:00:00Z","reviewedAt":null,"stale":false},` +
		`{"id":50,"objectType":"placement","status":"pending","target":"placement:17","slug":"intro","title":"Intro","summary":"Move intro from reference to guides","note":"","reviewNote":"","author":"bot","createdAt":"2026-10-07T09:00:00Z","reviewedAt":null,"stale":false},` +
		`{"id":49,"objectType":"openapi","status":"pending","target":"openapi","slug":"","title":"Payments API","summary":"Sync the API reference: 2 new, 1 changed, 0 removed","note":"","reviewNote":"","author":"bot","createdAt":"2026-10-07T08:00:00Z","reviewedAt":null,"stale":false},` +
		`{"id":48,"objectType":"workspace","status":"pending","target":"workspace","slug":"","title":"","summary":"Change workspace settings: access mode","note":"","reviewNote":"","author":"bot","createdAt":"2026-10-07T07:00:00Z","reviewedAt":null,"stale":false},` +
		`{"id":47,"objectType":"article","status":"rejected","target":"article:17","slug":"intro","title":"Intro","summary":"Publish intro: DRAFT to STABLE","note":"","reviewNote":"Withdrawn: the latest change from this key changes only a draft, so it saved directly.","withdrawn":true,"author":"bot","createdAt":"2026-10-07T06:00:00Z","reviewedAt":"2026-10-07T06:30:00Z"},` +
		`{"id":4,"objectType":"article","status":"approved","slug":"api-keys","title":"Agent Keys","note":"typo","reviewNote":"","author":"bot","createdAt":"2026-07-19T10:00:00Z","reviewedAt":"2026-07-20T10:00:00Z"}` +
		`]`
	f := &fakeAPI{body: rows}
	out, errOut, code := run(t, f, []string{"proposals", "list", "--status", "all"}, runOpts{term: true})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{
		"51 changelog pending - Delete changelog entry: Release 3.0 ",
		"50 placement pending intro Move intro from reference to guides ",
		"49 openapi pending - Sync the API reference: 2 new, 1 changed, 0 removed ",
		"48 workspace pending - Change workspace settings: access mode ",
		"47 article withdrawn intro Publish intro: DRAFT to STABLE ",
		"4 article approved api-keys Agent Keys ",
	} {
		if !strings.Contains(collapse(out), want) {
			t.Errorf("list missing the row %q:\n%s", want, out)
		}
	}
	if !strings.HasPrefix(out, "ID ") || !strings.Contains(strings.SplitN(out, "\n", 2)[0], " SUMMARY ") {
		t.Errorf("the header must name SUMMARY:\n%s", out)
	}
	if out, _, _ := run(t, f, []string{"proposals", "list", "--status", "all"}, runOpts{}); out != compactJSON(t, rows)+"\n" {
		t.Errorf("a pipe gets the REST body as it is: %q", out)
	}

	one := `[{"id":51,"objectType":"changelog","status":"pending","target":"changelog:7","slug":"","title":"Release 3.0","summary":"Delete changelog entry: Release 3.0",` +
		`"note":"A duplicate entry.","reviewNote":"","author":"bot","createdAt":"2026-10-07T10:00:00Z","reviewedAt":null,"stale":true,"baseRevision":"aaaaaaaaaaaa","currentRevision":"bbbbbbbbbbbb"}]`
	f = &fakeAPI{body: one}
	out, errOut, code = run(t, f, []string{"proposals", "get", "51"}, runOpts{term: true})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for label, want := range map[string]string{
		"type:":    "type: changelog",
		"status:":  "status: pending",
		"target:":  "target: changelog:7",
		"summary:": "summary: Delete changelog entry: Release 3.0",
		"stale:":   "stale: yes, the changelog entry changed after the proposal",
	} {
		if got := outLine(out, label); got != want {
			t.Errorf("%s line = %q, want %q\n%s", label, got, want, out)
		}
	}
	if lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n"); !strings.HasPrefix(lines[len(lines)-1], "review note:") {
		t.Errorf("a proposal without markdown prints no text after the fields:\n%s", out)
	}

	withdrawn := `[{"id":47,"objectType":"article","status":"rejected","target":"article:17","slug":"intro","title":"Intro","summary":"Publish intro: DRAFT to STABLE",` +
		`"note":"","reviewNote":"Withdrawn: the latest change from this key changes only a draft, so it saved directly.","withdrawn":true,"author":"bot",` +
		`"createdAt":"2026-10-07T06:00:00Z","reviewedAt":"2026-10-07T06:30:00Z","baseRevision":"aaaaaaaaaaaa","currentRevision":"aaaaaaaaaaaa"}]`
	out, _, _ = run(t, &fakeAPI{body: withdrawn}, []string{"proposals", "get", "47"}, runOpts{term: true})
	if got, want := outLine(out, "status:"), "status: withdrawn, a later write of the key made the proposal unneeded"; got != want {
		t.Errorf("status line = %q, want %q\n%s", got, want, out)
	}
}

// collapse turns each run of whitespace into one space, so a test can match
// a table row without its column widths.
func collapse(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ") + " "
	}
	return strings.Join(lines, "\n")
}

// TestStaleLineNamesTheTarget: the stale line of proposals get names the
// kind of target.
func TestStaleLineNamesTheTarget(t *testing.T) {
	for kind, want := range map[string]string{
		"article":   "the article",
		"placement": "the article",
		"landing":   "the landing page",
		"changelog": "the changelog entry",
		"workspace": "the workspace settings",
		"openapi":   "the API reference",
	} {
		if got := changedTarget(kind); got != want {
			t.Errorf("changedTarget(%q) = %q, want %q", kind, got, want)
		}
	}
}

// TestReviewErrorHints: the terminal hints of the errors that the review of
// agent changes adds.
func TestReviewErrorHints(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		stdin  string
		status int
		body   string
		want   string
	}{
		{"edit of a pending proposal", []string{"articles", "update", "intro", "--edits", "-"}, editsJSON, 422,
			`{"error":{"code":"edit_not_found","message":"edit 0 does not occur","status":422,"details":{"index":0,"proposalId":12}}}`,
			"  The edits apply to proposal 12, which waits for review. Run 'cowl proposals get 12', then copy the old text of edits[0] exactly from its Markdown.\n"},
		{"edit of the live article", []string{"articles", "update", "intro", "--edits", "-"}, editsJSON, 422,
			`{"error":{"code":"edit_not_found","message":"edit 0 does not occur","status":422,"details":{"index":0}}}`,
			"  Read the article again and copy the old text of edits[0] exactly.\n"},
		{"note on an older server", []string{"articles", "update", "intro", "--markdown", "x", "--note", "Why."}, "", 400,
			`{"error":{"code":"invalid_body","message":"invalid JSON: json: unknown field \"note\"","status":400}}`,
			"  This server does not take --note. Run the command again without --note.\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeAPI{status: tt.status, body: tt.body}
			_, errOut, code := run(t, f, tt.args, runOpts{stdin: tt.stdin, term: true})
			if code != exitUsage || !strings.HasSuffix(errOut, tt.want) {
				t.Errorf("exit %d\nstderr %q\nwant the hint %q", code, errOut, tt.want)
			}
		})
	}
}

// TestReviewedWritesTakeNote: each reviewed write has --note in its flags
// and its usage.
func TestReviewedWritesTakeNote(t *testing.T) {
	if len(reviewedWrites) != 11 {
		t.Fatalf("reviewedWrites has %d commands, want 11", len(reviewedWrites))
	}
	for _, w := range reviewedWrites {
		cmd, _, err := dispatch(w.args)
		if err != nil {
			t.Fatalf("%s: %v", w.name, err)
		}
		if cmd.full() != w.name {
			t.Errorf("%s dispatches to %s", w.name, cmd.full())
		}
		flag := cmd.flagSet(&globals{}).Lookup("note")
		if flag == nil || flag.Usage != noteHelp || !strings.HasSuffix(cmd.Usage, " [--note NOTE]") {
			t.Errorf("cowl %s needs --note with the reviewer help and [--note NOTE] in its usage: %q", w.name, cmd.Usage)
		}
	}
	out, _, code := run(t, &fakeAPI{}, []string{"help"}, runOpts{})
	if want := "When the organization reviews agent changes, a write to live content waits in\na proposal for an editor."; code != 0 || !strings.Contains(out, want) {
		t.Errorf("root help must say that a write can wait for review:\n%s", out)
	}
}
