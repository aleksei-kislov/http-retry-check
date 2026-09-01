# HTTP Retry Check test scaffold

Copy this scaffold into a Go module that depends on github.com/aleksei-kislov/http-retry-check. Run:

    go test -count=1 -run '^TestHTTPRetryScenarios$' -v ./...

The test fails on unsafe or inconclusive results. A green result covers only these six local scenarios; it does not certify the client or run it in a sandbox.
