package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"strconv"
)

type searchRow struct {
	Type    string  `json:"type"`
	Slug    string  `json:"slug"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score"`
}

func cmdSearch() *Command {
	var semantic bool
	var limit int
	return &Command{
		Name: "search", OpIDs: []string{"searchDocs"},
		Summary: "Search a workspace's docs (full-text, or --semantic)",
		Usage:   "cowl search QUERY [-w WORKSPACE] [--semantic] [--limit N]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&semantic, "semantic", false, "rank by embedding similarity (falls back to full-text)")
			fs.IntVar(&limit, "limit", 10, "max results for semantic search (max 50)")
		},
		Run: func(a *App, args []string) error {
			query := joinArgs(args)
			if query == "" {
				return usageError("QUERY is required")
			}
			q := url.Values{"q": {query}}
			if semantic {
				q.Set("semantic", "true")
				q.Set("limit", strconv.Itoa(limit))
			}
			raw, err := a.request("GET", a.ws()+"/search", q, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var rows []searchRow
			usedSemantic := false
			if semantic {
				var resp struct {
					Semantic bool        `json:"semantic"`
					Results  []searchRow `json:"results"`
				}
				if err := json.Unmarshal(raw, &resp); err != nil {
					return err
				}
				rows, usedSemantic = resp.Results, resp.Semantic
			} else if err := json.Unmarshal(raw, &rows); err != nil {
				return err
			}
			// The server only takes limit for semantic search; honor the flag
			// on the full-text path here.
			if !semantic && a.flagWasSet("limit") && limit > 0 && len(rows) > limit {
				rows = rows[:limit]
			}
			if len(rows) == 0 {
				fmt.Fprintln(a.Out, "no results")
				return nil
			}
			headers := []string{"SLUG", "TYPE", "TITLE", "SNIPPET"}
			if usedSemantic {
				headers = append(headers, "SCORE")
			}
			out := make([][]string, 0, len(rows))
			for _, r := range rows {
				row := []string{r.Slug, r.Type, r.Title, dash(snippetText(r.Snippet))}
				if usedSemantic {
					row = append(row, strconv.FormatFloat(r.Score, 'f', 3, 64))
				}
				out = append(out, row)
			}
			a.table(headers, out)
			if semantic && !usedSemantic {
				fmt.Fprintln(a.Err, "note: semantic ranking unavailable; showing full-text results")
			}
			return nil
		},
	}
}
