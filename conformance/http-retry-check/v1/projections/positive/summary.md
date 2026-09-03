# HTTP Retry Check report

> This report covers only six local scenarios run in the same process as the client. It does not prove compatibility, production safety, security, isolation, provenance, or behavior at other destinations.

**Result:** `pass` — 6 passed, 0 unsafe, 0 inconclusive.

| Scenario | Result | Findings |
| --- | --- | --- |
| `accept_then_disconnect` | Pass | None |
| `disconnect_before_acceptance` | Pass | None |
| `changed_body_retry` | Pass | None |
| `cross_origin_redirect_credentials` | Pass | None |
| `retry_limit` | Pass | None |
| `delayed_response` | Pass | None |
