// Package httpcheck_test contains runnable HTTP Retry Check examples.
package httpcheck_test

import (
	"net/http"
	"testing"

	httpchecktest "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1/testing"
)

// TestHTTPRetryScenarios exercises the six controlled HTTP/1 scenarios with a
// minimal standard-library client and a safe cross-origin redirect policy.
// Replace the client construction with the configuration used by your
// application. Run it with:
//
//	go test -count=1 -run '^TestHTTPRetryScenarios$' -v ./examples/httpcheck/v1
func TestHTTPRetryScenarios(t *testing.T) {
	client := &http.Client{
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) != 0 && request.URL.Host != via[0].URL.Host {
				request.Header.Del("Authorization")
			}
			return nil
		},
	}
	t.Cleanup(client.CloseIdleConnections)

	httpchecktest.Check(t, client)
}
