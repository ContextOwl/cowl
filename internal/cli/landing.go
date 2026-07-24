package cli

import (
	"encoding/json"
	"flag"
	"fmt"
)

func cmdLandingGet() *Command {
	return &Command{
		Group: "landing", Name: "get", OpIDs: []string{"getLanding"},
		Summary: "Print the workspace landing page as JSON",
		Usage:   "cowl landing get [-w WORKSPACE]",
		Run: func(a *App, args []string) error {
			raw, err := a.request("GET", a.ws()+"/landing", nil, nil)
			if err != nil {
				return err
			}
			return a.printJSON(raw)
		},
	}
}

func cmdLandingAutofill() *Command {
	return &Command{
		Group: "landing", Name: "autofill", OpIDs: []string{"autofillLanding"},
		Summary: "Generate a landing draft from workspace content (not saved)",
		Usage:   "cowl landing autofill [-w WORKSPACE]",
		Run: func(a *App, args []string) error {
			raw, err := a.request("GET", a.ws()+"/landing/autofill", nil, nil)
			if err != nil {
				return err
			}
			return a.printJSON(raw)
		},
	}
}

func cmdLandingSet() *Command {
	var file string
	return &Command{
		Group: "landing", Name: "set", OpIDs: []string{"setLanding"},
		Summary: "Write the workspace landing page from a JSON file",
		Usage:   "cowl landing set --file landing.json|-",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&file, "file", "", "landing JSON file, - for stdin (required)")
		},
		Run: func(a *App, args []string) error {
			body, err := landingJSON(a, file)
			if err != nil {
				return err
			}
			raw, err := a.request("PUT", a.ws()+"/landing", nil, body)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			fmt.Fprintln(a.Out, "landing updated")
			return nil
		},
	}
}

func cmdLandingPropose() *Command {
	var file, note string
	return &Command{
		Group: "landing", Name: "propose", OpIDs: []string{"proposeLandingEdit"},
		Summary: "Propose a landing change for editor review",
		Usage:   "cowl landing propose --file landing.json|- [--note NOTE]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&file, "file", "", "landing JSON file, - for stdin (required)")
			fs.StringVar(&note, "note", "", "note for the reviewer")
		},
		Run: func(a *App, args []string) error {
			landing, err := landingJSON(a, file)
			if err != nil {
				return err
			}
			body := map[string]any{"landing": landing}
			if note != "" {
				body["note"] = note
			}
			raw, err := a.request("POST", a.ws()+"/landing/proposals", nil, body)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var ref struct {
				ID     int64  `json:"id"`
				Status string `json:"status"`
			}
			if err := json.Unmarshal(raw, &ref); err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "created landing proposal %d (%s)\n", ref.ID, ref.Status)
			return nil
		},
	}
}

func landingJSON(a *App, file string) (json.RawMessage, error) {
	if file == "" {
		return nil, usageError("--file is required (a landing JSON document; try 'cowl landing get' or 'cowl landing autofill' for the shape)")
	}
	data, err := a.readFileArg(file)
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("%s is not valid JSON", file)
	}
	return json.RawMessage(data), nil
}
