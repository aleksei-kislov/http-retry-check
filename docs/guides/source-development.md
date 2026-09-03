# Source development

Run these commands from the repository root.

## Toolchains

CI uses Go 1.26.6 and .NET SDK 10.0.303; the Go module supports Go 1.25 and later.

```bash
go env GOVERSION
dotnet --version
```

## Go checks

Run all five checks before opening a pull request:

```bash
gofmt -w $(git ls-files '*.go')
go mod tidy -diff
go vet ./...
go test -p=1 -timeout=15m -count=1 ./...
go test -p=1 -timeout=25m -count=1 -race ./cmd/... ./internal/... ./pkg/...
```

## C# checks

Locked restore prevents dependency changes during validation.

```bash
dotnet restore csharp/HttpRetryCheck.slnx -p:ContinuousIntegrationBuild=true --locked-mode --configfile csharp/NuGet.Config --no-http-cache --verbosity minimal
dotnet build csharp/HttpRetryCheck.slnx -p:ContinuousIntegrationBuild=true --configuration Release --no-restore --verbosity minimal
dotnet test csharp/tests/HttpRetryCheck.Tests/HttpRetryCheck.Tests.csproj -p:ContinuousIntegrationBuild=true --configuration Release --no-build --no-restore --settings csharp/HttpRetryCheck.runsettings --verbosity normal
```

### Bump a test dependency

1. Change the exact version in `csharp/Directory.Packages.props`.
2. Regenerate all three lock files:

   ```bash
   for hrc_project in csharp/src/HttpRetryCheck/HttpRetryCheck.csproj csharp/tests/HttpRetryCheck.Tests/HttpRetryCheck.Tests.csproj csharp/examples/HttpResilienceRetry/HttpResilienceRetry.csproj; do
     dotnet restore "$hrc_project" -p:ContinuousIntegrationBuild=true --force-evaluate --configfile csharp/NuGet.Config --no-http-cache --verbosity minimal
   done
   ```

3. Replace the rows between `nuget-hashes:begin` and `nuget-hashes:end` in `.github/workflows/csharp-ci.yml` with this output:

   ```bash
   set -euo pipefail
   hrc_locks=(csharp/src/HttpRetryCheck/packages.lock.json csharp/tests/HttpRetryCheck.Tests/packages.lock.json csharp/examples/HttpResilienceRetry/packages.lock.json)
   jq -r -s '[.[]|.dependencies[]|to_entries[]|select(.value.type!="Project")|[(.key|ascii_downcase),(.value.resolved|ascii_downcase)]]|unique|sort|.[]|@tsv' "${hrc_locks[@]}" |
   while IFS=$'\t' read -r hrc_id hrc_version; do
     hrc_archive="$hrc_id/$hrc_version/$hrc_id.$hrc_version.nupkg"
     hrc_sha256="$(curl -fsSL --retry 3 "https://api.nuget.org/v3-flatcontainer/$hrc_archive" | shasum -a 256 | awk '{print $1}')"
     printf "            '%s' = '%s'\n" "$hrc_archive" "$hrc_sha256"
   done
   ```

4. Update exact-version assertions in `csharp/tests/HttpRetryCheck.Tests/Authority/SourceAuthorityTests.cs`.
5. Run the C# checks above, then restore, build, and run `csharp/examples/HttpResilienceRetry` in locked Release mode.

## Change shared behaviour

Update the Go and C# code and tests, `conformance/http-retry-check/v1`,
`schemas/v1`, scenario semantics, and affected guides and CLI documentation
together. Both implementations must continue to produce identical evidence bytes.

```bash
corpus_parent="$(mktemp -d)"; corpus_output="$corpus_parent/v1"
GOPROXY=off HTTP_RETRY_CHECK_REGENERATE_CORPUS=1 HTTP_RETRY_CHECK_CORPUS_OUTPUT="$corpus_output" go test -tags=corpusgen -count=1 -run '^TestRegenerateHTTPRetryCheckCorpus$' ./internal/httpcheck/v1/corpus
diff -ru conformance/http-retry-check/v1 "$corpus_output"
```

## Release

Pushing a `v*` tag runs `.github/workflows/release.yml`. Releases require a tag
on current `main` after both CI workflows pass, the public repository
`aleksei-kislov/http-retry-check`, a matching C# `VersionPrefix`, a `NUGET_USER`
repository variable, and a NuGet.org trusted-publishing policy bound to `release.yml`.
Tag an `-rc.N` version first. Install its package from NuGet.org in a scratch
project and verify the GitHub release binaries against `SHA256SUMS` before
tagging the final version. Published tags and packages are never moved or deleted.
