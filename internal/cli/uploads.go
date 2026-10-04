package cli

import (
	"fmt"
	"os"
)

func cmdUploadsImage() *Command {
	return &Command{
		Group: "uploads", Name: "image", OpIDs: []string{"uploadImage"},
		Summary: "Upload a PNG, JPEG, WebP, or GIF image and print its URL for Markdown",
		Usage:   "cowl uploads image FILE [-w WORKSPACE]",
		Run: func(a *App, args []string) error {
			file, err := oneArg(args, "FILE")
			if err != nil {
				return err
			}
			content, err := os.ReadFile(file)
			if err != nil {
				return usageError(fmt.Sprintf("cannot read %s: %v", file, unwrapPathError(err)))
			}
			raw, err := a.requestFile("POST", a.ws()+"/uploads", file, content)
			if err != nil {
				return err
			}
			return emitAs(a, raw, func(res struct {
				URL string `json:"url"`
			}) error {
				fmt.Fprintln(a.Out, res.URL)
				return nil
			})
		},
	}
}
