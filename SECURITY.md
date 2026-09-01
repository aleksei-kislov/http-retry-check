# Security policy

## Report privately

Report suspected vulnerabilities through **GitHub Private Vulnerability
Reporting**. Do not publish vulnerability details in an issue, pull request,
or discussion.

Security fixes target the latest tagged release.

## Scope

HTTP Retry Check runs your trusted client in the same process and observes only
its local HTTP/1 test servers. It does not isolate the client or monitor traffic
sent elsewhere, and a passing result is not a security certification.

See [Architecture and limits](docs/architecture.md) for the complete boundary.
