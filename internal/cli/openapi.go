package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
)

func cmdOpenAPIStatus() *Command {
	return &Command{
		Group: "openapi", Name: "status", OpIDs: []string{"getOpenAPIStatus"},
		Summary: "Show the attached OpenAPI spec status as JSON",
		Usage:   "cowl openapi status [-w WORKSPACE]",
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			raw, err := a.request("GET", a.ws()+"/openapi", nil, nil)
			if err != nil {
				return err
			}
			return a.printJSON(raw)
		},
	}
}

// openAPISpec is the --json output of 'cowl openapi spec'. The REST body is
// the stored spec, and a YAML spec is not JSON, so cowl wraps the text.
type openAPISpec struct {
	Format string `json:"format"`
	Spec   string `json:"spec"`
}

func cmdOpenAPISpec() *Command {
	return &Command{
		Group: "openapi", Name: "spec", OpIDs: []string{"getOpenAPISpec"},
		Summary: `Print the stored OpenAPI spec as it was attached. --json wraps it in {"format","spec"}`,
		Usage:   "cowl openapi spec [-w WORKSPACE] [--json]",
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			raw, err := a.request("GET", a.ws()+"/openapi/spec", nil, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				format := "yaml"
				if json.Valid(raw) {
					format = "json"
				}
				return a.printValue(openAPISpec{Format: format, Spec: string(raw)})
			}
			if _, err := a.Out.Write(raw); err != nil {
				return err
			}
			if a.OutTTY && len(raw) > 0 && raw[len(raw)-1] != '\n' {
				fmt.Fprintln(a.Out)
			}
			return nil
		},
	}
}

func cmdOpenAPIAttach() *Command {
	var specURL, file, note string
	return &Command{
		Group: "openapi", Name: "attach", OpIDs: []string{"attachOpenAPI"},
		Summary: "Attach an OpenAPI spec (by URL or file) and generate reference pages",
		Usage:   "cowl openapi attach --url URL | --file spec.json|- [--note NOTE]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&specURL, "url", "", "public URL of the spec")
			fs.StringVar(&file, "file", "", "spec file (JSON or YAML), - for stdin")
			fs.StringVar(&note, "note", "", noteHelp)
		},
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			if (specURL == "") == (file == "") {
				return usageError("pass exactly one of --url or --file")
			}
			body := map[string]any{}
			if specURL != "" {
				body["url"] = specURL
			} else {
				data, err := a.readFileArg(file)
				if err != nil {
					return err
				}
				body["spec"] = string(data)
			}
			raw, httpStatus, err := a.requestNote("PUT", a.ws()+"/openapi", body, note)
			if err != nil {
				return err
			}
			return a.emitWrite(raw, httpStatus, func() error { return a.printJSON(raw) })
		},
	}
}

func cmdOpenAPISync() *Command {
	var note string
	return &Command{
		Group: "openapi", Name: "sync", OpIDs: []string{"syncOpenAPI"},
		Summary: "Fetch the spec URL again and regenerate pages. A failed fetch keeps the stored spec",
		Usage:   "cowl openapi sync [-w WORKSPACE] [--note NOTE]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&note, "note", "", noteHelp)
		},
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			raw, httpStatus, err := a.requestStatus("POST", a.ws()+"/openapi/sync", noteQuery(note), nil)
			if err != nil {
				return err
			}
			return a.emitWrite(raw, httpStatus, func() error { return a.printJSON(raw) })
		},
	}
}

func cmdOpenAPIDetach() *Command {
	var yes bool
	var note string
	return &Command{
		Group: "openapi", Name: "detach", OpIDs: []string{"detachOpenAPI"},
		Summary: "Detach the OpenAPI spec and delete the pages it generated",
		Usage:   "cowl openapi detach [--yes] [--note NOTE]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "skip the confirmation prompt")
			fs.StringVar(&note, "note", "", noteHelp)
		},
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			if err := a.confirm("detach the OpenAPI spec from workspace "+a.workspace+" and delete its generated pages", yes); err != nil {
				return err
			}
			raw, httpStatus, err := a.requestStatus("DELETE", a.ws()+"/openapi", noteQuery(note), nil)
			if err != nil {
				return err
			}
			return emitWriteAs(a, raw, httpStatus, func(res struct {
				Removed int `json:"removed"`
			}) error {
				fmt.Fprintf(a.Out, "detached the OpenAPI spec and deleted %d generated pages\n", res.Removed)
				return nil
			})
		},
	}
}

