# HTTP Retry Check

HTTP Retry Check runs your .NET HTTP client through six controlled HTTP/1
failure scenarios. It detects unsafe request replays, changed replay bodies,
cross-origin credential forwarding, excessive retries, and retries made before
the first request's outcome is known.

## Install

```bash
dotnet add package HttpRetryCheck --version 0.2.0
```

The package targets `net10.0` and has no package dependencies.

## Run the check

Pass the `HttpMessageInvoker` or `HttpClient` configuration your application
actually uses:

```csharp
using HttpRetryCheck.V1.Testing;

await ScenarioTest.CheckAsync(
    myConfiguredClient,
    Console.WriteLine);
```

The call completes normally only when all six scenarios pass. Unsafe and
inconclusive results throw `ScenarioAssertionException`.

See the [complete C# guide](https://github.com/aleksei-kislov/http-retry-check/blob/v0.2.0/docs/guides/csharp.md)
for configuration options, result inspection, and evidence export.

## Scope

The suite uses synthetic data and controlled loopback HTTP/1 endpoints. It
does not sandbox your client or certify its behavior outside the six scenarios.
