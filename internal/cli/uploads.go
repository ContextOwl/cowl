package cli

import (
	"encoding/json"
	"fmt"
	"os"
)

func cmdUploadsImage() *Command {
	return &Command{
		Group: "uploads", Name: "image", OpIDs: []string{"uploadImage"},
		Summary: "Upload a PNG, JPEG, WebP, or GIF image",
		Usage:   "cowl uploads image FILE [-w WORKSPACE]",
		Run: func(a *App, args []string) error {
			file, err := oneArg(args, "FILE")
			if err != nil {
				return err
			}
			content, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("read image: %w", err)
			}
			raw, err := a.requestFile("POST", a.ws()+"/uploads", file, content)
			if err != nil {
				return err
			}
			if a.g.jsonOut {
				return a.printJSON(raw)
			}
			var response struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(raw, &response); err != nil {
				return err
			}
			fmt.Fprintln(a.Out, response.URL)
			return nil
		},
	}
}
