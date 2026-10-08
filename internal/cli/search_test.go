package cli

import (
	"strings"
	"testing"
)

func TestSearchLearnedColumn(t *testing.T) {
	const (
		rotate = `{"type":"article","slug":"rotate-keys","title":"Rotate keys","status":"STABLE","updatedAt":"2026-10-01T09:00:00Z",` +
			`"url":"https://docs.example.com/docs/ws/rotate-keys","snippet":"rotate a key"`
		keys = `{"type":"article","slug":"api-keys","title":"Agent Keys","status":"STABLE","updatedAt":"2026-09-30T14:02:11Z",` +
			`"url":"https://docs.example.com/docs/ws/api-keys","snippet":"keys expire"`
		entry = `{"type":"changelog","id":42,"title":"Release 2.4","status":"published","publishedAt":"2026-09-20T10:00:00Z",` +
			`"url":"https://docs.example.com/docs/ws/changelog#e42","snippet":"keys now expire"}`
	)
	tests := []struct {
		name    string
		body    string
		header  []string          // the last columns of the header
		learned map[string]string // the LEARNED cell of each row, by SLUG/ID
	}{
		{
			name:    "full-text with a learned hit",
			body:    `{"semantic":false,"results":[` + rotate + `,"learned":true},` + keys + `},` + entry + `]}`,
			header:  []string{"URL", "LEARNED"},
			learned: map[string]string{"rotate-keys": "yes", "api-keys": "", "42": ""},
		},
		{
			name:    "semantic with a learned hit",
			body:    `{"semantic":true,"results":[` + rotate + `,"score":0.71,"learned":true},` + keys + `,"score":0.93}]}`,
			header:  []string{"SCORE", "LEARNED"},
			learned: map[string]string{"rotate-keys": "yes", "api-keys": ""},
		},
		{
			name:   "a body without the learned field",
			body:   `{"semantic":false,"results":[` + keys + `},` + entry + `]}`,
			header: []string{"SNIPPET", "URL"},
		},
		{
			name:   "no learned hit",
			body:   `{"semantic":true,"results":[` + keys + `,"score":0.93,"learned":false}]}`,
			header: []string{"URL", "SCORE"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeAPI{body: tt.body}
			out, errOut, code := run(t, f, []string{"search", "keys"}, runOpts{term: true})
			if code != 0 {
				t.Fatalf("exit %d: %s", code, errOut)
			}
			if q := lastReq(t, f).Query; q != "limit=10&q=keys" {
				t.Errorf("query = %q, want no new parameter", q)
			}
			lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
			header := strings.Fields(lines[0])
			if got := header[len(header)-len(tt.header):]; strings.Join(got, " ") != strings.Join(tt.header, " ") {
				t.Errorf("header ends with %q, want %q\n%s", got, tt.header, out)
			}
			if tt.learned == nil {
				if strings.Contains(out, "LEARNED") {
					t.Errorf("no hit is learned, so the table has no LEARNED column:\n%s", out)
				}
			} else {
				at := strings.Index(lines[0], "LEARNED")
				if at < 0 {
					t.Fatalf("a hit is learned, so the table needs a LEARNED column:\n%s", out)
				}
				for _, line := range lines[1:] {
					ref := strings.Fields(line)[0]
					want, ok := tt.learned[ref]
					if !ok {
						t.Errorf("unexpected row %q", line)
						continue
					}
					got := ""
					if len(line) > at {
						got = strings.TrimSpace(line[at:])
					}
					if got != want {
						t.Errorf("LEARNED cell of %s = %q, want %q\n%s", ref, got, want, out)
					}
				}
				if len(lines)-1 != len(tt.learned) {
					t.Errorf("table has %d rows, want %d\n%s", len(lines)-1, len(tt.learned), out)
				}
			}

			for _, c := range []struct {
				args []string
				opts runOpts
			}{
				{[]string{"search", "keys"}, runOpts{}},
				{[]string{"search", "keys", "--json"}, runOpts{term: true}},
			} {
				got, errOut, code := run(t, f, c.args, c.opts)
				if code != 0 {
					t.Fatalf("%v: exit %d: %s", c.args, code, errOut)
				}
				if want := compactJSON(t, tt.body) + "\n"; got != want {
					t.Errorf("%v terminal=%v: stdout = %q, want the compact response %q", c.args, c.opts.term, got, want)
				}
			}
		})
	}
}
