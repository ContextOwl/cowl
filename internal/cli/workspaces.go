package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
)

type workspaceRow struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Color      string `json:"color"`
	AccessMode string `json:"accessMode"`
	LLMSTxt    bool   `json:"llmsTxt"`
}

// workspaceTarget picks the path segment for workspace-object commands:
// an explicit positional argument wins over -w/config.
func workspaceTarget(a *App, args []string) (string, error) {
	switch len(args) {
	case 0:
		return a.workspace, nil
	case 1:
		return args[0], nil
	}
	return "", usageError("expected at most one WORKSPACE argument")
}

func cmdWorkspacesList() *Command {
	return &Command{
		Group: "workspaces", Name: "list", OpIDs: []string{"listWorkspaces"},
		Summary: "List the organization's workspaces visible to this key",
		Usage:   "cowl workspaces list",
		Run: func(a *App, args []string) error {
			raw, err := a.request("GET", "/api/v1/workspaces", nil, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var rows []workspaceRow
			if err := json.Unmarshal(raw, &rows); err != nil {
				return err
			}
			out := make([][]string, 0, len(rows))
			for _, r := range rows {
				llms := ""
				if r.LLMSTxt {
					llms = "yes"
				}
				out = append(out, []string{r.ID, r.Name, r.AccessMode, llms, r.Color})
			}
			a.table([]string{"ID", "NAME", "ACCESS", "LLMS.TXT", "COLOR"}, out)
			return nil
		},
	}
}

func cmdWorkspacesCreate() *Command {
	var opts struct {
		color, accessMode string
		llmsTxt           bool
	}
	return &Command{
		Group: "workspaces", Name: "create", OpIDs: []string{"createWorkspace"},
		Summary: "Create a workspace (org-wide key, paid plan)",
		Usage:   "cowl workspaces create NAME [--color #rrggbb] [--access-mode public|internal|private] [--llms-txt]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.color, "color", "", "accent color (#rrggbb)")
			fs.StringVar(&opts.accessMode, "access-mode", "", "public, internal, or private")
			fs.BoolVar(&opts.llmsTxt, "llms-txt", false, "serve llms.txt for this workspace")
		},
		Run: func(a *App, args []string) error {
			name, err := oneArg(args, "NAME")
			if err != nil {
				return err
			}
			body := map[string]any{"name": name}
			if opts.color != "" {
				body["color"] = opts.color
			}
			if opts.accessMode != "" {
				body["access_mode"] = opts.accessMode
			}
			if a.flagWasSet("llms-txt") {
				body["llms_txt"] = opts.llmsTxt
			}
			raw, err := a.request("POST", "/api/v1/workspaces", nil, body)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var ws workspaceRow
			if err := json.Unmarshal(raw, &ws); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "created workspace %s (%q)\n", ws.ID, ws.Name)
			return nil
		},
	}
}

func cmdWorkspacesUpdate() *Command {
	var opts struct {
		name, color, accessMode string
		llmsTxt                 bool
	}
	return &Command{
		Group: "workspaces", Name: "update", OpIDs: []string{"updateWorkspace"},
		Summary: "Update a workspace",
		Usage:   "cowl workspaces update [WORKSPACE] [--name N] [--color #rrggbb] [--access-mode M] [--llms-txt=BOOL]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&opts.name, "name", "", "new name")
			fs.StringVar(&opts.color, "color", "", "accent color (#rrggbb)")
			fs.StringVar(&opts.accessMode, "access-mode", "", "public, internal, or private")
			fs.BoolVar(&opts.llmsTxt, "llms-txt", false, "serve llms.txt for this workspace")
		},
		Run: func(a *App, args []string) error {
			target, err := workspaceTarget(a, args)
			if err != nil {
				return err
			}
			body := map[string]any{}
			if a.flagWasSet("name") {
				body["name"] = opts.name
			}
			if a.flagWasSet("color") {
				body["color"] = opts.color
			}
			if a.flagWasSet("access-mode") {
				body["access_mode"] = opts.accessMode
			}
			if a.flagWasSet("llms-txt") {
				body["llms_txt"] = opts.llmsTxt
			}
			if len(body) == 0 {
				return usageError("nothing to update: pass at least one of --name, --color, --access-mode, --llms-txt")
			}
			raw, err := a.request("PATCH", "/api/v1/workspaces/"+url.PathEscape(target), nil, body)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var ws workspaceRow
			if err := json.Unmarshal(raw, &ws); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "updated workspace %s\n", ws.ID)
			return nil
		},
	}
}

func cmdWorkspacesDelete() *Command {
	var yes bool
	return &Command{
		Group: "workspaces", Name: "delete", OpIDs: []string{"deleteWorkspace"},
		Summary: "Delete a workspace and all of its content",
		Usage:   "cowl workspaces delete WORKSPACE [--yes]",
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&yes, "yes", false, "skip the confirmation prompt")
		},
		Run: func(a *App, args []string) error {
			if len(args) != 1 {
				return usageError("expected exactly one WORKSPACE argument")
			}
			if err := a.confirm("delete workspace "+args[0]+" and ALL of its content", yes); err != nil {
				return err
			}
			raw, err := a.request("DELETE", "/api/v1/workspaces/"+url.PathEscape(args[0]), nil, nil)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			fmt.Fprintf(a.Out, "deleted workspace %s\n", args[0])
			return nil
		},
	}
}
