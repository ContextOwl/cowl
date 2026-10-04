package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func decodeEnvelope(t *testing.T, stderr string) errorDetail {
	t.Helper()
	if strings.Count(stderr, "\n") != 1 || !strings.HasSuffix(stderr, "\n") {
		t.Fatalf("stderr must be one JSON line, got %q", stderr)
	}
	var env struct {
		Error errorDetail `json:"error"`
	}
	if err := json.Unmarshal([]byte(stderr), &env); err != nil {
		t.Fatalf("stderr is not an error envelope: %v: %q", err, stderr)
	}
	return env.Error
}

func TestExitCodesForHTTPStatus(t *testing.T) {
	old := sleepFn
	sleepFn = func(time.Duration) {}
	defer func() { sleepFn = old }()

	tests := []struct {
		status int
		code   string
		want   int
	}{
		{400, "invalid_request", exitUsage},
		{413, "too_large", exitUsage},
		{422, "encrypted", exitUsage},
		{404, "not_found", exitNotFound},
		{401, "unauthorized", exitAuth},
		{402, "upgrade_required", exitAuth},
		{403, "permission_denied", exitAuth},
		{409, "stale_revision", exitConflict},
		{429, "rate_limited", exitRateLimit},
		{500, "internal", exitServer},
		{502, "bad_gateway", exitServer},
		{405, "method_not_allowed", exitError},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			body := `{"error":{"code":"` + tt.code + `","message":"m","status":` + itoa(tt.status) + `}}`
			f := &fakeAPI{status: tt.status, body: body}
			out, errOut, code := run(t, f, []string{"articles", "get", "x"}, runOpts{})
			if code != tt.want {
				t.Errorf("exit = %d, want %d", code, tt.want)
			}
			if out != "" {
				t.Errorf("stdout must stay empty on error: %q", out)
			}
			if errOut != body+"\n" {
				t.Errorf("stderr must be the server envelope unchanged:\n got %q\nwant %q", errOut, body+"\n")
			}
			if tt.status == 429 && len(f.requests()) != 2 {
				t.Errorf("429 must be retried once, requests = %d", len(f.requests()))
			}
		})
	}
}

func TestErrorEnvelopeKeepsDetails(t *testing.T) {
	body := `{"error":{"code":"stale_revision","message":"the article changed","status":409,"details":{"currentRevision":"aaaaaaaaaaaa","baseRevision":"bbbbbbbbbbbb"}}}`
	f := &fakeAPI{status: 409, body: body}
	args := []string{"articles", "update", "intro", "--markdown", "x", "--base-revision", "bbbbbbbbbbbb"}

	_, errOut, code := run(t, f, args, runOpts{})
	if code != exitConflict {
		t.Fatalf("exit = %d, want 5", code)
	}
	got := decodeEnvelope(t, errOut)
	if got.Code != "stale_revision" || got.Status != 409 || got.Details["currentRevision"] != "aaaaaaaaaaaa" {
		t.Errorf("envelope = %+v", got)
	}

	_, errOut, _ = run(t, f, args, runOpts{term: true})
	for _, want := range []string{"cowl: stale_revision: the article changed", "current revision: aaaaaaaaaaaa", "Read the article again"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("terminal error missing %q:\n%s", want, errOut)
		}
	}
}

func TestTerminalErrorHints(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   []string
	}{
		{"unauthorized", 401, `{"error":{"code":"unauthorized","message":"a valid access key is required","status":401}}`,
			[]string{"cowl: unauthorized: a valid access key is required", "cowl auth login"}},
		{"large removal", 422, `{"error":{"code":"large_removal","message":"the body removes 80% of the text","status":422,"details":{"removed":4000,"currentLength":5000}}}`,
			[]string{"pass --allow-shrink"}},
		{"unknown section", 400, `{"error":{"code":"invalid_request","message":"unknown section key","status":400,"details":{"field":"section_key","allowed":["guides","reference"]}}}`,
			[]string{"allowed: guides, reference"}},
		{"unknown anchor", 404, `{"error":{"code":"not_found","message":"no heading rotate","status":404,"details":{"anchors":["create-a-key","rotation"]}}}`,
			[]string{"anchors: create-a-key, rotation"}},
		{"slug taken", 409, `{"error":{"code":"slug_taken","message":"slug guide exists","status":409,"details":{"slug":"guide"}}}`,
			[]string{"Pass a different --slug"}},
		{"edit not found", 422, `{"error":{"code":"edit_not_found","message":"edit 2 does not occur","status":422,"details":{"index":2}}}`,
			[]string{"copy the old text of edits[2] exactly"}},
		{"edit ambiguous", 422, `{"error":{"code":"edit_ambiguous","message":"edit 0 occurs 3 times","status":422,"details":{"index":0,"matches":3}}}`,
			[]string{"old text of edits[0] so that it occurs only once"}},
		{"not JSON", 502, `<html>bad gateway</html>`, []string{"cowl: http_502: <html>bad gateway</html>"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeAPI{status: tt.status, body: tt.body}
			_, errOut, _ := run(t, f, []string{"articles", "get", "x"}, runOpts{term: true})
			for _, want := range tt.want {
				if !strings.Contains(errOut, want) {
					t.Errorf("stderr missing %q:\n%s", want, errOut)
				}
			}
		})
	}
}