type openAPILayout struct {
	Sections []struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	} `json:"sections"`
	Pages []struct {
		Slug    string `json:"slug"`
		Title   string `json:"title"`
		Method  string `json:"method"`
		Section string `json:"section"`
		Nav     string `json:"nav"`
	} `json:"pages"`
}

func cmdOpenAPIPages() *Command {
	return &Command{
		Group: "openapi", Name: "pages", OpIDs: []string{"listOpenAPIPages"},
		Summary: "List generated OpenAPI pages and their sections",
		Usage:   "cowl openapi pages [-w WORKSPACE]",
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			raw, err := a.request("GET", a.ws()+"/openapi/pages", nil, nil)
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(layout openAPILayout) error {
				if len(layout.Sections) > 0 {
					rows := make([][]string, 0, len(layout.Sections))
					for _, s := range layout.Sections {
						rows = append(rows, []string{s.Key, s.Label})
					}
					a.table([]string{"SECTION", "LABEL"}, rows)
					fmt.Fprintln(a.Out)
				}
				rows := make([][]string, 0, len(layout.Pages))
				for _, p := range layout.Pages {
					rows = append(rows, []string{p.Slug, dash(p.Method), p.Title, dash(p.Nav)})
				}
				a.table([]string{"SLUG", "METHOD", "TITLE", "NAV"}, rows)
				return nil
			})
		},
	}
}

func cmdOpenAPICreateSection() *Command {
	return &Command{
		Group: "openapi", Name: "create-section", OpIDs: []string{"createOpenAPISection"},
		Summary: "Create a sidebar section for generated OpenAPI pages, or return the one with the same label",
		Usage:   "cowl openapi create-section LABEL",
		Run: func(a *App, args []string) error {
			label, err := oneArg(args, "LABEL")
			if err != nil {
				return err
			}
			raw, err := a.request("POST", a.ws()+"/openapi/sections", nil, map[string]any{"label": label})
			if err != nil {
				return err
			}
			return a.printSectionRef(raw)
		},
	}
}

func cmdOpenAPIPlace() *Command {
	var section, note string
	var position int
	return &Command{
		Group: "openapi", Name: "place", OpIDs: []string{"placeOpenAPIPage"},
		Summary: "Move a generated OpenAPI page within a section",
		Usage:   "cowl openapi place SLUG --section SECTION_KEY [--position N] [--note NOTE]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&section, "section", "", "target section key (required)")
			fs.IntVar(&position, "position", 0, "0-based position in the section (default: the end)")
			fs.StringVar(&note, "note", "", noteHelp)
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
			raw, httpStatus, err := a.requestNote("POST", a.ws()+"/openapi/pages/"+url.PathEscape(slug)+"/placement", body, note)
			if err != nil {
				return err
			}
			return emitWriteAs(a, raw, httpStatus, func(page struct {
				Visibility string `json:"visibility"`
			}) error {
				a.printPlaced("page", slug, section, position, page.Visibility)
				return nil
			})
		},
	}
}

func cmdOpenAPIDetachPage() *Command {
	return &Command{
		Group: "openapi", Name: "detach-page", OpIDs: []string{"detachOpenAPIPage"},
		Summary: "Detach one generated page: it becomes a normal article and later syncs skip it",
		Usage:   "cowl openapi detach-page SLUG",
		Run: func(a *App, args []string) error {
			slug, err := oneArg(args, "SLUG")
			if err != nil {
				return err
			}
			raw, err := a.request("DELETE", a.ws()+"/openapi/pages/"+url.PathEscape(slug), nil, nil)
			if err != nil {
				return err
			}
			return a.emit(raw, func() error {
				fmt.Fprintf(a.Out, "detached page %s: it is now a normal article, and later syncs skip it\n", slug)
				return nil
			})
		},
	}
}
