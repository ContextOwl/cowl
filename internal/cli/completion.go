package cli

import (
	"fmt"
	"sort"
	"strings"
)

// completionModel derives group -> subcommands and the top-level word list
// from the command table, so completions never drift from the commands.
func completionModel() (tops []string, subs map[string][]string) {
	subs = map[string][]string{}
	seen := map[string]bool{}
	for _, c := range allCommands() {
		if c.Group == "" {
			if !seen[c.Name] {
				seen[c.Name] = true
				tops = append(tops, c.Name)
			}
			continue
		}
		if !seen[c.Group] {
			seen[c.Group] = true
			tops = append(tops, c.Group)
		}
		subs[c.Group] = append(subs[c.Group], c.Name)
	}
	tops = append(tops, "help")
	sort.Strings(tops)
	return tops, subs
}

func cmdCompletion() *Command {
	return &Command{
		Name:    "completion",
		Local:   true,
		Summary: "Print a shell completion script (bash, zsh, or fish)",
		Usage:   "cowl completion bash|zsh|fish",
		Run: func(a *App, args []string) error {
			if len(args) != 1 {
				return usageError("expected one of: bash, zsh, fish")
			}
			tops, subs := completionModel()
			switch args[0] {
			case "bash":
				printBashCompletion(a, tops, subs)
			case "zsh":
				printZshCompletion(a, tops, subs)
			case "fish":
				printFishCompletion(a, tops, subs)
			default:
				return usageError("unsupported shell: " + args[0] + " (expected bash, zsh, or fish)")
			}
			return nil
		},
	}
}

func printBashCompletion(a *App, tops []string, subs map[string][]string) {
	fmt.Fprintf(a.Out, `# bash completion for cowl. Install: eval "$(cowl completion bash)"
_cowl() {
    local cur prev words
    cur="${COMP_WORDS[COMP_CWORD]}"
    if [ "$COMP_CWORD" -eq 1 ]; then
        COMPREPLY=( $(compgen -W "%s" -- "$cur") )
        return
    fi
    case "${COMP_WORDS[1]}" in
`, strings.Join(tops, " "))
	groups := sortedKeys(subs)
	for _, g := range groups {
		fmt.Fprintf(a.Out, "        %s)\n            [ \"$COMP_CWORD\" -eq 2 ] && COMPREPLY=( $(compgen -W \"%s\" -- \"$cur\") )\n            ;;\n", g, strings.Join(subs[g], " "))
	}
	fmt.Fprint(a.Out, `    esac
}
complete -F _cowl cowl
`)
}

func printZshCompletion(a *App, tops []string, subs map[string][]string) {
	fmt.Fprintf(a.Out, `#compdef cowl
# zsh completion for cowl. Install: cowl completion zsh > "${fpath[1]}/_cowl"
_cowl() {
    if (( CURRENT == 2 )); then
        compadd %s
        return
    fi
    case "${words[2]}" in
`, strings.Join(tops, " "))
	for _, g := range sortedKeys(subs) {
		fmt.Fprintf(a.Out, "        %s)\n            (( CURRENT == 3 )) && compadd %s\n            ;;\n", g, strings.Join(subs[g], " "))
	}
	fmt.Fprint(a.Out, `    esac
}
_cowl "$@"
`)
}

func printFishCompletion(a *App, tops []string, subs map[string][]string) {
	fmt.Fprintln(a.Out, "# fish completion for cowl. Install: cowl completion fish > ~/.config/fish/completions/cowl.fish")
	for _, t := range tops {
		fmt.Fprintf(a.Out, "complete -c cowl -n '__fish_use_subcommand' -a '%s'\n", t)
	}
	for _, g := range sortedKeys(subs) {
		for _, s := range subs[g] {
			fmt.Fprintf(a.Out, "complete -c cowl -n '__fish_seen_subcommand_from %s' -a '%s'\n", g, s)
		}
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
