# HTTP Retry Check command reference

`v0.1.0` has no prebuilt binary. From a source checkout with Go 1.26 or
later:

```bash
go run ./cmd/http-retry-check help
```

See [source development](../guides/source-development.md#toolchains) to use the
same Go 1.26.6 version as CI.

Build a temporary binary when a script needs the program's exit code:

```bash
go build -o /tmp/http-retry-check ./cmd/http-retry-check
/tmp/http-retry-check http check report.json
```

`go run` wraps nonzero exits and prints `exit status N`, so it is unsuitable for
checking the program's exit code.

## Commands

```text
http-retry-check help [command]
http-retry-check version
http-retry-check http init <go|csharp> <new-scaffold-directory>
http-retry-check http validate <report-path|artifact-directory|->
http-retry-check http check <report-path|artifact-directory|->
http-retry-check http explain <report-path|->
http-retry-check http artifact <report-path|-> <new-output-directory>
```

`-h` and `--help` show help, and `--version` aliases `version`. The `version`
command reports the tagged module version when Go supplies one and `0.0.0-dev`
for a local development build. No command runs a client; your Go or C# test runs
the scenarios.

## `http init`

Create the built-in Go or C# test scaffold:

```bash
/tmp/http-retry-check http init go /absolute/path/to/new-go-scaffold
/tmp/http-retry-check http init csharp /absolute/path/to/new-csharp-scaffold
```

Success prints `HTTP Retry Check scaffold created`.

The Go scaffold contains `README.md` and `http_retry_test.go`. The C# scaffold
contains `README.md` and `HttpRetryTests.cs`. Move the generated files into a
module or test project that references this source; a scaffold is not an
installer or standalone project.

## `http validate`

Validate a JSON report or four-file artifact:

```bash
/tmp/http-retry-check http validate report.json
/tmp/http-retry-check http validate artifact-directory
printf '%s' "$REPORT_JSON" | /tmp/http-retry-check http validate -
```

Success prints `HTTP Retry Check evidence is valid`. Validation recalculates
fields derived from the recorded observations. An artifact must contain exactly
`manifest.json`, `report.json`, `junit.xml`, and `summary.md`, with matching
manifest sizes and digests and no extra entries.

## `http check`

Check whether a valid report or artifact should pass CI:

| Report outcome | Output | Exit |
| --- | --- | --- |
| positive | `HTTP Retry Check found no unsafe behavior` | 0 |
| unsafe | `HTTP Retry Check found unsafe behavior` | 1 |
| inconclusive | `HTTP Retry Check is inconclusive` | 1 |

```bash
/tmp/http-retry-check http check \
  conformance/http-retry-check/v1/projections/positive/report.json
/tmp/http-retry-check http check \
  conformance/http-retry-check/v1/projections/unsafe/report.json
```

Unsafe and inconclusive results both exit with 1 but print different messages.

## `http explain`

Show the overall result, each scenario, and any findings from a valid report:

```bash
/tmp/http-retry-check http explain report.json
/tmp/http-retry-check http explain - < report.json
```

This command accepts a report or standard input, not an artifact directory. It
does not add environment, client, path, or network details.

## `http artifact`

Create a four-file artifact from a valid report:

```bash
/tmp/http-retry-check http artifact report.json /absolute/path/to/new-artifact
/tmp/http-retry-check http artifact - /absolute/path/to/new-artifact < report.json
```

Success prints `HTTP Retry Check artifact created`. The command validates the
report, rebuilds each output file, and writes them without replacing an existing
path.

## Output destinations

Scaffold and artifact destinations must be new absolute paths. The parent
directories must already exist, and every parent must be a real directory rather
than a symlink. Paths containing `.` or `..`, filesystem roots, and existing
destinations are rejected. There is no force or overwrite option.

An artifact's final directory name must use printable ASCII without whitespace
or backslashes, must not end in a dot, and must not equal the reserved staging
name. Rejections use fixed text without exposing path or filesystem details.

On macOS, `/tmp` normally points to `/private/tmp`, so `/tmp/new-output` is
rejected as an output tree. Use an absent child of `/private/tmp` or another
real directory. Running the binary at `/tmp/http-retry-check` is unaffected.

Writing artifact directories without replacing an existing path is supported
on macOS amd64/arm64 and Linux amd64/arm64. On other Go platforms, artifact
creation reports that the target is unavailable; the other commands still
work.

## Input rules

The CLI reads only the path or standard input you name:

- `-` reads one report from standard input only where the command list shows
  it;
- each report or artifact payload is limited to 262,144 bytes;
- an entire artifact is limited to 1,048,576 bytes;
- symlink evidence paths and artifact members are rejected;
- report structure, identifiers, findings, generated files, and digests are
  validated; and
- errors use fixed text without echoing a path or payload.

The CLI does not search recursively or infer a current-directory input.

## Troubleshooting

Some errors share one message so that paths and input data are not echoed:

| Message and exit | Meaning | What to do |
| --- | --- | --- |
| `usage: http-retry-check …` (2) | Unsupported command, argument count, or `http init` language. | Compare the command with the list above. |
| `HTTP Retry Check evidence is invalid` (2) | The input is missing, empty, too large, a symlink or unsupported object, not in the required JSON form, inconsistent with the scenario rules, or not a four-file artifact. `http explain` also rejects artifacts. | Check the accepted input kind and input rules, then regenerate suspect evidence from a validated result. |
| `HTTP Retry Check scaffold target is unavailable` / `HTTP Retry Check artifact target is unavailable` (2) | The path violates the output rules, changed while it was checked, or cannot be written safely on this platform or filesystem. | Follow the output-destination rules; for artifacts use a portable name such as `http-retry-check-results`. |
| `http-retry-check internal failure` (3) | Reading, writing, cleanup, or console output failed after the command started. This is not an evidence result. | Check storage and permissions. If it repeats, report the revision, command, exit code, and a reproduction with private data removed. |

## Exit codes

| Exit | Meaning |
| --- | --- |
| 0 | The requested operation completed; for `check`, the outcome is positive. |
| 1 | `check` found an unsafe or inconclusive result. |
| 2 | Usage, evidence, or output destination was rejected. |
| 3 | An internal read, write, or output failure prevented the command from completing. |

For report-row and finding meanings, see the
[scenario semantics](semantics.md).
