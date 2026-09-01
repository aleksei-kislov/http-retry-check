# Use HTTP Retry Check from C#

Build a client with the retry settings you want to test, then pass it to HTTP
Retry Check. The API accepts an `HttpMessageInvoker`, including `HttpClient`.
The C# implementation targets `net10.0`, runs all six HTTP/1 scenarios, and has
no production package dependencies.

No NuGet package is published in `v0.1.1`; reference the source project from a
checkout of that tag.

## Reference the source project

Use .NET SDK 10.0.303. From your `net10.0` project, reference the checkout
directly:

```xml
<ItemGroup>
  <ProjectReference Include="/absolute/path/to/http-retry-check/csharp/src/HttpRetryCheck/HttpRetryCheck.csproj" />
</ItemGroup>
```

The repository uses MSTest only for its own tests.

## Add the test helper

Configure the client explicitly for HTTP/1.1 and disable ambient proxy use:

```csharp
using System;
using System.Net;
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
        using var handler = new SocketsHttpHandler
        {
            AllowAutoRedirect = true,
            MaxAutomaticRedirections = 2,
            UseCookies = false,
            UseProxy = false,
        };
        using var client = new HttpClient(handler)
        {
            DefaultRequestVersion = HttpVersion.Version11,
            DefaultVersionPolicy = HttpVersionPolicy.RequestVersionExact,
        };

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

A passing run logs six lines and returns normally. Unsafe and inconclusive
runs throw `ScenarioAssertionException` with different messages. Treat both as
failures: correct the unsafe client behavior, or fix the first incomplete scenario.
The [finding-code reference](../reference/semantics.md#finding-code-reference)
maps the diagnostic callback's text to canonical JSON codes.

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
recorded observations. The returned result cannot be changed.

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
