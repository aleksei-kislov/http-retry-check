package retryablehttp_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"
	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

func TestDefaultRetryableClientReplaysAcceptedRequests(t *testing.T) {
	isolateProxyEnvironment(t)
	_, client := newRetryableClient(t)

	result := runSuite(t, client)
	if result.Assessment != httpcheck.AssessmentUnsafeBehaviorObserved {
		t.Fatalf("assessment = %q, want %q", result.Assessment, httpcheck.AssessmentUnsafeBehaviorObserved)
	}
	accepted := scenario(t, result, httpcheck.ScenarioAcceptThenDisconnect)
	if accepted.Assessment != httpcheck.AssessmentUnsafeBehaviorObserved ||
		!contains(accepted.Findings, httpcheck.FindingRetryAfterAcceptedRequest) {
		t.Fatalf("accepted-disconnect row = %q %v", accepted.Assessment, accepted.Findings)
	}
	t.Logf("default aggregate: %s", result.Assessment)
	t.Logf("default finding: %s includes %s", accepted.Scenario, httpcheck.FindingRetryAfterAcceptedRequest)
}

func TestNonIdempotentRetryAndRedirectFixPasses(t *testing.T) {
	isolateProxyEnvironment(t)
	retryClient, client := newRetryableClient(t)
	retryClient.CheckRetry = retryOnlyIdempotentMethods
	retryClient.HTTPClient.CheckRedirect = stripAuthorizationAcrossOrigins
	client.CheckRedirect = stripAuthorizationAcrossOrigins

	result := runSuite(t, methodAwareDoer{client: client})
	if result.Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved {
		t.Fatalf("assessment = %q, want %q", result.Assessment, httpcheck.AssessmentNoUnsafeBehaviorObserved)
	}
	for _, row := range result.Scenarios {
		if row.Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved || len(row.Findings) != 0 {
			t.Fatalf("scenario %q = %q %v", row.Scenario, row.Assessment, row.Findings)
		}
	}
	redirect := scenario(t, result, httpcheck.ScenarioCrossOriginRedirectCredentials)
	if redirect.Observation.Credential != httpcheck.CredentialAbsentAtTarget {
		t.Fatalf("redirect credential = %q, want %q", redirect.Observation.Credential, httpcheck.CredentialAbsentAtTarget)
	}
	t.Logf("fixed aggregate: %s", result.Assessment)
}

type requestMethodKey struct{}

type methodAwareDoer struct {
	client *http.Client
}

func (doer methodAwareDoer) Do(request *http.Request) (*http.Response, error) {
	defer request.Body.Close()
	ctx := context.WithValue(request.Context(), requestMethodKey{}, request.Method)
	return doer.client.Do(request.WithContext(ctx))
}

func retryOnlyIdempotentMethods(
	ctx context.Context,
	response *http.Response,
	err error,
) (bool, error) {
	retry, policyErr := retryablehttp.DefaultRetryPolicy(ctx, response, err)
	if !retry || policyErr != nil {
		return retry, policyErr
	}
	method, _ := ctx.Value(requestMethodKey{}).(string)
	if !idempotentMethod(method) {
		return false, nil
	}
	return true, nil
}

func idempotentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace,
		http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

func stripAuthorizationAcrossOrigins(request *http.Request, via []*http.Request) error {
	if len(via) != 0 && !sameOrigin(via[0].URL, request.URL) {
		request.Header.Del("Authorization")
	}
	return nil
}

func sameOrigin(left, right *url.URL) bool {
	return left != nil && right != nil &&
		strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Host, right.Host)
}

func newRetryableClient(t *testing.T) (*retryablehttp.Client, *http.Client) {
	t.Helper()
	retryClient := retryablehttp.NewClient()
	retryClient.Logger = nil
	client := retryClient.StandardClient()
	t.Cleanup(func() {
		client.CloseIdleConnections()
		retryClient.HTTPClient.CloseIdleConnections()
	})
	return retryClient, client
}

func isolateProxyEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "all_proxy", "no_proxy",
	} {
		t.Setenv(name, "")
	}
}

func runSuite(t *testing.T, doer httpcheck.Doer) httpcheck.Result {
	t.Helper()
	result, err := httpcheck.Run(t.Context(), doer)
	if err != nil {
		t.Fatalf("run HTTP Retry Check: %v", err)
	}
	if err := httpcheck.Validate(result); err != nil {
		t.Fatalf("validate HTTP Retry Check result: %v", err)
	}
	if len(result.Scenarios) != 6 {
		t.Fatalf("scenario count = %d, want 6", len(result.Scenarios))
	}
	return result
}

func scenario(t *testing.T, result httpcheck.Result, wanted httpcheck.ScenarioID) httpcheck.ScenarioResult {
	t.Helper()
	for _, row := range result.Scenarios {
		if row.Scenario == wanted {
			return row
		}
	}
	t.Fatalf("scenario %q is absent", wanted)
	return httpcheck.ScenarioResult{}
}

func contains(findings []httpcheck.FindingCode, wanted httpcheck.FindingCode) bool {
	for _, finding := range findings {
		if finding == wanted {
			return true
		}
	}
	return false
}
