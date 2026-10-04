package cli

import (
	"flag"
	"fmt"
	"net/url"
	"strings"
)

type repeatedFlag []string

func (r *repeatedFlag) String() string { return strings.Join(*r, ",") }
func (r *repeatedFlag) Set(v string) error {
	*r = append(*r, v)
	return nil
}

var apiMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// cmdAPI is the escape hatch: call any /api/v1 path directly, like `gh api`.
func cmdAPI() *Command {
	var input string
	var queries repeatedFlag
	return &Command{
		Name:    "api",
		Summary: "Call any REST endpoint directly (escape hatch)",
		Usage:   "cowl api METHOD PATH [--input FILE|-] [--query key=value]...",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&input, "input", "", "JSON request body file, - for stdin")
			fs.Var(&queries, "query", "query parameter key=value (repeatable)")
		},
		Run: func(a *App, args []string) error {
			if len(args) != 2 {
				return usageError("expected METHOD and PATH arguments")
			}
			method := strings.ToUpper(args[0])
			if !apiMethods[method] {
				return usageError("METHOD must be one of GET, POST, PUT, PATCH, DELETE")
			}
			path := args[1]
			if !strings.HasPrefix(path, "/") {
				path = "/api/v1/" + path
			}
			q := url.Values{}
			for _, kv := range queries {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					return usageError("--query needs key=value, got: " + kv)
				}
				q.Add(k, v)
			}
			var body any
			if input != "" {
				data, err := a.readFileArg(input)
				if err != nil {
					return err
				}
				body = data
			}
			raw, err := a.request(method, path, q, body)
			if err != nil {
				return err
			}
			if len(raw) == 0 && !a.jsonOut() {
				fmt.Fprintln(a.Out, "ok")
				return nil
			}
			return a.printJSON(raw)
		},
	}
}
