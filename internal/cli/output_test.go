package cli

import (
	"bytes"
	"testing"
)

// Server text can hold control characters. A table cell and a field value
// drop each one that is not whitespace. Whitespace collapses to one space, so
// each row stays on one line.
func TestCellsDropControlCharacters(t *testing.T) {
	var out bytes.Buffer
	a := &App{IO: IO{Out: &out}}
	a.table([]string{"QUERY", "SEARCHES"}, [][]string{
		{"sso\x1b with\a okta", "9"},
		{"rotate \x1b a\u009b key\x7f\b", "7"},
		{"tabs\tand\nlines\u0085here", "1"},
	})
	a.fields([][2]string{{"name:", "Docs\x1b\a team"}})
	want := "QUERY                SEARCHES\n" +
		"sso with okta        9\n" +
		"rotate a key         7\n" +
		"tabs and lines here  1\n" +
		"name:  Docs team\n"
	if got := out.String(); got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}
