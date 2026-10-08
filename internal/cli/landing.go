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
			if err := noArgs(args); err != nil {
				return err
			}
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
		Summary: "Generate a landing draft from workspace content without saving it (needs landing.update)",
		Usage:   "cowl landing autofill [-w WORKSPACE]",
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			raw, err := a.request("GET", a.ws()+"/landing/autofill", nil, nil)
			if err != nil {
				return err
			}
			return a.printJSON(raw)
		},
	}
}

func cmdLandingSet() *Command {
	var file, note string
	return &Command{
		Group: "landing", Name: "set", OpIDs: []string{"setLanding"},
		Summary: "Replace the whole workspace landing page with a JSON file (start from 'cowl landing get')",
		Usage:   "cowl landing set --file landing.json|- [--note NOTE]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&file, "file", "", "landing JSON file, - for stdin (required)")
			fs.StringVar(&note, "note", "", noteHelp)
		},
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			body, err := landingJSON(a, file)
			if err != nil {
				return err
			}
			raw, httpStatus, err := a.requestStatus("PUT", a.ws()+"/landing", noteQuery(note), body)
			if err != nil {
				return err
			}
			return a.emitWrite(raw, httpStatus, func() error {
				fmt.Fprintln(a.Out, "landing updated")
				return nil
			})
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
			if err := noArgs(args); err != nil {
				return err
			}
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
			return emitAs(a, raw, func(ref struct {
				ID        int64  `json:"id"`
				Status    string `json:"status"`
				ReviewURL string `json:"reviewUrl"`
			}) error {
				fmt.Fprintf(a.Out, "created landing proposal %d (%s)%s\n", ref.ID, ref.Status, urlSuffix(ref.ReviewURL))
				return nil
			})
		},
	}
}

func landingJSON(a *App, file string) (json.RawMessage, error) {
	if file == "" {
		return nil, usageError("--file is required: a landing JSON document. 'cowl landing get' and 'cowl landing autofill' show the shape")
	}
	data, err := a.readFileArg(file)
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return nil, usageError(file + " is not valid JSON")
	}
	return json.RawMessage(data), nil
}
