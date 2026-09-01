# HTTP Retry Check report

> This report covers only six local scenarios run in the same process as the client. It does not prove compatibility, production safety, security, isolation, provenance, or behavior at other destinations.

**Result:** `inconclusive` — 5 passed, 0 unsafe, 1 inconclusive.

| Scenario | Result | Findings |
| --- | --- | --- |
| `accept_then_disconnect` | Pass | None |
| `disconnect_before_acceptance` | Pass | None |
| `changed_body_retry` | Pass | None |
| `cross_origin_redirect_credentials` | Pass | None |
| `retry_limit` | Pass | None |
| `delayed_response` | Inconclusive | `attempt_not_observed`: No request reached the test server.<br>`capture_incomplete`: The test did not capture a complete scenario result. |
