package scenariosuite

import (
	"net/netip"
	"testing"
	"time"
)

func FuzzReadScenarioAttempt(f *testing.F) {
	address := netip.MustParseAddrPort("127.0.0.1:41001")
	f.Add([]byte(rawRequest(
		address,
		"HTTP/1.1",
		[]string{syntheticCredential},
		syntheticBodyText,
		len(syntheticBodyText),
	)))
	f.Add([]byte("POST /case HTTP/1.1\r\nHost: 127.0.0.1:41001\r\n"))
	f.Add([]byte(
		"POST /case HTTP/1.1\r\n" +
			"Host: 127.0.0.1:41001\r\n" +
			"Authorization: " + syntheticCredential + " transformed\r\n" +
			"Transfer-Encoding: chunked\r\nConnection: close\r\n\r\n" +
			"5\r\nhello\r\n0\r\nX-Trailer: value\r\n\r\n",
	))
	f.Add([]byte("not an HTTP request\r\n\r\n"))

	f.Fuzz(func(t *testing.T, wire []byte) {
		// Keep fuzz cases cheap while still covering the complete header parser
		// and malformed body-framing paths. Larger fixed boundary cases have
		// dedicated tests.
		if len(wire) > maxRequestHeaderSize+(64<<10) {
			return
		}
		observed := readScenarioAttempt(
			newMemoryConn(string(wire)),
			address,
			time.Now().Add(time.Second),
		)
		if observed.captureComplete && !observed.complete {
			t.Fatal("capture completed without a complete request")
		}
		if observed.complete && !observed.headersObserved {
			t.Fatal("request completed before its headers were observed")
		}
		if observed.credentialExact && !observed.credentialExposed {
			t.Fatal("an exact credential was not classified as exposed")
		}
		if !observed.headersObserved && (observed.credentialExact || observed.credentialExposed) {
			t.Fatal("credential state was derived without parsed headers")
		}
		if observed.remainingReadBudget < 0 || observed.remainingReadBudget > maxRequestBodySize+1 {
			t.Fatalf("remaining read budget escaped its bound: %d", observed.remainingReadBudget)
		}
	})
}
