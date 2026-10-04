package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"
)

type outlineItem struct {
	Level  int    `json:"level"`
	Text   string `json:"text"`
	Anchor string `json:"anchor"`
}

// articleDoc holds the fields of the getArticle object that the text format
// prints. Truncated and NextOffset come only from MCP reads, but the format
// is the same for both.
type articleDoc struct {
	Slug           string        `json:"slug"`
	Title          string        `json:"title"`
	Status         string        `json:"status"`
	Visibility     string        `json:"visibility"`
	Section        string        `json:"section"`
	Nav            string        `json:"nav"`
	UpdatedAt      time.Time     `json:"updatedAt"`
	Revision       string        `json:"revision"`
	URL            string        `json:"url"`
	Source         string        `json:"source"`
	SectionAnchor  string        `json:"sectionAnchor"`
	RedirectedFrom string        `json:"redirectedFrom"`
	Truncated      bool          `json:"truncated"`
	NextOffset     int           `json:"nextOffset"`
	Outline        []outlineItem `json:"outline"`
	Markdown       string        `json:"markdown"`
}

// writeArticleDoc prints an article in the get_article text format of the
// MCP server: YAML front matter, then the Markdown body.
func writeArticleDoc(w io.Writer, d articleDoc) error {
	var fm frontMatter
	fm.add("slug", d.Slug)
	fm.add("title", jsonText(d.Title))
	fm.add("status", d.Status)
	fm.add("visibility", d.Visibility)
	fm.add("section", jsonText(d.Section))
	fm.add("nav", d.Nav)
	fm.add("updated_at", rfc3339(d.UpdatedAt))
	fm.add("revision", d.Revision)
	fm.add("url", d.URL)
	if d.Source != "" {
		fm.add("source", d.Source)
	}
	if d.SectionAnchor != "" {
		fm.add("section_anchor", d.SectionAnchor)
	}
	if d.RedirectedFrom != "" {
		fm.add("redirected_from", d.RedirectedFrom)
	}
	if d.Truncated {
		fm.add("truncated", "true")
		fm.add("next_offset", strconv.Itoa(d.NextOffset))
	}
	anchors := make([]string, 0, len(d.Outline))
	for _, o := range d.Outline {
		anchors = append(anchors, o.Anchor)
	}
	fm.add("anchors", jsonTextList(anchors))
	return fm.write(w, d.Markdown)
}

type changelogEntry struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Markdown    string     `json:"markdown"`
	Tags        []string   `json:"tags"`
	Status      string     `json:"status"`
	PublishedAt *time.Time `json:"publishedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	URL         string     `json:"url"`
}

// writeChangelogDoc prints a changelog entry in the same style as an
// article: YAML front matter, then the Markdown body.
func writeChangelogDoc(w io.Writer, e changelogEntry) error {
	var fm frontMatter
	fm.add("id", strconv.FormatInt(e.ID, 10))
	fm.add("title", jsonText(e.Title))
	fm.add("status", e.Status)
	fm.add("tags", jsonTextList(e.Tags))
	published := ""
	if e.PublishedAt != nil {
		published = rfc3339(*e.PublishedAt)
	}
	fm.add("published_at", published)
	fm.add("updated_at", rfc3339(e.UpdatedAt))
	fm.add("url", e.URL)
	return fm.write(w, e.Markdown)
}

type frontMatter struct {
	b strings.Builder
}

func (f *frontMatter) add(key, value string) {
	f.b.WriteString(key)
	f.b.WriteByte(':')
	if value != "" {
		f.b.WriteByte(' ')
		f.b.WriteString(value)
	}
	f.b.WriteByte('\n')
}

func (f *frontMatter) write(w io.Writer, markdown string) error {
	var out strings.Builder
	out.WriteString("---\n")
	out.WriteString(f.b.String())
	out.WriteString("---\n")
	out.WriteString(markdown)
	if markdown != "" && !strings.HasSuffix(markdown, "\n") {
		out.WriteByte('\n')
	}
	_, err := io.WriteString(w, out.String())
	return err
}

// jsonText encodes a string as JSON without HTML escapes, so titles with
// spaces, colons or quotes stay valid YAML and readable.
func jsonText(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(buf.String(), "\n")
}

func jsonTextList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, s := range items {
		quoted = append(quoted, jsonText(s))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
