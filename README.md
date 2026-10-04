# cowl

`cowl` is the command-line client for the ContextOwl REST API. Each REST operation is a command, and each command uses the same agent key and permissions as the ContextOwl MCP server.

## Install

To install the latest verified release to `~/.local/bin` on Linux or macOS, run the installer:

```bash
installer="$(mktemp)"
curl -fsSL https://github.com/ContextOwl/cowl/releases/latest/download/install.sh -o "$installer"
sh "$installer"
rm "$installer"
```

The installer checks the archive against `SHA256SUMS`. Set `COWL_INSTALL_DIR` to install to a different directory. Windows archives are on the [releases page](https://github.com/ContextOwl/cowl/releases).

To build from source, run:

```bash
go install github.com/ContextOwl/cowl/cmd/cowl@latest
```

## Sign in

Create an agent key in **Admin > Settings > API**. Then save the key:

```bash
cowl auth login                                       # hidden prompt
cowl auth login --with-token -w platform < key.txt   # key from stdin, default workspace
cowl whoami                                           # role, workspaces and permissions of the key
```

For a self-hosted server, add `--base-url https://docs.example.com` to `cowl auth login`. cowl saves the base URL with the key and sends the saved key only to that base URL.

In CI, set `CONTEXTOWL_PAT` instead. Add `CONTEXTOWL_BASE_URL` for a self-hosted server and `CONTEXTOWL_WORKSPACE` for an org-wide key. A key from the environment goes only to `CONTEXTOWL_BASE_URL` or to the default host. The `COWL_PAT`, `COWL_BASE_URL`, `COWL_WORKSPACE` and `COWL_CONFIG` names also work.

## Use it with an agent

To teach a coding agent to use cowl, install the agent skill:

```bash
npx skills add ContextOwl/cowl
```

The skill tells the agent to search before it answers, to read single sections, to cite URLs and to send doc fixes as proposals that an editor reviews. It pre-approves read commands only.

## Agent mode

When stdout is not a terminal, each command that prints a table or a receipt prints the REST response as one line of JSON. Add `--json` to get JSON on a terminal. `cowl articles get`, `cowl changelog get` and `cowl openapi spec` print their content in both cases.

When stderr is not a terminal, an error is one JSON line in the error envelope of the API:

```json
{"error":{"code":"stale_revision","message":"the article changed","status":409,"details":{"currentRevision":"8f3a2c1b9d0e"}}}
```

A failure that cowl finds before a request uses the same envelope with status 0 and the code `usage`, `no_key`, `untrusted_host`, `network_error`, `config_error`, `invalid_response` or `aborted`.

| Exit code | Meaning |
|---|---|
| 0 | Success |
| 1 | Other error |
| 2 | Usage error, or HTTP 400, 413 or 422 |
| 3 | HTTP 404 |
| 4 | No key, untrusted host, or HTTP 401, 402 or 403 |
| 5 | HTTP 409 |
| 6 | HTTP 429 after one retry |
| 7 | HTTP 5xx or a network failure |

When something does not work, run `cowl doctor`. It checks the setup and the connection, and it never prints the key, names, hosts or paths, so you can share its output in an issue.

## Documentation

Run `cowl help` for the command list. The [CLI guide](https://developer.contextowl.co/docs/platform/cli) explains installation, authentication, commands and scripting.
