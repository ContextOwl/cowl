package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type changelogRow struct {
	ID          int64      `json:"id"`
	Title       string     `json:"title"`
	Tags        []string   `json:"tags"`
	Status      string     `json:"status"`
	PublishedAt *time.Time `json:"publishedAt"`
}

func changelogIDArg(args []string) (string, error) {
	if len(args) != 1 {
		return "", usageError("expected exactly one ID argument")
	}
	if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
		return "", usageError("ID must be an integer")
	}
	return args[0], nil
}

func cmdChangelogList() *Command {
	var drafts bool
	return &Command{
		Group: "changelog", Name: "list", OpIDs: []string{"listChangelog"},
		Summary: "List changelog entries",
		Usage:   "cowl changelog list [--drafts]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&drafts, "drafts", false, "include drafts (needs changelog.update)")
		},
		Run: func(a *App, args []string) error {
			q := url.Values{}
			if drafts {
				q.Set("drafts", "true")
			}
			raw, err := a.request("GET", a.ws()+"/changelog", q, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var rows []changelogRow
			if err := json.Unmarshal(raw, &rows); err != nil {
				return err
			}
			out := make([][]string, 0, len(rows))
			for _, r := range rows {
				var pub time.Time
				if r.PublishedAt != nil {
					pub = *r.PublishedAt
				}
				out = append(out, []string{
					strconv.FormatInt(r.ID, 10), r.Title, r.Status,
					dash(strings.Join(r.Tags, ",")), timeLabel(pub),
				})
			}
			a.table([]string{"ID", "TITLE", "STATUS", "TAGS", "PUBLISHED"}, out)
			return nil
		},
	}
}

func cmdChangelogCreate() *Command {
	var opts struct{ title, file, markdown, tags, status, publishedAt string }
	return &Command{
		Group: "changelog", Name: "create", OpIDs: []string{"createChangelog"},
		Summary: "Create a changelog entry (publishing needs changelog.publish)",
		Usage:   "cowl changelog create --title TITLE [--file FILE|-] [--markdown TEXT] [--tags a,b] [--status draft|published] [--published-at RFC3339]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.title, "title", "", "entry title (required)")
			fs.StringVar(&opts.file, "file", "", "markdown file, - for stdin")
			fs.StringVar(&opts.markdown, "markdown", "", "inline markdown content")
			fs.StringVar(&opts.tags, "tags", "", "comma-separated tags")
			fs.StringVar(&opts.status, "status", "", "draft or published")
			fs.StringVar(&opts.publishedAt, "published-at", "", "publish timestamp (RFC3339)")
		},
		Run: func(a *App, args []string) error {
			if opts.title == "" {
				return usageError("--title is required")
			}
			content, hasContent, err := a.contentFrom(opts.file, opts.markdown, a.flagWasSet("markdown"))
			if err != nil {
				return err
			}
			body := map[string]any{"title": opts.title}
			if hasContent {
				body["markdown"] = content
			}
			if a.flagWasSet("tags") {
				body["tags"] = csv(opts.tags)
			}
			if opts.status != "" {
				body["status"] = opts.status
			}
			if opts.publishedAt != "" {
				body["published_at"] = opts.publishedAt
			}
			raw, err := a.request("POST", a.ws()+"/changelog", nil, body)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var entry changelogRow
			if err := json.Unmarshal(raw, &entry); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "created changelog entry %d (%s)\n", entry.ID, entry.Status)
			return nil
		},
	}
}

func cmdChangelogUpdate() *Command {
	var opts struct{ title, file, markdown, tags, status, publishedAt string }
	return &Command{
		Group: "changelog", Name: "update", OpIDs: []string{"updateChangelog"},
		Summary: "Update a changelog entry (changing --status needs changelog.publish)",
		Usage:   "cowl changelog update ID [--title T] [--file FILE|-] [--markdown TEXT] [--tags a,b] [--status S] [--published-at RFC3339]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.title, "title", "", "new title")
			fs.StringVar(&opts.file, "file", "", "markdown file, - for stdin")
			fs.StringVar(&opts.markdown, "markdown", "", "inline markdown content")
			fs.StringVar(&opts.tags, "tags", "", "comma-separated tags (replaces the set)")
			fs.StringVar(&opts.status, "status", "", "draft or published")
			fs.StringVar(&opts.publishedAt, "published-at", "", "publish timestamp (RFC3339)")
		},
		Run: func(a *App, args []string) error {
			id, err := changelogIDArg(args)
			if err != nil {
				return err
			}
			content, hasContent, err := a.contentFrom(opts.file, opts.markdown, a.flagWasSet("markdown"))
			if err != nil {
				return err
			}
			body := map[string]any{}
			if a.flagWasSet("title") {
				body["title"] = opts.title
			}
			if hasContent {
				body["markdown"] = content
			}
			if a.flagWasSet("tags") {
				body["tags"] = csv(opts.tags)
			}
			if a.flagWasSet("status") {
				body["status"] = opts.status
			}
			if a.flagWasSet("published-at") {
				body["published_at"] = opts.publishedAt
			}
			if len(body) == 0 {
				return usageError("nothing to update: pass at least one field flag")
			}
			raw, err := a.request("PATCH", a.ws()+"/changelog/"+id, nil, body)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			fmt.Fprintf(a.Out, "updated changelog entry %s\n", id)
			return nil
		},
	}
}

func cmdChangelogDelete() *Command {
	var yes bool
	return &Command{
		Group: "changelog", Name: "delete", OpIDs: []string{"deleteChangelog"},
		Summary: "Delete a changelog entry",
		Usage:   "cowl changelog delete ID [--yes]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "skip the confirmation prompt")
		},
		Run: func(a *App, args []string) error {
			id, err := changelogIDArg(args)
			if err != nil {
				return err
			}
			if err := a.confirm("delete changelog entry "+id, yes); err != nil {
				return err
			}
			raw, err := a.request("DELETE", a.ws()+"/changelog/"+id, nil, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			fmt.Fprintf(a.Out, "deleted changelog entry %s\n", id)
			return nil
		},
	}
}
