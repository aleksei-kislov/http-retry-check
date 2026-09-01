# HTTP Retry Check architecture

HTTP Retry Check has separate Go and C# implementations. Each runs the same six
HTTP/1 scenarios and produces byte-for-byte identical evidence for the same
result.

## Components

The two implementations do not call each other. Shared schemas and test data
keep their scenario order, results, and output formats in sync.

## Runtime boundary

Go accepts a `Doer`, which is satisfied by `*http.Client`. C# accepts an
`HttpMessageInvoker`, including `HttpClient`.

Your client runs as trusted code in the same process as the suite. Your code
still controls its retry settings, connection pool, cancellation, disposal,
and global state. The suite does not load clients by name, run plugins, or
start child processes.

Before execution, the suite reserves seven fresh TCP listeners on literal
`127.0.0.1`. Five scenarios use one listener each; the redirect scenario uses
two. The suite gives your client synthetic requests addressed only to these
loopback endpoints.

The suite observes only those endpoints. It does not sandbox the client or see
traffic the client sends elsewhere.

## Result model

The [scenario semantics](reference/semantics.md) define the scenario order,
allowed behaviour, recorded values, findings, and scope statement.

Validation recalculates each row from its recorded values. Unsafe takes
precedence over inconclusive, and all six rows must pass for the overall result
to pass.

The Go runtime and public Go API use one evaluator. The CLI evidence decoder
reconstructs the rules separately so it does not trust stored findings, text,
or assessments. C# implements the same rules independently; the shared corpus
tests both languages against the same valid and invalid cases.

## Evidence flow

Both implementations can turn a validated result into:

1. canonical JSON;
2. JUnit XML, where unsafe rows are failures and inconclusive rows are errors;
3. GitHub-flavoured Markdown; or
4. a four-file artifact with a manifest.

An artifact contains exactly `manifest.json`, `report.json`, `junit.xml`, and
`summary.md`. The manifest records each file's media type, size, and SHA-256,
plus one digest for the complete set. Validation rebuilds all four files and
compares their bytes.

These digests detect changed evidence. They do not prove who ran the test or
what environment it ran in.

The repository includes passing, unsafe, inconclusive, mixed, and invalid
examples. The Go and C# tests both read the same files.

## CLI boundary

The `http-retry-check` command creates test scaffolds and reads or packages
saved evidence. It does not run clients. See the
[command reference](reference/cli.md) for commands, input rules, and exit codes.

## Limits

This version covers six local HTTP/1 scenarios. It does not cover TLS, HTTP/2,
HTTP/3, DNS, ambient proxies, external origins, real credentials, hidden client
activity, or every retry policy and connection race. It does not certify a
client, sandbox code, sign artifacts, or attest the test environment. Use it
alongside idempotency protections, integration tests, security review, and
production monitoring.
