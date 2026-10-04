package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWriteArticleDoc(t *testing.T) {
	updated := time.Date(2026, 9, 30, 16, 2, 11, 500, time.FixedZone("CEST", 2*3600))
	base := articleDoc{
		Slug: "api-keys", Title: "Agent Keys", Status: "STABLE", Visibility: "public", Section: "Reference", Nav: "reference",
		UpdatedAt: updated, Revision: "8f3a2c1b9d0e", URL: "https://developer.contextowl.co/docs/platform/api-keys",
		Outline: []outlineItem{
			{Level: 2, Text: "Create a key", Anchor: "create-a-key"},
			{Level: 2, Text: "Scope model", Anchor: "scope-model"},
			{Level: 3, Text: "Rotation", Anchor: "rotation"},
		},
		Markdown: "# Agent Keys\n...",
	}
	const head = "---\n" +
		"slug: api-keys\n" +
		"title: \"Agent Keys\"\n" +
		"status: STABLE\n" +
		"visibility: public\n" +
		"section: \"Reference\"\n" +
		"nav: reference\n" +
		"updated_at: 2026-09-30T14:02:11Z\n" +
		"revision: 8f3a2c1b9d0e\n" +
		"url: https://developer.contextowl.co/docs/platform/api-keys\n"

	tests := []struct {
		name string
		edit func(d *articleDoc)
		want string
	}{
		{
			name: "contract example",
			edit: func(d *articleDoc) {},
			want: head + "anchors: [\"create-a-key\", \"scope-model\", \"rotation\"]\n---\n# Agent Keys\n...\n",
		},
		{
			name: "optional lines after url in contract order",
			edit: func(d *articleDoc) {
				d.Source, d.SectionAnchor, d.RedirectedFrom, d.Truncated, d.NextOffset = "openapi", "rotation", "old-keys", true, 50000
				d.Markdown = "## Rotation\n"
			},
			want: head + "source: openapi\nsection_anchor: rotation\nredirected_from: old-keys\ntruncated: true\nnext_offset: 50000\n" +
				"anchors: [\"create-a-key\", \"scope-model\", \"rotation\"]\n---\n## Rotation\n",
		},
		{
			name: "strings with colons, quotes and HTML stay readable JSON",
			edit: func(d *articleDoc) {
				d.Title, d.Section, d.Outline, d.Markdown = `Q&A: "keys" <beta>`, "Ops: Runbooks", nil, ""
			},
			want: strings.Replace(strings.Replace(head, `title: "Agent Keys"`, `title: "Q&A: \"keys\" <beta>"`, 1), `section: "Reference"`, `section: "Ops: Runbooks"`, 1) +
				"anchors: []\n---\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := base
			tt.edit(&d)
			var b strings.Builder
			if err := writeArticleDoc(&b, d); err != nil {
				t.Fatal(err)
			}
			if b.String() != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", b.String(), tt.want)
			}
		})
	}
}

func TestWriteArticleDocFromRESTObject(t *testing.T) {
	var d articleDoc
	if err := json.Unmarshal([]byte(sectionJSON), &d); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := writeArticleDoc(&b, d); err != nil {
		t.Fatal(err)
	}
	want := "---\nslug: api-keys\ntitle: \"Agent Keys\"\nstatus: STABLE\nvisibility: public\nsection: \"Reference\"\nnav: reference\n" +
		"updated_at: 2026-09-30T14:02:11Z\nrevision: 8f3a2c1b9d0e\nurl: https://developer.contextowl.co/docs/platform/api-keys\n" +
		"section_anchor: rotation\nanchors: [\"create-a-key\", \"rotation\"]\n---\n## Rotation\n\nCreate a replacement key.\n"
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestWriteChangelogDoc(t *testing.T) {
	var e changelogEntry
	if err := json.Unmarshal([]byte(changelogJSON), &e); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := writeChangelogDoc(&b, e); err != nil {
		t.Fatal(err)
	}
	want := "---\nid: 42\ntitle: \"Release 2.4\"\nstatus: published\ntags: [\"new\", \"fixed\"]\npublished_at: 2026-09-20T10:00:00Z\n" +
		"updated_at: 2026-09-21T08:00:00Z\nurl: https://developer.contextowl.co/docs/platform/changelog#e42\n---\nKeys now expire after 90 days.\n"
	if b.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", b.String(), want)
	}

	draft := changelogEntry{ID: 1, Title: "v2", Status: "draft"}
	b.Reset()
	if err := writeChangelogDoc(&b, draft); err != nil {
		t.Fatal(err)
	}
	if want := "---\nid: 1\ntitle: \"v2\"\nstatus: draft\ntags: []\npublished_at:\nupdated_at:\nurl:\n---\n"; b.String() != want {
		t.Errorf("draft got:\n%q\nwant:\n%q", b.String(), want)
	}
}
