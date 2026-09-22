# Public API coverage

Checked against the [official API reference](https://workflowy.com/api-reference/)
on 2026-09-22. All 12 documented operations have CLI entry points:

| HTTP operation | CLI |
| --- | --- |
| `POST /nodes` | `create` |
| `POST /nodes/:id` | `update` |
| `GET /nodes/:id` | `get --method=get` |
| `GET /nodes?parent_id=...` | `list --method=get` |
| `DELETE /nodes/:id` | `delete` |
| `POST /nodes/:id/move` | `move` |
| `POST /nodes/:id/mirror` | `mirror` |
| `DELETE /nodes/:id/mirror` | `delete-mirror` |
| `POST /nodes/:id/complete` | `complete` |
| `POST /nodes/:id/uncomplete` | `uncomplete` |
| `GET /nodes-export` | `get --method=export --all` |
| `GET /targets` | `targets` |

This matrix covers the public API, not undocumented browser endpoints or the
separate MCP tool surface. `api_parity_test.go` tests all 12 HTTP contracts with a
local server; CLI tests cover argument handling and scope restrictions.

## Mirrors

```sh
workflowy mirror <source-id> <parent-id> --position bottom
workflowy --format=json mirror 'https://workflowy.com/#/<short-id>' <parent-id>
workflowy delete-mirror <mirror-id>
```

IDs can be full UUIDs, 12-character short IDs, or Workflowy node URLs. The CLI
resolves them using fresh GET requests, without requiring an export-cache refresh.
The parent must be an existing node; `None`, `inbox`, and `today` are not supported
by the mirror endpoint. Position is `top` (default) or `bottom`.

Creation returns `item_id` (new mirror) and `origin_id` (actual original). Mirroring
a mirror points to its original. The server also redirects a mirror destination
to its original. `delete-mirror` deletes only the mirror instance; the API rejects
ordinary nodes, and the CLI never falls back to ordinary deletion.

The public GET/export API currently does not expose enough mirror-origin metadata
to resolve existing mirrors reliably. A mirror can appear with an empty name;
use `--include-empty-names` when fetching such nodes. `report mirrors --method=backup`
can use mirror metadata from a local backup.

Because effective origins cannot be verified, `mirror` refuses creation when
`--read-root-id` or `--write-root-id` is restrictive. `delete-mirror` checks both
restrictions against the instance being deleted.

## Node fields and empty updates

JSON node output preserves `parent_id` and `completed`. Export conversion also
preserves both fields; backup conversion derives child parents and completion.

```sh
workflowy update <id> --note ""
workflowy update <id> --name ""
workflowy update <id> --layout-mode code-block
workflowy update <id> --layout-mode quote-block
```

An omitted flag leaves that field unchanged. Pass the empty value as a separate
argument (`--note ""`); the CLI argument parser rejects `--note=`.

## Live integration test

Build the CLI, then use a dedicated existing sandbox parent:

```sh
go build -o workflowy ./cmd/workflowy
TEST_PARENT_ID=<sandbox-uuid> bats test/api_mirror.bats
```

Requires Bats, jq, and `~/.workflowy/api.key` (or `API_KEY_FILE`). The test creates
its own subtree, verifies mirror creation, mirror-of-mirror origin resolution,
empty updates, and dedicated deletion, then deletes only its temporary subtree.

## Install this fork locally

From a checked-out revision of this fork, with Go installed:

```sh
mkdir -p "$HOME/.local/bin"
GOBIN="$HOME/.local/bin" go install \
  -ldflags "-X main.version=0.9.0+tanacorp.api-parity -X main.commit=$(git rev-parse HEAD)" \
  ./cmd/workflowy
```

Ensure `~/.local/bin` is on PATH. If a Homebrew installation takes precedence,
`brew unlink workflowy-cli` keeps that installation available while removing its
command symlink. Check `command -v workflowy` and `workflowy version`. To return to
the Homebrew version when Homebrew precedes `~/.local/bin`, use
`brew link workflowy-cli`.
