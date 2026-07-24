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
		cmdWorkspacesList(), cmdWorkspacesCreate(), cmdWorkspacesUpdate(), cmdWorkspacesDelete(),
		cmdArticlesList(), cmdArticlesGet(), cmdArticlesCreate(), cmdArticlesUpdate(), cmdArticlesPlace(),
		cmdSectionsCreate(),
		cmdProposalsList(), cmdProposalsCreate(),
		cmdChangelogList(), cmdChangelogCreate(), cmdChangelogUpdate(), cmdChangelogDelete(),
		cmdLandingGet(), cmdLandingAutofill(), cmdLandingSet(), cmdLandingPropose(),
		cmdMemoryList(), cmdMemorySearch(), cmdMemoryGet(), cmdMemoryWrite(), cmdMemoryDelete(),
		cmdOpenAPIStatus(), cmdOpenAPIAttach(), cmdOpenAPISync(), cmdOpenAPIDetach(),
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
		Usage:   "cowl version",
		Run: func(a *App, args []string) error {
			fmt.Fprintf(a.Out, "cowl %s %s/%s\n", Version, runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
