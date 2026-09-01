# HTTP Retry Check v1 scenario semantics

This page defines how Go and C# interpret the six v1 scenarios and their
results. It is not a general HTTP retry specification.

## The six scenarios

Every valid result contains these rows in this order:

| Scenario ID | What the server does | Passing behaviour | Unsafe behaviour |
| --- | --- | --- | --- |
| `accept_then_disconnect` | Accept and apply one request, then close without a response. | Exactly one accepted attempt; no replay. | A later attempt after the accepted effect, changed request properties, too many attempts, or more than one effect. |
| `disconnect_before_acceptance` | Read one request, then close while acceptance remains unconfirmed. | Exactly one observed attempt; no replay and no effect. | A later attempt despite uncertain acceptance, changed request properties, too many attempts, or any observed effect. |
| `changed_body_retry` | Accept one request and retain its bytes before closing without a response. | Exactly one accepted attempt; no replay. | A replay after acceptance, including a changed method, destination, or body, too many attempts, or more than one effect. |
| `cross_origin_redirect_credentials` | Return a redirect from one controlled origin to a second. | The client may refuse the redirect, or follow it without forwarding the synthetic credential. | Credential exposure at the target, changed request properties, too many attempts, or more than one target effect. |
| `retry_limit` | Return complete retryable responses. | One or two complete attempts. | More than two attempts or an unexpected effect. |
| `delayed_response` | Accept one request, delay, then attempt a response. | One accepted attempt with completed delay and verified cleanup. | A retry after acceptance, a retry before the response completes, changed request properties, too many attempts, or more than one effect. |

The redirect scenario has two listeners; each other scenario has one. All
seven listeners are fresh and bind distinct endpoints: unique ports on the
literal IPv4 loopback address `127.0.0.1`.

## Run sequencing and early stopping

The runtime reserves all seven listeners before running the scenarios in the
order above. It continues only under these rules:

- Client code that ignores its context or cancellation token can block the run;
  the suite cannot stop code in its own process.
- A scenario is complete only when the request ends in an allowed way, all
  request-body activity has stopped, cleanup succeeds, and neither the scenario
  nor caller context was cancelled. Finishing before the timeout is not enough.
- In the three disconnect scenarios, an error or exception allows the run to
  continue only after the server observed an attempt. An earlier failure stops
  later scenarios. The delayed-response scenario has a separate continuation
  rule after acceptance.
- An unexpected error or exception in the redirect or retry-limit scenario
  makes capture incomplete and stops later invocations.
- When a scenario does not complete, later listeners close and their rows remain
  unavailable and `inconclusive`. Diagnose the first incomplete row; later rows
  did not run independently.

Each origin records at most three attempts and refuses a fourth. This can stop
the run, but findings already recorded, such as `attempt_limit_exceeded`, remain
in the result. A client that retries both transport failures and status responses
may therefore stop before the dedicated retry-limit scenario.

When Go handles `http.ErrUseLastResponse`, `net/http` creates one replay body
before asking `CheckRedirect` whether to follow the `307`. If the hook refuses,
that body is never sent and is left unclosed. After `Do` returns the `307`, the
Go implementation may close exactly one such unused body created by the suite.
This cleanup exception does not apply if a replay started, more than one replay
body was acquired, the scenario or status differs, or the call returned an
error; none of those cases can pass.

## Observation model

Each row records a limited set of facts observed by the suite:

- capture completeness and cleanup state;
- attempt, effect, overlap, and response counts;
- retries after an accepted effect or unconfirmed acceptance;
- retries before a delayed response;
- first-response and delay completion;
- method, destination, and body consistency; and
- where the suite's synthetic credential was observed.

Validation rejects values outside this model and recalculates findings and
assessments from the recorded values. It does not trust calculated fields in a
submitted report.

## Findings and precedence

If a row is both unsafe and incomplete, it is `unsafe_behavior_observed`.
Otherwise, incomplete evidence is `inconclusive`, and only a complete passing
row is `no_unsafe_behavior_observed`. The overall result follows the same order:
unsafe first, then inconclusive, then passing. Unsafe and inconclusive are
different results, but both should fail CI.

### Assessment messages

| Assessment | Fixed text |
| --- | --- |
| `no_unsafe_behavior_observed` | No unsafe HTTP behavior was observed. |
| `unsafe_behavior_observed` | Unsafe HTTP behavior was observed. |
| `inconclusive` | The result is inconclusive. |

### Finding-code reference

The Go helper and the optional C# diagnostic callback print the messages below.
JSON reports store the same message beside its finding code.

| Finding code | Fixed text |
| --- | --- |
| `attempt_not_observed` | No request reached the test server. |
| `capture_incomplete` | The test did not capture a complete scenario result. |
| `response_incomplete` | The required response attempt or completion was not observed. |
| `delay_incomplete` | The test server did not finish its delayed-response phase. |
| `attempt_limit_exceeded` | The client made more than two attempts. |
| `retry_before_response` | The client retried while an earlier response was still pending. |
| `retry_after_accepted_request` | The client retried after the server accepted the request. |
| `retry_after_unconfirmed_acceptance` | The client retried without knowing whether the server accepted the earlier request. |
| `method_changed` | The request method did not match the expected method. |
| `destination_changed` | The request was sent to an unexpected destination. |
| `body_changed` | A request body differed from the original. |
| `credential_not_observed` | The test could not determine where the synthetic credential was sent. |
| `credential_missing` | The original request did not contain exactly one expected synthetic Authorization value. |
| `credential_exposed_at_target` | The synthetic Authorization value reached the redirect target. |
| `effect_not_observed` | The test did not observe the expected server-side effect. |
| `effect_limit_exceeded` | The test server recorded more effects than this scenario allows. |
| `cleanup_unverified` | The test could not verify that all scenario resources were cleaned up. |
| `scenario_incomplete` | The scenario did not collect enough evidence to pass. |

`scenario_incomplete` is kept for compatibility but cannot appear in a valid v1
result. Validation rejects it when the recorded values require more specific
findings.

The source request must contain exactly one `Authorization` value equal to the
suite's synthetic credential. At the redirect target, any `Authorization`
value containing that complete synthetic marker counts as exposure, including
a value with an added prefix or suffix. Unrelated values do not count.

## Evidence meaning

A report contains six rows, fixed explanations, an overall assessment, summary
counts, and a scope statement. The same report always produces the same JSON,
JUnit, Markdown, and artifact files. Artifact hashes detect changed bytes; they
do not prove who ran the test or where it ran.

Every report stores the exact machine-readable `claim_ceiling` scope statement:

> This report covers only six local scenarios run in the same process as the
> client. It does not prove compatibility, production safety, security,
> isolation, provenance, or behavior at other destinations.

This version also excludes TLS, HTTP/2, HTTP/3, real credentials, and failures
outside the six scenarios. A passing result does not certify a client.

## Corpus goldens

The committed reference files test both language implementations; they are not
recordings of the example client. The passing `retry_limit` example uses the
allowed maximum of two attempts, while the example client makes one.
`report.json` stores those counters, but JUnit and Markdown do not. Two passing
reports can therefore have identical `junit.xml` and `summary.md` files.

## Machine-readable references

- [Conformance corpus](../../conformance/http-retry-check/v1)
- [Report schema](../../schemas/v1/http-retry-check-report.schema.json)
- [Artifact-manifest schema](../../schemas/v1/http-retry-check-artifact-manifest.schema.json)
- [Corpus-manifest schema](../../schemas/v1/http-retry-check-conformance-corpus-manifest.schema.json)

These v1 identities describe the evidence model in the `v0.1.0` source
release.
