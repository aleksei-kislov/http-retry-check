// Package httpcheck_test contains runnable HTTP Retry Check examples.
package httpcheck_test

import (
	"net/http"
	"testing"

	httpchecktest "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1/testing"
)

// TestHTTPRetryScenarios exercises the six controlled HTTP/1 scenarios with
// an explicit proxy-free standard-library client. Run it with:
//
//	go test -count=1 -run '^TestHTTPRetryScenarios$' -v ./examples/httpcheck/v1
func TestHTTPRetryScenarios(t *testing.T) {
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

	httpchecktest.Check(t, client)
}
