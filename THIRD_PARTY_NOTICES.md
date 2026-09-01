# Third-party notices

The Go and C# production code uses only the respective standard libraries. No
external package is vendored or bundled in this source repository.

- Go binaries include parts of the Go runtime and standard library. Their
  redistribution terms are retained in
  [`third_party/licenses/go`](third_party/licenses/go).
- C# tests restore `Microsoft.NET.Test.Sdk` and MSTest packages from NuGet.
  They are development dependencies and are not part of the production
  assembly. Exact versions are recorded in
  [`csharp/Directory.Packages.props`](csharp/Directory.Packages.props) and the
  [test lock file](csharp/tests/HttpRetryCheck.Tests/packages.lock.json);
  applicable texts are retained under
  [`third_party/licenses`](third_party/licenses).
