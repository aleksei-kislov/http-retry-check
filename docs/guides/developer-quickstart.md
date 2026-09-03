# Five-minute quick start

Use this guide to run HTTP Retry Check from a source checkout. Choose Go or C#;
you do not need both to test a client.

The check sends synthetic requests to seven fresh listeners on literal
`127.0.0.1` and records the result of six controlled HTTP/1 scenarios.

## Option A: run the Go example

You need Go 1.25 or later. From the repository root:

```bash
go env GOVERSION
go test -count=1 -run '^TestHTTPRetryScenarios$' -v ./examples/httpcheck/v1
```

Go 1.25 is the minimum; repository CI uses Go 1.26.6. A passing test logs six
scenario lines and ends with `PASS`.

To connect your own client, start with
[`examples/httpcheck/v1/http_retry_test.go`](../../examples/httpcheck/v1/http_retry_test.go).
Replace its example `*http.Client` with the same client construction your
application uses. The [complete Go usage guide](go.md) shows the optional
proxy-free HTTP/1 baseline, run settings, and evidence API.

## Option B: run the C# implementation

You need .NET SDK 10.0.303. From the repository root:

```bash
dotnet --version

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

The first command must print exactly `10.0.303`; the final command must report
that all tests passed. Restore may contact NuGet.org for locked test-only
packages and vulnerability data; the production project uses only the .NET
base class library.

The [complete C# usage guide](csharp.md) shows how to reference the source
project, call `ScenarioTest.CheckAsync`, choose run settings, and save evidence
from your own `net10.0` test.

## Read the result correctly

`no_unsafe_behavior_observed` passes. `unsafe_behavior_observed` and
`inconclusive` both fail; incomplete evidence is not a softer pass.

If execution stops early, fix the first incomplete row before looking at later
unavailable rows. Because the client runs in the same process, code that ignores
cancellation can block the test.

See [scenario semantics](../reference/semantics.md) for lifecycle, findings, and
scope, or the [CLI reference](../reference/cli.md) to inspect saved evidence and
use its exit codes in CI.
