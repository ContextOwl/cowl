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
			raw, err := a.request("GET", a.ws()+"/openapi", nil, nil)
			if err != nil {
				return err
			}
			return a.printJSON(raw)
		},
	}
}

func cmdOpenAPIAttach() *Command {
	var specURL, file string
	return &Command{
		Group: "openapi", Name: "attach", OpIDs: []string{"attachOpenAPI"},
		Summary: "Attach an OpenAPI spec (by URL or file) and generate reference pages",
		Usage:   "cowl openapi attach --url URL | --file spec.json|-",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&specURL, "url", "", "public URL of the spec")
			fs.StringVar(&file, "file", "", "spec file (JSON or YAML), - for stdin")
		},
		Run: func(a *App, args []string) error {
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
			raw, err := a.request("PUT", a.ws()+"/openapi", nil, body)
			if err != nil {
				return err
			}
			return a.printJSON(raw)
		},
	}
}

func cmdOpenAPISync() *Command {
	return &Command{
		Group: "openapi", Name: "sync", OpIDs: []string{"syncOpenAPI"},
		Summary: "Regenerate pages from the attached spec",
		Usage:   "cowl openapi sync [-w WORKSPACE]",
		Run: func(a *App, args []string) error {
			raw, err := a.request("POST", a.ws()+"/openapi/sync", nil, nil)
			if err != nil {
				return err
			}
			return a.printJSON(raw)
		},
	}
}

func cmdOpenAPIDetach() *Command {
	var yes bool
	return &Command{
		Group: "openapi", Name: "detach", OpIDs: []string{"detachOpenAPI"},
		Summary: "Detach the OpenAPI spec (generated pages become editable)",
		Usage:   "cowl openapi detach [--yes]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "skip the confirmation prompt")
		},
		Run: func(a *App, args []string) error {
			if err := a.confirm("detach the OpenAPI spec from workspace "+a.workspace, yes); err != nil {
				return err
			}
			raw, err := a.request("DELETE", a.ws()+"/openapi", nil, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var res struct {
				Removed int `json:"removed"`
			}
			if err := json.Unmarshal(raw, &res); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "detached OpenAPI spec (%d generated pages removed)\n", res.Removed)
			return nil
		},
	}
}

func cmdOpenAPIPages() *Command {
	return &Command{
		Group: "openapi", Name: "pages", OpIDs: []string{"listOpenAPIPages"},
		Summary: "List generated OpenAPI pages and their sections",
		Usage:   "cowl openapi pages [-w WORKSPACE]",
		Run: func(a *App, args []string) error {
			raw, err := a.request("GET", a.ws()+"/openapi/pages", nil, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var layout struct {
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
			if err := json.Unmarshal(raw, &layout); err != nil {
				return err
			}
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
		},
	}
}

func cmdOpenAPICreateSection() *Command {
	return &Command{
		Group: "openapi", Name: "create-section", OpIDs: []string{"createOpenAPISection"},
		Summary: "Create a nav section for generated OpenAPI pages",
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
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var ref struct {
				Key   string `json:"key"`
				Label string `json:"label"`
			}
			if err := json.Unmarshal(raw, &ref); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "created section %s (%q)\n", ref.Key, ref.Label)
			return nil
		},
	}
}

func cmdOpenAPIPlace() *Command {
	var section string
	var position int
	return &Command{
		Group: "openapi", Name: "place", OpIDs: []string{"placeOpenAPIPage"},
		Summary: "Move a generated OpenAPI page within a section",
		Usage:   "cowl openapi place SLUG --section SECTION_KEY [--position N]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&section, "section", "", "target section key (required)")
			fs.IntVar(&position, "position", 0, "position within the section (0-based)")
		},
		Run: func(a *App, args []string) error {
			if len(args) != 1 {
				return usageError("expected exactly one SLUG argument")
			}
			if section == "" {
				return usageError("--section is required")
			}
			body := map[string]any{"section": section}
			if a.flagWasSet("position") {
				body["position"] = position
			}
			raw, err := a.request("POST", a.ws()+"/openapi/pages/"+url.PathEscape(args[0])+"/placement", nil, body)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			fmt.Fprintf(a.Out, "placed page %s in %s\n", args[0], section)
			return nil
		},
	}
}

func cmdOpenAPIDetachPage() *Command {
	return &Command{
		Group: "openapi", Name: "detach-page", OpIDs: []string{"detachOpenAPIPage"},
		Summary: "Detach one generated page so it can be edited",
		Usage:   "cowl openapi detach-page SLUG",
		Run: func(a *App, args []string) error {
			if len(args) != 1 {
				return usageError("expected exactly one SLUG argument")
			}
			raw, err := a.request("DELETE", a.ws()+"/openapi/pages/"+url.PathEscape(args[0]), nil, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			fmt.Fprintf(a.Out, "detached page %s (now editable)\n", args[0])
			return nil
		},
	}
}
