package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// validArticleStatuses mirrors store.ValidStatuses so the CLI rejects a typo
// before it sends a request.
var validArticleStatuses = map[string]bool{
	"DRAFT": true, "IN REVIEW": true, "BETA": true, "STABLE": true, "DEPRECATED": true,
}

const articleStatusList = "DRAFT, IN REVIEW, BETA, STABLE, DEPRECATED"

// draftRuleNote is the help text of the read commands that says which keys
// read drafts.
const draftRuleNote = `The org setting Published pages only limits draft reads. When it is on, only
a key that can use article.create, article.update, article.propose,
article.publish or article.place reads DRAFT and IN REVIEW articles. Run
cowl whoami to see if the key reads drafts.`

const publishedOnlyFlagHelp = "leave out DRAFT and IN REVIEW articles. A key that reads no drafts never gets them"

var validArticleVisibilities = map[string]bool{
	"public": true, "internal": true, "private": true,
}

type articleRow struct {
	Slug       string    `json:"slug"`
	Title      string    `json:"title"`
	Section    string    `json:"section"`
	Nav        string    `json:"nav"`
	Status     string    `json:"status"`
	Encrypted  bool      `json:"encrypted"`
	Visibility string    `json:"visibility"`
	UpdatedAt  time.Time `json:"updatedAt"`
	URL        string    `json:"url"`
}

// normalizeStatus upper-cases an article status and collapses its spaces, so
// "in  review" becomes "IN REVIEW".
func normalizeStatus(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), " "))
}

// statusFilter normalizes a comma-separated --status value for the
// listArticles filter.
func statusFilter(s string) (string, error) {
	parts := csv(s)
	for i, p := range parts {
		p = normalizeStatus(p)
		if !validArticleStatuses[p] {
			return "", usageError("--status takes a comma-separated list of " + articleStatusList + ", got " + parts[i])
		}
		parts[i] = p
	}
	return strings.Join(parts, ","), nil
}

func cmdArticlesList() *Command {
	var opts struct {
		status, nav, updatedSince string
		publishedOnly             bool
	}
	return &Command{
		Group: "articles", Name: "list", OpIDs: []string{"listArticles"},
		Summary: "List articles in sidebar order, or newest first with --updated-since",
		Usage:   "cowl articles list [--status S1,S2] [--nav KEY|none] [--updated-since 7d] [--published-only]",
		Notes:   draftRuleNote,
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.status, "status", "", "only these statuses, comma-separated: "+articleStatusList+
				". A key that reads no drafts gets no DRAFT or IN REVIEW rows")
			fs.StringVar(&opts.nav, "nav", "", "only articles in this section key, or none for unplaced articles")
			fs.StringVar(&opts.updatedSince, "updated-since", "", "only articles updated since a time: RFC 3339, YYYY-MM-DD, or a duration such as 7d or 24h")
			fs.BoolVar(&opts.publishedOnly, "published-only", false, publishedOnlyFlagHelp)
		},
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			q := url.Values{}
			if opts.status != "" {
				status, err := statusFilter(opts.status)
				if err != nil {
					return err
				}
				q.Set("status", status)
			}
			if opts.nav != "" {
				q.Set("nav", opts.nav)
			}
			if opts.updatedSince != "" {
				q.Set("updated_since", opts.updatedSince)
			}
			if opts.publishedOnly {
				q.Set("published_only", "true")
			}
			raw, err := a.request("GET", a.ws()+"/articles", q, nil)
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(rows []articleRow) error {
				out := make([][]string, 0, len(rows))
				for _, r := range rows {
					out = append(out, []string{r.Slug, r.Title, dash(r.Section), dash(r.Nav), r.Status, yesNo(r.Encrypted), timeLabel(r.UpdatedAt), dash(r.URL)})
				}
				a.table([]string{"SLUG", "TITLE", "SECTION", "NAV", "STATUS", "ENCRYPTED", "UPDATED", "URL"}, out)
				return nil
			})
		},
	}
}

