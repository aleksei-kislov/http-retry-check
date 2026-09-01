# HTTP Retry Check report

> This report covers only six local scenarios run in the same process as the client. It does not prove compatibility, production safety, security, isolation, provenance, or behavior at other destinations.

**Result:** `fail` — 4 passed, 1 unsafe, 1 inconclusive.

| Scenario | Result | Findings |
| --- | --- | --- |
| `accept_then_disconnect` | Pass | None |
| `disconnect_before_acceptance` | Pass | None |
| `changed_body_retry` | Pass | None |
| `cross_origin_redirect_credentials` | Unsafe | `credential_exposed_at_target`: The synthetic Authorization value reached the redirect target. |
| `retry_limit` | Pass | None |
| `delayed_response` | Inconclusive | `attempt_not_observed`: No request reached the test server.<br>`capture_incomplete`: The test did not capture a complete scenario result.<br>`cleanup_unverified`: The test could not verify that all scenario resources were cleaned up. |
