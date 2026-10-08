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

When stdout is not a terminal, each command that prints a table or a receipt prints one line of JSON instead. For an API command, the line is the REST response body. Add `--json` to get the same line on a terminal.

`cowl articles get`, `cowl changelog get` and `cowl openapi spec` print their content in both cases. With `--json`, the first two print the REST object. `cowl openapi spec --json` prints `{"format":"yaml","spec":"..."}` with the stored spec as one string, because a YAML spec is not JSON.

When stderr is not a terminal, an error is one JSON line in the error envelope of the API:

```json
{"error":{"code":"stale_revision","message":"the article changed","status":409,"details":{"currentRevision":"8f3a2c1b9d0e"}}}
```

`status` is the HTTP status of the error. A failure that is not an HTTP error has status 0 and one of these codes: `usage`, `no_key`, `untrusted_host`, `config_error`, `network_error`, `invalid_response` or `aborted`. A server error without a JSON body gets the code `http_` and the status number, such as `http_502`.

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

## Writes that wait for review

When your organization reviews agent changes, a write to live content waits in a proposal for an editor. cowl then prints the proposal and its review link, and exits 0:

```console
$ cowl articles update api-keys --edits edits.json --note "Release 2.4 changed the key lifetime to 90 days."
proposal 42 waits for review: https://contextowl.co/admin/proposals?ws=platform&id=42
  Change api-keys: text
```

In agent mode, cowl prints the REST body, which has `pendingReview` set to true. Give the review link to the person who approves changes, and do not run the command again. A later write of the same key updates the same proposal.

These commands can wait for review and take `--note`: `articles update`, `articles place`, `landing set`, `changelog create`, `changelog update`, `changelog delete`, `openapi attach`, `openapi sync`, `openapi detach`, `openapi place` and `workspaces update`. cowl sends `--note` only when you set it. A server without the review of agent changes rejects the note in the request body, so cowl then sends the write again without it.

Unless the key is a publishing key, `cowl workspaces delete` fails with `review_required` and exits 4. `cowl workspaces create` with access mode public or `--listed` fails the same way. To make a new workspace public or listed, create it without these flags. Then pass the flags to `cowl workspaces update`, which files a proposal for an admin.

`cowl whoami` shows how the changes of the key to live content apply: `none`, `review` or `direct`. An admin can approve a key as a publishing key in **Admin > Settings > API**. Its changes then apply at once.

## Documentation

Run `cowl help` for the command list. The [CLI guide](https://developer.contextowl.co/docs/platform/cli) explains installation, authentication, commands and scripting.