func cmdArticlesGet() *Command {
	var section string
	return &Command{
		Group: "articles", Name: "get", OpIDs: []string{"getArticle"},
		Summary: "Print articles as front matter and Markdown (--json for the API objects)",
		Usage:   "cowl articles get SLUG... [--section ANCHOR]",
		Notes: draftRuleNote + `

For a key that reads no drafts, a DRAFT or IN REVIEW article is not found,
and so is a slug that redirects to one.`,
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&section, "section", "", "print only the heading with this anchor and its text (one SLUG only)")
		},
		Run: func(a *App, args []string) error {
			if len(args) == 0 {
				return usageError("expected at least one SLUG argument")
			}
			if section != "" && len(args) > 1 {
				return usageError("--section works with one SLUG only")
			}
			q := url.Values{}
			if section != "" {
				q.Set("section", section)
			}
			raws := make([]json.RawMessage, 0, len(args))
			for _, slug := range args {
				raw, err := a.request("GET", a.ws()+"/articles/"+url.PathEscape(slug), q, nil)
				if err != nil {
					return err
				}
				raws = append(raws, raw)
			}
			if a.g.jsonOut {
				if len(raws) == 1 {
					return a.printJSON(raws[0])
				}
				return a.printJSON(joinJSONArray(raws))
			}
			var buf bytes.Buffer
			for i, raw := range raws {
				var doc articleDoc
				if err := json.Unmarshal(raw, &doc); err != nil {
					return invalidResponse(err)
				}
				if i > 0 {
					buf.WriteByte('\n')
				}
				if err := writeArticleDoc(&buf, doc); err != nil {
					return err
				}
			}
			_, err := a.Out.Write(buf.Bytes())
			return err
		},
	}
}

func cmdArticlesCreate() *Command {
	var opts struct{ title, slug, section, sectionKey, file, markdown string }
	return &Command{
		Group: "articles", Name: "create", OpIDs: []string{"createArticle"},
		Summary: "Create a draft article",
		Usage:   "cowl articles create --title TITLE [--slug SLUG] [--section-key KEY] [--section LABEL] [--file FILE|- | --markdown TEXT]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.title, "title", "", "article title (required)")
			fs.StringVar(&opts.slug, "slug", "", "slug for the article. The create fails with slug_taken when it exists (default: made from the title)")
			fs.StringVar(&opts.sectionKey, "section-key", "", "sidebar section key to place the article in (see 'cowl sections list')")
			fs.StringVar(&opts.section, "section", "", "section label shown on the article. It does not place the article: use --section-key")
			fs.StringVar(&opts.file, "file", "", "markdown file, - for stdin")
			fs.StringVar(&opts.markdown, "markdown", "", "inline markdown content")
		},
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			if opts.title == "" {
				return usageError("--title is required")
			}
			content, hasContent, err := a.contentFrom(opts.file, opts.markdown, a.flagWasSet("markdown"))
			if err != nil {
				return err
			}
			body := map[string]any{"title": opts.title}
			if opts.slug != "" {
				body["slug"] = opts.slug
			}
			if opts.section != "" {
				body["section"] = opts.section
			}
			if opts.sectionKey != "" {
				body["section_key"] = opts.sectionKey
			}
			if hasContent {
				body["markdown"] = content
			}
			raw, err := a.request("POST", a.ws()+"/articles", nil, body)
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(ref articleWrite) error {
				fmt.Fprintf(a.Out, "created article %s (%s)%s\n", ref.Slug, ref.details(), urlSuffix(ref.URL))
				return nil
			})
		},
	}
}

// articleWrite is the response of createArticle and updateArticle.
type articleWrite struct {
	Slug             string   `json:"slug"`
	Status           string   `json:"status"`
	Nav              string   `json:"nav"`
	URL              string   `json:"url"`
	Revision         string   `json:"revision"`
	PreviousRevision string   `json:"previousRevision"`
	Changed          []string `json:"changed"`
	Placed           bool     `json:"placed"`
}

func (w articleWrite) details() string {
	parts := []string{dash(w.Status)}
	if w.Nav != "" {
		parts = append(parts, "section "+w.Nav)
	}
	if w.Revision != "" {
		parts = append(parts, "revision "+w.Revision)
	}
	return strings.Join(parts, ", ")
}

func urlSuffix(u string) string {
	if u == "" {
		return ""
	}
	return " " + u
}

