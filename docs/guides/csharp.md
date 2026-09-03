# Use HTTP Retry Check from C#

Build a client with the retry settings you want to test, then pass it to HTTP
Retry Check. The API accepts an `HttpMessageInvoker`, including `HttpClient`.
The C# implementation targets `net10.0`, runs all six HTTP/1 scenarios, and has
no production package dependencies.

## Install the package

Use .NET SDK 10.0.303 and add the current release to your `net10.0` project:

```bash
dotnet add package HttpRetryCheck --version 0.2.0
```

## Build from source instead

To work from a source checkout, reference the project directly:

```xml
<ItemGroup>
  <ProjectReference Include="/absolute/path/to/http-retry-check/csharp/src/HttpRetryCheck/HttpRetryCheck.csproj" />
</ItemGroup>
```

The repository uses MSTest only for its own tests.

## Add the test helper

Test the client configuration your application actually uses first. This
example starts with a plain `HttpClient`; replace its construction with your
production handler chain and retry settings:

```csharp
using System;
using System.Net.Http;
using System.Threading.Tasks;
using HttpRetryCheck.V1.Testing;
using Microsoft.VisualStudio.TestTools.UnitTesting;

namespace HttpRetryCheck.Example;

[TestClass]
public sealed class HttpRetryTests
{
    public TestContext TestContext { get; set; } = null!;

    [TestMethod]
    public async Task TestHttpRetryScenarios()
    {
        using var client = new HttpClient(); // Replace with your application's client.

        await ScenarioTest.CheckAsync(
            client,
            line => TestContext.WriteLine(line));
    }
}
```

Adapt the handler chain and client construction for your retry implementation.
Your code remains responsible for disposal, cancellation, and shared client
state; the suite does not dispose of or reconfigure the client. The token
bounds the suite's own work, but `RunAsync` still waits for your handler's
`SendAsync` call. A handler that ignores cancellation can therefore block the
run after the token is cancelled.

### Optional direct HTTP/1 baseline

After testing production settings, you can remove ambient proxy and protocol
selection from the comparison:

```csharp
using var handler = new SocketsHttpHandler
{
    AllowAutoRedirect = true,
    MaxAutomaticRedirections = 2,
    UseCookies = false,
    UseProxy = false,
};
using var client = new HttpClient(handler)
{
    DefaultRequestVersion = System.Net.HttpVersion.Version11,
    DefaultVersionPolicy = HttpVersionPolicy.RequestVersionExact,
};
```

`UseProxy = false` prevents system or environment proxy settings from routing
the controlled requests. The version policy keeps the client on HTTP/1.1. This
baseline changes the client configuration, so use it to isolate a production
result rather than replace one.

If your production handler enables `ExpectContinue`, keep
`Expect100ContinueTimeout` comfortably below the selected connection timeout;
otherwise the result can be inconclusive.

A passing run logs six lines and returns normally. Unsafe and inconclusive
runs throw `ScenarioAssertionException` with different messages. Treat both as
failures: correct the unsafe client behavior, or fix the first incomplete scenario.
The [finding-code reference](../reference/semantics.md#finding-code-reference)
maps the diagnostic callback's text to canonical JSON codes.

## Configure the run

`ScenarioOptions` exposes the same defaults and bounds as Go:

| Property | Default | Valid value | What it bounds |
| --- | --- | --- | --- |
| `ScenarioTimeout` | 5 seconds | More than zero, at most 1 minute | One complete scenario |
| `ConnectionTimeout` | 2 seconds | More than zero, at most 1 minute | One accepted connection |
| `QuietWindow` | 300 milliseconds | More than zero, at most 1 minute | Retry observation after `SendAsync` and request content finish |
| `AttemptLimit` | 2 | Integer from 1 through 3 | Attempts allowed before `attempt_limit_exceeded` |

After adding `using HttpRetryCheck.V1;`, set only the values you need to change:

```csharp
var options = new ScenarioOptions(
    quietWindow: TimeSpan.FromMilliseconds(500),
    attemptLimit: 3);

await ScenarioTest.CheckAsync(
    client,
    options,
    line => TestContext.WriteLine(line));
```

Invalid values are rejected before the client runs. The attempt limit is
recorded in every row and evaluated against the observed attempt count. Raising
it changes only the general attempt-limit rule; a replay after acceptance, an
uncertain request, or a pending response remains unsafe. See
[run settings](../reference/semantics.md#run-settings) for the shared rules.

## Work with the result directly

Use the root API when you need the six-row result in your own code:

```csharp
using HttpRetryCheck.V1;

SuiteResult result = await ScenarioSuite.RunAsync(client);
ScenarioSuite.Validate(result);

foreach (ScenarioResult row in result.Scenarios)
{
    Console.WriteLine(
        $"{ScenarioExplanations.ScenarioText(row.Scenario)}: " +
        ScenarioExplanations.AssessmentText(row.Assessment));
}
```

Validation recalculates the findings, each row, and the overall result from the
recorded observations. Each `Observation` includes the selected `AttemptLimit`
and captured `Protocol`; a complete capture records `HTTP/1.1`. The returned
result cannot be changed.
If the caller's token is cancelled, `RunAsync` throws an
`OperationCanceledException` carrying that token after cleanup; `CheckAsync`
lets the same exception propagate.
Other exceptions from your handler or its response are also rethrown after
the suite closes its listeners and settles suite-owned request content.

## Produce deterministic evidence

The reporting API creates the same bytes as Go for an equivalent result:

```csharp
using System.Collections.Generic;
using System.IO;
using HttpRetryCheck.V1.Reporting;

Report report = ScenarioReports.Create(result);
byte[] canonicalJson = ScenarioReports.Encode(report);
byte[] junit = ScenarioReports.JUnit(report);
byte[] summary = ScenarioReports.GitHubSummary(report);
IReadOnlyList<ArtifactFile> artifact = ScenarioReports.BuildArtifact(report);

ScenarioReports.Validate(report);
ScenarioReports.ValidateArtifact(artifact);
await File.WriteAllBytesAsync("report.json", canonicalJson);
```

The reporting API returns the files in memory, leaving you to decide where and
how to store them. From the checkout root, the Go-built CLI can create a C#
test scaffold or inspect saved evidence:

```bash
go run ./cmd/http-retry-check http init csharp \
  /absolute/existing-parent/new-csharp-scaffold
go run ./cmd/http-retry-check http check <report-or-artifact-path>
```

To validate a source contribution, follow the complete
[source-development checks](source-development.md#c-checks).

## Scope

See [scenario semantics](../reference/semantics.md) for the scenario rules and
what a passing result does and does not establish.
