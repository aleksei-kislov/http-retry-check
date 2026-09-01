package httpcheck_test

import (
	"net/http"
	"testing"

	httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"
)

// TestDefaultRedirectPolicyExposesSyntheticCredentialAcrossPorts shows HTTP
// Retry Check detecting Go's default redirect policy forwarding the synthetic
// credential to a different port. The test passes when the suite reports that
// unsafe behavior.
func TestDefaultRedirectPolicyExposesSyntheticCredentialAcrossPorts(t *testing.T) {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	transport := &http.Transport{
		Proxy:             nil,
		Protocols:         protocols,
		DisableKeepAlives: true,
	}
	t.Cleanup(transport.CloseIdleConnections)

	result, err := httpcheck.Run(t.Context(), &http.Client{Transport: transport})
	if err != nil {
		t.Fatalf("run HTTP Retry Check: %v", err)
	}
	if err := httpcheck.Validate(result); err != nil {
		t.Fatalf("validate HTTP Retry Check result: %v", err)
	}
	if result.Assessment != httpcheck.AssessmentUnsafeBehaviorObserved {
		t.Fatalf("assessment = %q, want %q", result.Assessment, httpcheck.AssessmentUnsafeBehaviorObserved)
	}
	if len(result.Scenarios) != 6 {
		t.Fatalf("scenario count = %d, want 6", len(result.Scenarios))
	}

	for index, scenario := range result.Scenarios {
		if index == 3 {
			if scenario.Scenario != httpcheck.ScenarioCrossOriginRedirectCredentials {
				t.Fatalf("scenario[3] = %q, want %q", scenario.Scenario, httpcheck.ScenarioCrossOriginRedirectCredentials)
			}
			if scenario.Assessment != httpcheck.AssessmentUnsafeBehaviorObserved {
				t.Fatalf("redirect assessment = %q, want %q", scenario.Assessment, httpcheck.AssessmentUnsafeBehaviorObserved)
			}
			if len(scenario.Findings) != 1 || scenario.Findings[0] != httpcheck.FindingCredentialExposedAtTarget {
				t.Fatalf("redirect findings = %v, want [%s]", scenario.Findings, httpcheck.FindingCredentialExposedAtTarget)
			}
			if scenario.Observation.Credential != httpcheck.CredentialExposedAtTarget {
				t.Fatalf("redirect credential = %q, want %q", scenario.Observation.Credential, httpcheck.CredentialExposedAtTarget)
			}
			continue
		}
		if scenario.Assessment != httpcheck.AssessmentNoUnsafeBehaviorObserved || len(scenario.Findings) != 0 {
			t.Fatalf("scenario[%d] = assessment %q, findings %v; want positive with no findings", index, scenario.Assessment, scenario.Findings)
		}
	}

	t.Log("HTTP Retry Check caught Go's default redirect policy forwarding the synthetic Authorization value between controlled 127.0.0.1 ports.")
}