func cmdArticlesUpdate() *Command {
	var opts struct {
		title, section, status, visibility, file, markdown, edits, baseRevision string
		allowShrink                                                             bool
	}
	return &Command{
		Group: "articles", Name: "update", OpIDs: []string{"updateArticle"},
		Summary: "Update an article directly (changing --status needs article.publish)",
		Usage:   "cowl articles update SLUG [--title T] [--section LABEL] [--status STATUS] [--visibility TIER] [--file FILE|- | --markdown TEXT | --edits FILE|-] [--base-revision REV] [--allow-shrink]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.title, "title", "", "new title")
			fs.StringVar(&opts.section, "section", "", "new section label shown on the article")
			fs.StringVar(&opts.status, "status", "", articleStatusList)
			fs.StringVar(&opts.visibility, "visibility", "", "public, internal, or private")
			fs.StringVar(&opts.file, "file", "", "new markdown body from a file, - for stdin")
			fs.StringVar(&opts.markdown, "markdown", "", "new inline markdown body")
			fs.StringVar(&opts.edits, "edits", "", `JSON file with 1 to 20 exact text edits [{"old":"...","new":"..."}], - for stdin`)
			fs.StringVar(&opts.baseRevision, "base-revision", "", "revision you read. The update fails with stale_revision when the article changed since then")
			fs.BoolVar(&opts.allowShrink, "allow-shrink", false, "allow a new body that removes more than half of the text")
		},
		Run: func(a *App, args []string) error {
			slug, err := oneArg(args, "SLUG")
			if err != nil {
				return err
			}
			body := map[string]any{}
			if a.flagWasSet("title") {
				body["title"] = opts.title
			}
			if a.flagWasSet("section") {
				body["section"] = opts.section
			}
			if a.flagWasSet("status") {
				status := normalizeStatus(opts.status)
				if !validArticleStatuses[status] {
					return usageError("--status must be one of " + articleStatusList + ", got " + opts.status)
				}
				body["status"] = status
			}
			if a.flagWasSet("visibility") {
				if !validArticleVisibilities[opts.visibility] {
					return usageError("--visibility must be public, internal, or private")
				}
				body["visibility"] = opts.visibility
			}
			if err := a.bodyContent(body, opts.file, opts.markdown, opts.edits); err != nil {
				return err
			}
			if len(body) == 0 {
				return usageError("nothing to update: pass at least one of --title, --section, --status, --visibility, --file, --markdown, --edits")
			}
			if err := a.revisionFlags(body, opts.baseRevision, opts.allowShrink); err != nil {
				return err
			}
			raw, err := a.request("PATCH", a.ws()+"/articles/"+url.PathEscape(slug), nil, body)
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(res articleWrite) error {
				if len(res.Changed) == 0 && !res.Placed {
					fmt.Fprintf(a.Out, "article %s is unchanged (revision %s)\n", slug, dash(res.Revision))
					return nil
				}
				parts := []string{}
				if len(res.Changed) > 0 {
					parts = append(parts, "changed "+strings.Join(res.Changed, ", "))
				}
				if res.Placed {
					parts = append(parts, "placed in section "+dash(res.Nav))
				}
				fmt.Fprintf(a.Out, "updated article %s (%s) revision %s%s\n", slug, strings.Join(parts, ", "), dash(res.Revision), urlSuffix(res.URL))
				return nil
			})
		},
	}
}

func cmdArticlesPlace() *Command {
	var section string
	var position int
	return &Command{
		Group: "articles", Name: "place", OpIDs: []string{"placeArticle"},
		Summary: "Move an article into a sidebar section",
		Usage:   "cowl articles place SLUG --section SECTION_KEY [--position N]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&section, "section", "", "target section key, or none to unplace (required)")
			fs.IntVar(&position, "position", 0, "0-based position in the section (default: the end)")
		},
		Run: func(a *App, args []string) error {
			slug, err := oneArg(args, "SLUG")
			if err != nil {
				return err
			}
			if section == "" {
				return usageError("--section is required")
			}
			body := map[string]any{"section": section}
			if a.flagWasSet("position") {
				if position < 0 {
					return usageError("--position must be 0 or more")
				}
				body["position"] = position
			}
			raw, err := a.request("POST", a.ws()+"/articles/"+url.PathEscape(slug)+"/placement", nil, body)
			if err != nil {
				return err
			}
			return a.emit(raw, func() error {
				where := ""
				if a.flagWasSet("position") {
					where = " at position " + strconv.Itoa(position)
				}
				fmt.Fprintf(a.Out, "placed article %s in %s%s\n", slug, section, where)
				return nil
			})
		},
	}
}
