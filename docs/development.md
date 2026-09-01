# Development

## Setup

    devbox shell        # or: direnv allow, if you use direnv

That provides Go, just, helm and goreleaser at the versions CI uses.
`CGO_ENABLED=0` is set in `devbox.json`, so the binary is static and the image
can be distroless.

## The one command

    just check

That is what CI runs — vet, test, build, plus the chart checks. It builds
`./cmd/netbox-mcp` explicitly rather than relying on `go build ./...`, because
a repo whose main package has gone missing still passes the latter.

## Running it against a real NetBox

    just run   https://netbox.example.com <token>              # stdio
    just serve https://netbox.example.com <token> 127.0.0.1:8080  # http

`serve` binds loopback by default here, unlike the production default: this
server authenticates nothing, so a development instance should not be on a
network interface.

## Testing

Tests drive the **registered tools** over an in-memory MCP transport against a
stub NetBox, rather than calling helpers directly. A test that calls a helper
proves the helper works; the bugs worth catching are in how a tool wires itself
to the client.

### Confirm a test fails before believing it

Every behavioural test here was checked by breaking the thing it names and
watching it fail. Four are worth keeping that way, because each protects
something whose absence is invisible:

- the write blocklist — without it NetBox accepts a script, which it then runs
- update being a `PATCH` — a `PUT` silently clears every unmentioned field
- the `limit` cap — an unbounded list returns a whole estate
- the `__in` refusal — NetBox ignores the suffix and answers with everything

A mutation test is only evidence if the mutation actually applied. Print the
diff, or a before-and-after count you measured both halves of.

## The chart

`just chart` lints and renders it, then checks the things that should **fail**:
a missing `netbox.url`, a URL ending in `/api`, and an unknown values key. A
chart exercised only with correct values has not been tested — the schema and
guards exist to reject input.

## Releasing

    git tag -a v0.2.0 -F <message-file>
    git push origin v0.2.0

The `Release` workflow builds and publishes the image via ko (no Dockerfile,
multi-arch `amd64` + `arm64`) and the chart via helmctl.

Bump `charts/netbox-mcp/Chart.yaml` in the same commit as the code. Note the
chart version and `appVersion` do **not** track each other here: the chart
starts at 1.0.0 because `opwerm/netbox-mcp-chart` already publishes 0.x to the
same OCI path, packaging the upstream Python server. Both remain published --
0.x is the read-only upstream kept as a fallback, 1.x is this one -- so the
major version is what tells them apart. Do not release a 0.x from this repo.

Preview without publishing:

    just snapshot

goreleaser refuses to run on a dirty tree, and an uncommitted `devbox.lock`
counts.

**Write commit and tag messages to a file and use `-F`.** Backticks inside a
double-quoted shell string execute.

## Adding a tool

The tools are generic over an object type, so most new capability is a new
*endpoint shape*, not a new model. Before adding one, check whether
`netbox_get_objects` already covers it.

If you do add one:

1. Give it annotations — a tool with none fails `TestEveryToolIsAnnotated`.
2. Write the description for a model, not a developer. Say what it returns and,
   where NetBox is surprising, what it does not.
3. If it writes, route it through `writablePath` so the blocklist applies.
4. Bound anything that can return a list.
