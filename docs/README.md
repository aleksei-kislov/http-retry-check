# HTTP Retry Check documentation

HTTP Retry Check tests clients against six controlled HTTP/1 retry scenarios.
Choose the shortest path for your task:

| I want to… | Start here |
| --- | --- |
| Run an example in five minutes | [Developer quick start](guides/developer-quickstart.md) |
| See a real Go retry library fail, then pass with a fix | [`go-retryablehttp` example](../examples/adapters/go-retryablehttp) |
| Compare safe and unsafe .NET resilience pipelines | [.NET HTTP resilience retry example](../csharp/examples/HttpResilienceRetry) |
| Test a Go client | [Complete Go usage guide](guides/go.md) |
| Test a .NET client | [Complete C# usage guide](guides/csharp.md) |
| Use the CLI | [CLI reference](reference/cli.md) |
| Understand run settings, outcomes, and findings | [Scenario semantics](reference/semantics.md) |
| Understand the trust boundary and evidence flow | [Architecture](architecture.md) |
| Build, test, or contribute to the source | [Source development](guides/source-development.md) |

## Examples and evidence

- [`conformance/http-retry-check/v1`](../conformance/http-retry-check/v1): valid
  and invalid language-neutral evidence.
- [`schemas/v1`](../schemas/v1): report and manifest JSON Schemas.
- [`examples/httpcheck/v1`](../examples/httpcheck/v1): safe and intentionally
  unsafe Go examples.
- [`examples/adapters/go-retryablehttp`](../examples/adapters/go-retryablehttp):
  a pinned real-client failure and a passing policy.
- [`csharp/examples/HttpResilienceRetry`](../csharp/examples/HttpResilienceRetry):
  safe and intentionally unsafe .NET resilience pipelines.

## Project information

- [Contributing](../CONTRIBUTING.md)
- [Support](../SUPPORT.md)
- [Security reporting](../SECURITY.md)
- [Governance](../GOVERNANCE.md)
- [Apache License 2.0](../LICENSE)