func TestNonJSONErrorBecomesEnvelope(t *testing.T) {
	f := &fakeAPI{status: 502, body: "<html>bad gateway</html>"}
	_, errOut, code := run(t, f, []string{"articles", "list"}, runOpts{})
	if code != exitServer {
		t.Errorf("exit = %d, want 7", code)
	}
	got := decodeEnvelope(t, errOut)
	if got.Code != "http_502" || got.Status != 502 || got.Message != "<html>bad gateway</html>" {
		t.Errorf("envelope = %+v", got)
	}
}

func TestLocalErrorsUseTheEnvelope(t *testing.T) {
	t.Run("usage", func(t *testing.T) {
		f := &fakeAPI{}
		_, errOut, code := run(t, f, []string{"articles", "get"}, runOpts{})
		got := decodeEnvelope(t, errOut)
		if code != exitUsage || got.Code != "usage" || got.Status != 0 || got.Details["usage"] != "cowl articles get SLUG... [--section ANCHOR]" {
			t.Errorf("exit=%d envelope=%+v", code, got)
		}
	})
	t.Run("flag parse error", func(t *testing.T) {
		f := &fakeAPI{}
		_, errOut, code := run(t, f, []string{"articles", "list", "--bogus"}, runOpts{})
		got := decodeEnvelope(t, errOut)
		if code != exitUsage || got.Code != "usage" || !strings.Contains(got.Message, "-bogus") {
			t.Errorf("exit=%d envelope=%+v", code, got)
		}
	})
	t.Run("no command", func(t *testing.T) {
		f := &fakeAPI{}
		_, errOut, code := run(t, f, nil, runOpts{})
		if got := decodeEnvelope(t, errOut); code != exitUsage || got.Code != "usage" {
			t.Errorf("exit=%d envelope=%+v", code, got)
		}
		_, errOut, code = run(t, f, nil, runOpts{term: true})
		if code != exitUsage || !strings.Contains(errOut, "usage: cowl <command>") {
			t.Errorf("a terminal gets the root help: exit=%d\n%s", code, errOut)
		}
	})
	t.Run("no key", func(t *testing.T) {
		f := &fakeAPI{}
		_, errOut, code := run(t, f, []string{"articles", "list"}, runOpts{env: map[string]string{"CONTEXTOWL_PAT": ""}})
		got := decodeEnvelope(t, errOut)
		if code != exitAuth || got.Code != "no_key" || got.Status != 0 {
			t.Errorf("exit=%d envelope=%+v", code, got)
		}
		if len(f.requests()) != 0 {
			t.Error("no request may go out without a key")
		}
		_, errOut, _ = run(t, f, []string{"articles", "list"}, runOpts{env: map[string]string{"CONTEXTOWL_PAT": ""}, term: true})
		if !strings.Contains(errOut, "cowl: no_key: no agent key: run 'cowl auth login', or set CONTEXTOWL_PAT") {
			t.Errorf("terminal no_key error: %q", errOut)
		}
	})
	t.Run("network error", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		_, errOut, code := runAt(t, url, []string{"articles", "list"}, runOpts{})
		got := decodeEnvelope(t, errOut)
		if code != exitServer || got.Code != "network_error" || got.Status != 0 || !strings.Contains(got.Message, "cannot reach "+url) {
			t.Errorf("exit=%d envelope=%+v", code, got)
		}
	})
	t.Run("invalid response", func(t *testing.T) {
		f := &fakeAPI{body: `{"not":"a list"}`}
		_, errOut, code := run(t, f, []string{"articles", "list"}, runOpts{outTTY: true})
		if got := decodeEnvelope(t, errOut); code != exitError || got.Code != "invalid_response" {
			t.Errorf("exit=%d envelope=%+v", code, got)
		}
	})
}

func TestEmptyProposalResultIsNotFound(t *testing.T) {
	f := &fakeAPI{body: `[]`}
	_, errOut, code := run(t, f, []string{"proposals", "get", "77"}, runOpts{})
	if got := decodeEnvelope(t, errOut); code != exitNotFound || got.Code != "not_found" {
		t.Errorf("exit=%d envelope=%+v", code, got)
	}
}
