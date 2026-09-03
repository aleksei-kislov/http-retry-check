# `go-retryablehttp` adapter

This module pins `github.com/hashicorp/go-retryablehttp` v0.7.8 and runs its
standard `*http.Client` through HTTP Retry Check.

From this directory, run:

```bash
GOTOOLCHAIN=local go test -count=1 -v ./...
```

The important output is:

```text
default aggregate: unsafe_behavior_observed
default finding: accept_then_disconnect includes retry_after_accepted_request
fixed aggregate: no_unsafe_behavior_observed
PASS
```

The first test keeps the library defaults and only disables logging. It clears
proxy environment variables for a repeatable local test; it does not replace
the library's transport. The test passes because it expects HTTP Retry Check to
catch the unsafe replay after an accepted request.

The fixed example retries only idempotent methods, removes `Authorization` when
either retryablehttp's inner client or the returned standard client crosses an
origin, and closes the input body that v0.7.8's round tripper buffers. Those
policies produce six positive rows for this controlled suite. Applications
should decide separately when an idempotency key or other evidence makes a
non-idempotent retry acceptable.
