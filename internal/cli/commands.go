package cli

import (
	"fmt"
	"runtime"
)

// allCommands builds the full command table. Constructors return fresh
// instances so flag state never leaks between invocations.
func allCommands() []*Command {
	return []*Command{
		cmdAuthLogin(), cmdAuthStatus(), cmdAuthLogout(),
		cmdWhoami(), cmdDoctor(),
		cmdWorkspacesList(), cmdWorkspacesCreate(), cmdWorkspacesUpdate(), cmdWorkspacesDelete(),
		cmdArticlesList(), cmdArticlesGet(), cmdArticlesCreate(), cmdArticlesUpdate(), cmdArticlesPlace(),
		cmdSectionsList(), cmdSectionsCreate(),
		cmdUploadsImage(),
		cmdProposalsList(), cmdProposalsGet(), cmdProposalsCreate(),
		cmdChangelogList(), cmdChangelogGet(), cmdChangelogCreate(), cmdChangelogUpdate(), cmdChangelogDelete(),
		cmdLandingGet(), cmdLandingAutofill(), cmdLandingSet(), cmdLandingPropose(),
		cmdOpenAPIStatus(), cmdOpenAPISpec(), cmdOpenAPIAttach(), cmdOpenAPISync(), cmdOpenAPIDetach(),
		cmdOpenAPIPages(), cmdOpenAPICreateSection(), cmdOpenAPIPlace(), cmdOpenAPIDetachPage(),
		cmdSearch(),
		cmdAPI(),
		cmdVersion(),
		cmdCompletion(),
	}
}

func cmdVersion() *Command {
	return &Command{
		Name:    "version",
		Local:   true,
		Summary: "Print the cowl version",
		Usage:   "cowl version [--json]",
		Run: func(a *App, args []string) error {
			if err := noArgs(args); err != nil {
				return err
			}
			if a.jsonOut() {
				return a.printValue(map[string]string{"version": version(), "os": runtime.GOOS, "arch": runtime.GOARCH})
			}
			fmt.Fprintf(a.Out, "cowl %s %s/%s\n", version(), runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
