// Command cowl is the ContextOwl CLI: a client for the public REST API
// (/api/v1) authenticated with an agent key. See internal/cli.
package main

import (
	"os"

	"golang.org/x/term"

	"github.com/ContextOwl/cowl/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], cli.IO{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
		Env: os.Getenv,
		TTY: term.IsTerminal(int(os.Stdin.Fd())),
	}))
}
