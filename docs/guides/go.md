# Use HTTP Retry Check from Go

Build a client with the retry settings you want to test, then pass it to HTTP
Retry Check. The API accepts a `Doer`, including `*http.Client`. It runs six
HTTP/1 scenarios and can save the result as JSON, JUnit XML, Markdown, or a
four-file artifact.

## Add the module

From the Go module where you want to test your client, add the current release:

```bash
go get github.com/aleksei-kislov/http-retry-check@v0.2.0
```

Use Go 1.25 or later in your module:

```bash
go env GOVERSION
```

Your module and local Go settings choose the toolchain. Repository CI uses Go
1.26.6; see [source development](source-development.md#toolchains) to use the
same version.

## Add the test helper

Test the client configuration your application actually uses first. This
minimal example uses Go's plain standard-library client; replace its
construction with your production transport and retry middleware:

```go
package clienttest

import (
	"net/http"
	"testing"

	httpchecktest "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1/testing"
)

func TestHTTPRetryScenarios(t *testing.T) {
	client := &http.Client{} // Replace with your application's client construction.
	t.Cleanup(client.CloseIdleConnections)

	httpchecktest.Check(t, client)
}
```

Keep cleanup explicit and avoid sharing mutable global client state with
parallel tests. The suite does not close or reconfigure your client.

Go's default redirect policy can copy an `Authorization` header when a
redirect keeps the same hostname but changes the port. HTTP Retry Check treats
that port change as a different controlled origin, so the plain client above
reports `credential_exposed_at_target`. Keep that result if it describes your
production client; do not hide it just to make the test green.

### Optional direct HTTP/1 baseline

After testing production settings, you can remove ambient proxy and protocol
selection from the comparison:

```go
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
```

`Proxy: nil` prevents the transport from using proxy settings such as those
read from the environment by Go's default transport. `Protocols` restricts the
client to HTTP/1, and the redirect hook removes the synthetic credential when
the port changes. Refusing the redirect with `http.ErrUseLastResponse` is also
allowed. This baseline changes the client configuration, so use it to isolate a
production result rather than replace one. The
[runnable passing example](../../examples/httpcheck/v1/http_retry_test.go) uses
this redirect policy without forcing the optional transport settings.

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

## Configure the run

The default settings match in Go and C#:

| Go option | Default | Valid value | What it bounds |
| --- | --- | --- | --- |
| `WithScenarioTimeout` | 5 seconds | More than zero, at most 1 minute | One complete scenario |
| `WithConnectionTimeout` | 2 seconds | More than zero, at most 1 minute | One accepted connection |
| `WithQuietWindow` | 300 milliseconds | More than zero, at most 1 minute | Retry observation after `Do` and request bodies finish |
| `WithAttemptLimit` | 2 | Integer from 1 through 3 | Attempts allowed before `attempt_limit_exceeded` |

Pass only the settings you need to change:

```go
httpchecktest.Check(
	t,
	client,
	httpcheck.WithQuietWindow(500*time.Millisecond),
	httpcheck.WithAttemptLimit(3),
)
```

Import `time` and the root package as
`httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"` for
that example. Options are applied in order, so the last value for a setting
wins. A nil option or invalid final value is rejected before the client runs:
`Run` returns `ErrInvalidCall`, and the testing helper fails the test.

The attempt limit is recorded in every row and evaluated against the observed
attempt count. Raising it changes only the general attempt-limit rule; a replay
after acceptance, an uncertain request, or a pending response remains unsafe.
See [run settings](../reference/semantics.md#run-settings) for the shared rules.

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
Each `Observation` includes the selected `AttemptLimit` and captured `Protocol`;
a complete capture records `HTTP/1.1`.
The context bounds the suite's own work, but `Run` still waits for your
`Doer.Do` call to return. A custom `Doer` or `RoundTripper` that ignores the
request context can therefore block the run after cancellation.
After cleanup, cancellation returns the valid partial result together with
`context.Canceled` or `context.DeadlineExceeded`; a context already cancelled
on entry returns the context error and an empty result.
A panic from your `Doer` or its response body is re-raised after the suite
closes its listeners and settles suite-owned request bodies.

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

Invalid evidence errors support `errors.Is`: use `httpreport.ErrInvalidReport`
for report validation failures and `httpreport.ErrInvalidArtifact` for
artifact validation failures.

The API returns the files in memory, leaving you to decide where and how to
store them. The CLI can inspect a saved report without running the client
again:

```bash
go run github.com/aleksei-kislov/http-retry-check/cmd/http-retry-check@v0.2.0 \
  http check <report-or-artifact-path>
go run github.com/aleksei-kislov/http-retry-check/cmd/http-retry-check@v0.2.0 \
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
