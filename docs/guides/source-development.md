# Source development

This guide shows how to run the same checks as CI. Run each command from the
repository root and keep generated files outside the repository.

## Toolchains

CI is pinned to:

- Go 1.26.6; and
- .NET SDK 10.0.303 through `global.json`.

The Go module supports Go 1.26 and later, but contributors should use Go 1.26.6
to match CI.

Check your installed versions before investigating a build failure:

```bash
GOTOOLCHAIN=local go env GOVERSION
dotnet --version
```

Expected output is `go1.26.6` and `10.0.303`, respectively. With
`GOTOOLCHAIN=local`, Go does not switch to or download the preferred toolchain;
the command reports the installed local version. Install Go 1.26.6 before
continuing if it reports another version.

## Go checks

The Go code uses only the standard library. Run formatting, module, static, and
test checks with the pinned toolchain:

```bash
export GOTOOLCHAIN=local

gofmt -w $(git ls-files '*.go')
go mod tidy -diff
go vet ./...
go test -p=1 -timeout=15m -count=1 ./...
go test -p=1 -timeout=25m -count=1 -race ./cmd/... ./internal/... ./pkg/...
```

`gofmt -w` edits files in place, so inspect its diff. Run the slower race check
on supported Go race platforms; C# supplies the Windows runtime lane.

Build the CLI outside the repository:

```bash
go build -o /tmp/http-retry-check ./cmd/http-retry-check
/tmp/http-retry-check help
/tmp/http-retry-check http check \
  conformance/http-retry-check/v1/projections/positive/report.json
```

## C# checks

The production project uses only the .NET base class library. Test dependencies
are pinned in the package metadata and lock files.

Restore, build, and test with:

```bash
dotnet restore csharp/HttpRetryCheck.slnx \
  --locked-mode \
  --configfile csharp/NuGet.Config \
  --no-http-cache \
  --verbosity minimal

dotnet build csharp/HttpRetryCheck.slnx \
  --configuration Release \
  --no-restore \
  --verbosity minimal

dotnet test csharp/tests/HttpRetryCheck.Tests/HttpRetryCheck.Tests.csproj \
  --configuration Release \
  --no-build \
  --no-restore \
  --settings csharp/HttpRetryCheck.runsettings \
  --verbosity normal
```

Restore may contact NuGet.org for locked packages and vulnerability data. Keep
`--locked-mode` when validating a contribution. The tests run on Linux and
Windows and cover the public API, runtime, shared test data, schemas, output
formats, and expected source files.

## Change shared behaviour

Changes to scenarios, observations, findings, reports, or output formats usually
require updates to:

- Go implementation and tests;
- C# implementation and tests;
- [`conformance/http-retry-check/v1`](../../conformance/http-retry-check/v1);
- [`schemas/v1`](../../schemas/v1);
- [scenario semantics](../reference/semantics.md); and
- user guides or CLI documentation affected by the change.

Go and C# must continue to produce byte-for-byte identical JSON, JUnit,
Markdown, and artifact files for the same result.

Regenerate the language-neutral corpus into a fresh directory and compare it
with the committed copy without using the network:

```bash
corpus_parent="$(mktemp -d)"
corpus_output="$corpus_parent/v1"

GOTOOLCHAIN=local \
GOPROXY=off \
HTTP_RETRY_CHECK_REGENERATE_CORPUS=1 \
HTTP_RETRY_CHECK_CORPUS_OUTPUT="$corpus_output" \
go test -tags=corpusgen -count=1 \
  -run '^TestRegenerateHTTPRetryCheckCorpus$' \
  ./internal/httpcheck/v1/corpus

diff -ru conformance/http-retry-check/v1 "$corpus_output"
```

The generator requires a clean absolute output path that does not exist. A
clean comparison prints no diff. Review any expected diff before replacing the
committed corpus.

## Documentation checks

Check Markdown links, example paths, commands, and claims with every
documentation change. Keep the public identifiers consistent:
`github.com/aleksei-kislov/http-retry-check`, `http-retry-check`, and
`HttpRetryCheck.V1`.

## Verify a release candidate

Run the Go and C# checks above on the clean commit that will be tagged. The Go
workflow creates a semantic-version tag in a disposable standalone clone,
builds the CLI with VCS stamping enabled, and checks its `version` output. It
never creates or changes a tag in the source repository or remote.

After both GitHub workflows pass on that exact commit, create the release tag.
Tag pushes run both workflows again. Wait for them to pass, then verify a fresh
standalone clone of the tag before publishing artifacts:

```bash
git status --short
git rev-parse HEAD
git rev-list -n 1 v0.1.1
go test -p=1 -timeout=15m -count=1 ./...
release_binary="$(mktemp -d)/http-retry-check"
go build -o "$release_binary" ./cmd/http-retry-check
"$release_binary" version
```

The two revisions must match, the working tree must be clean, and the command
must print `http-retry-check v0.1.1`. Replace `v0.1.1` with the candidate tag
for later releases. Do not move a published tag; fix a rejected candidate on a
new commit and test that commit again.
