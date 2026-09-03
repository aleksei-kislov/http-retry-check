# `Microsoft.Extensions.Http.Resilience` retry example

This executable runs HTTP Retry Check against two otherwise identical
`Microsoft.Extensions.Http.Resilience` handler chains. The safe control uses an
empty Polly pipeline. The intentionally unsafe profile retries one transient
failure after a fixed 10 ms delay, without jitter or `Retry-After` handling.
Both clients disable proxies and use exact HTTP/1.1.

From the repository root, restore, build, and run with the pinned SDK:

```bash
DOTNET_ROOT="$HOME/.dotnet" "$HOME/.dotnet/dotnet" restore \
  csharp/examples/HttpResilienceRetry/HttpResilienceRetry.csproj \
  --locked-mode \
  --configfile csharp/NuGet.Config \
  --no-http-cache \
  --verbosity minimal \
  -p:ContinuousIntegrationBuild=true

DOTNET_ROOT="$HOME/.dotnet" "$HOME/.dotnet/dotnet" build \
  csharp/examples/HttpResilienceRetry/HttpResilienceRetry.csproj \
  --configuration Release \
  --no-restore \
  --verbosity minimal \
  -p:ContinuousIntegrationBuild=true

DOTNET_ROOT="$HOME/.dotnet" "$HOME/.dotnet/dotnet" run \
  --project csharp/examples/HttpResilienceRetry/HttpResilienceRetry.csproj \
  --configuration Release \
  --no-build \
  --no-restore
```

Expected output:

```text
safe: assessment=NoUnsafeBehaviorObserved positive=6 unsafe=0 inconclusive=0
unsafe-retry: assessment=UnsafeBehaviorObserved positive=3 unsafe=3 inconclusive=0
```

The unsafe result is expected: it demonstrates that the suite observes this
specific retry-on-POST profile. These are bounded, same-process, local HTTP/1.1
observations, not a claim about every Polly or production configuration.
