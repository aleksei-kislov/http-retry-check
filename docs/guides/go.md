# Use HTTP Retry Check from Go

Build a client with the retry settings you want to test, then pass it to HTTP
Retry Check. The API accepts a `Doer`, including `*http.Client`. It runs six
HTTP/1 scenarios and can save the result as JSON, JUnit XML, Markdown, or a
four-file artifact.

## Add the module

From the Go module where you want to test your client, add the current release:

```bash
go get github.com/aleksei-kislov/http-retry-check@v0.1.1
```

Use Go 1.26 or later in your module:

```bash
go env GOVERSION
```

Your module and local Go settings choose the toolchain. Repository CI uses Go
1.26.6; see [source development](source-development.md#toolchains) to use the
same version.

## Add the test helper

Start with an explicit proxy-free HTTP/1 transport. This avoids accidentally
testing an ambient proxy or negotiating another protocol:

```go
package clienttest

import (
	"net/http"
	"testing"

	httpchecktest "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1/testing"
)

func TestHTTPRetryScenarios(t *testing.T) {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	transport := &http.Transport{
		Proxy:             nil,
		Protocols:         protocols,
		DisableKeepAlives: true,
	}
	t.Cleanup(transport.CloseIdleConnections)

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) != 0 && request.URL.Host != via[0].URL.Host {
				request.Header.Del("Authorization")
			}
			return nil
		},
	}

	httpchecktest.Check(t, client)
}
```

Replace the client construction with the retry setup used by your project. Keep
cleanup explicit, and avoid sharing mutable global client state with parallel
tests. The suite does not close or reconfigure your client.

Go's default redirect policy can copy an `Authorization` header when a
redirect keeps the same hostname but changes the port. HTTP Retry Check treats
that port change as a different controlled origin. The `CheckRedirect` policy
above compares `URL.Host` (including the port) and removes the credential
before following. The [complete example](../../examples/httpcheck/v1/http_retry_test.go)
and the Go scaffold created by [`http init`](../reference/cli.md#http-init) use
the same policy. Refusing the redirect with `http.ErrUseLastResponse` is also a
positive policy under the [scenario semantics](../reference/semantics.md).

Run the test without the Go test cache:

```bash
go mod tidy
go test -count=1 -run '^TestHTTPRetryScenarios$' -v ./...
```

The helper passes only when all six rows and the overall result are
`no_unsafe_behavior_observed`. It logs the scenario result and any findings.
`unsafe_behavior_observed` and `inconclusive` both fail the test.
The [finding-code reference](../reference/semantics.md#finding-code-reference)
maps those human-readable messages to the codes stored in canonical JSON.

## Work with the result directly

Use the root package when you need the six-row result in your own code:

```go
import httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"

result, err := httpcheck.Run(t.Context(), client)
if err != nil {
	return err
}
if err := httpcheck.Validate(result); err != nil {
	return err
}
```

In a Go test, `t.Context()` supplies the active context. `Run` requires a
non-nil active context and client. `Validate` performs no I/O. It recalculates
the findings, each row, and the overall result from the recorded observations.
The context bounds the suite's own work, but `Run` still waits for your
`Doer.Do` call to return. A custom `Doer` or `RoundTripper` that ignores the
request context can therefore block the run after cancellation.

## Produce deterministic evidence

The reporting package creates canonical JSON, JUnit XML, GitHub Markdown, and
the four-file artifact in memory:

```go
import (
	"os"

	httpreport "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1/report"
)

reportValue, err := httpreport.New(result)
if err != nil {
	return err
}
canonicalJSON, err := httpreport.Encode(reportValue)
if err != nil {
	return err
}
junit, err := httpreport.JUnit(reportValue)
if err != nil {
	return err
}
summary, err := httpreport.GitHubSummary(reportValue)
if err != nil {
	return err
}
artifact, err := httpreport.BuildArtifact(reportValue)
if err != nil {
	return err
}
if err := httpreport.ValidateArtifact(artifact); err != nil {
	return err
}
if err := os.WriteFile("report.json", canonicalJSON, 0o600); err != nil {
	return err
}

_, _, _ = canonicalJSON, junit, summary
```

The API returns the files in memory, leaving you to decide where and how to
store them. The CLI can inspect a saved report without running the client
again:

```bash
go run github.com/aleksei-kislov/http-retry-check/cmd/http-retry-check@v0.1.1 \
  http check <report-or-artifact-path>
go run github.com/aleksei-kislov/http-retry-check/cmd/http-retry-check@v0.1.1 \
  http explain <report-path>
```

## Troubleshoot an incomplete run

If a run stops early, fix the first incomplete scenario before looking at later
unavailable rows. A custom `Doer` or `RoundTripper` must close original and
replay bodies on every return path and honor cancellation. A body that is not
closed promptly can delay cleanup; a client call that never returns can block
the run. See
[run sequencing and early stopping](../reference/semantics.md#run-sequencing-and-early-stopping)
for the lifecycle rules and the
[finding-code reference](../reference/semantics.md#finding-code-reference) for
the helper's diagnostic codes.

## Scope

See [evidence meaning](../reference/semantics.md#evidence-meaning) for what a
passing result does and does not establish.
