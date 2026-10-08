package cli

import (
	"fmt"
	"strconv"
)

type sectionRow struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Visibility   string `json:"visibility"`
	ArticleCount int    `json:"articleCount"`
}

func cmdSectionsList() *Command {
	return &Command{
		Group: "sections", Name: "list", OpIDs: []string{"listSections"},
		Summary: "List the sidebar sections in sidebar order",
		Usage:   "cowl sections list [-w WORKSPACE]",
		Notes: draftRuleNote + `

For a key that reads no drafts, the ARTICLES column counts published
articles only, and so does articleCount in the JSON output. A section that
holds only drafts shows 0.`,
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			raw, err := a.request("GET", a.ws()+"/sections", nil, nil)
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(rows []sectionRow) error {
				out := make([][]string, 0, len(rows))
				for _, r := range rows {
					out = append(out, []string{r.Key, r.Label, dash(r.Visibility), strconv.Itoa(r.ArticleCount)})
				}
				a.table([]string{"KEY", "LABEL", "VISIBILITY", "ARTICLES"}, out)
				return nil
			})
		},
	}
}

// sectionRef is the response of createSection and createOpenAPISection.
// Both are idempotent by label: Created is false when the label exists.
type sectionRef struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Created *bool  `json:"created"`
}

func (a *App) printSectionRef(raw []byte) error {
	return emitAs(a, raw, func(ref sectionRef) error {
		if ref.Created != nil && !*ref.Created {
			fmt.Fprintf(a.Out, "section %s (%q) already exists\n", ref.Key, ref.Label)
			return nil
		}
		fmt.Fprintf(a.Out, "created section %s (%q)\n", ref.Key, ref.Label)
		return nil
	})
}

func cmdSectionsCreate() *Command {
	return &Command{
		Group: "sections", Name: "create", OpIDs: []string{"createSection"},
		Summary: "Create a sidebar section, or return the one with the same label",
		Usage:   "cowl sections create LABEL",
		Run: func(a *App, args []string) error {
			label, err := oneArg(args, "LABEL")
			if err != nil {
				return err
			}
			raw, err := a.request("POST", a.ws()+"/sections", nil, map[string]any{"label": label})
			if err != nil {
				return err
			}
			return a.printSectionRef(raw)
		},
	}
}
