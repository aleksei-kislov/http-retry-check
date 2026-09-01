# HTTP Retry Check report

> This report covers only six local scenarios run in the same process as the client. It does not prove compatibility, production safety, security, isolation, provenance, or behavior at other destinations.

**Result:** `fail` — 5 passed, 1 unsafe, 0 inconclusive.

| Scenario | Result | Findings |
| --- | --- | --- |
| `accept_then_disconnect` | Pass | None |
| `disconnect_before_acceptance` | Pass | None |
| `changed_body_retry` | Pass | None |
| `cross_origin_redirect_credentials` | Unsafe | `credential_exposed_at_target`: The synthetic Authorization value reached the redirect target. |
| `retry_limit` | Pass | None |
| `delayed_response` | Pass | None |
