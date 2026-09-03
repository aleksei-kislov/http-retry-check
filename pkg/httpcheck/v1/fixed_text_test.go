package httpcheck_test

import (
	"strings"
	"testing"

	httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"
	httpcheckreport "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1/report"
)

func TestProjectionTextStaysWithinSharedEncoderAlphabet(t *testing.T) {
	values := map[string]string{
		"report schema version":        httpcheckreport.SchemaVersion,
		"report schema ID":             httpcheckreport.SchemaID,
		"suite identity":               httpcheckreport.SuiteIdentity,
		"explanation identity":         httpcheckreport.ExplanationIdentity,
		"claim ceiling":                httpcheckreport.ClaimCeiling,
		"outcome pass":                 string(httpcheckreport.OutcomePass),
		"outcome fail":                 string(httpcheckreport.OutcomeFail),
		"outcome inconclusive":         string(httpcheckreport.OutcomeInconclusive),
		"cleanup succeeded":            string(httpcheck.CleanupSucceeded),
		"cleanup failed":               string(httpcheck.CleanupFailed),
		"credential not observed":      string(httpcheck.CredentialNotObserved),
		"credential source only":       string(httpcheck.CredentialSourceOnly),
		"credential absent at target":  string(httpcheck.CredentialAbsentAtTarget),
		"credential exposed at target": string(httpcheck.CredentialExposedAtTarget),
		"credential missing":           string(httpcheck.CredentialMissing),
	}

	scenarios := []httpcheck.ScenarioID{
		httpcheck.ScenarioAcceptThenDisconnect,
		httpcheck.ScenarioDisconnectBeforeAcceptance,
		httpcheck.ScenarioChangedBodyRetry,
		httpcheck.ScenarioCrossOriginRedirectCredentials,
		httpcheck.ScenarioRetryLimit,
		httpcheck.ScenarioDelayedResponse,
	}
	for _, scenario := range scenarios {
		values["scenario ID "+string(scenario)] = string(scenario)
		values["scenario text "+string(scenario)] = httpcheck.ScenarioText(scenario)
	}

	assessments := []httpcheck.Assessment{
		httpcheck.AssessmentNoUnsafeBehaviorObserved,
		httpcheck.AssessmentUnsafeBehaviorObserved,
		httpcheck.AssessmentInconclusive,
	}
	for _, assessment := range assessments {
		values["assessment "+string(assessment)] = string(assessment)
		values["assessment text "+string(assessment)] = httpcheck.AssessmentText(assessment)
	}

	findings := []httpcheck.FindingCode{
		httpcheck.FindingAttemptNotObserved,
		httpcheck.FindingCaptureIncomplete,
		httpcheck.FindingResponseIncomplete,
		httpcheck.FindingDelayIncomplete,
		httpcheck.FindingAttemptLimitExceeded,
		httpcheck.FindingRetryBeforeResponse,
		httpcheck.FindingRetryAfterAcceptedRequest,
		httpcheck.FindingRetryAfterUnconfirmedAcceptance,
		httpcheck.FindingMethodChanged,
		httpcheck.FindingDestinationChanged,
		httpcheck.FindingBodyChanged,
		httpcheck.FindingCredentialNotObserved,
		httpcheck.FindingCredentialMissing,
		httpcheck.FindingCredentialExposedAtTarget,
		httpcheck.FindingEffectNotObserved,
		httpcheck.FindingEffectLimitExceeded,
		httpcheck.FindingCleanupUnverified,
		httpcheck.FindingScenarioIncomplete,
	}
	for _, finding := range findings {
		values["finding code "+string(finding)] = string(finding)
		values["finding text "+string(finding)] = httpcheck.FindingText(finding)
	}

	reportValue, err := httpcheckreport.New(projectionGuardResult())
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := httpcheckreport.BuildArtifact(reportValue)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range artifact {
		values["artifact name "+file.Name] = file.Name
		values["artifact media type "+file.Name] = file.MediaType
	}

	for name, value := range values {
		for _, character := range value {
			if character < 0x20 || character > 0x7e || strings.ContainsRune("\"'<>&+\\", character) {
				t.Errorf("%s contains a projection-unsafe character %q", name, character)
			}
		}
	}
}

func projectionGuardResult() httpcheck.Result {
	positive := httpcheck.AssessmentNoUnsafeBehaviorObserved
	observation := func(
		attempts uint32,
		effects uint64,
		responseAttempts uint32,
		responseComplete uint32,
		firstResponse bool,
		delayComplete uint32,
		credential httpcheck.CredentialState,
	) httpcheck.Observation {
		return httpcheck.Observation{
			CaptureComplete:       true,
			AttemptCount:          attempts,
			AttemptLimit:          2,
			Protocol:              "HTTP/1.1",
			EffectCount:           effects,
			ResponseAttemptCount:  responseAttempts,
			ResponseCompleteCount: responseComplete,
			FirstResponseComplete: firstResponse,
			DelayCompleteCount:    delayComplete,
			MethodConsistent:      true,
			DestinationConsistent: true,
			BodyConsistent:        true,
			Credential:            credential,
			Cleanup:               httpcheck.CleanupSucceeded,
		}
	}
	row := func(scenario httpcheck.ScenarioID, observed httpcheck.Observation) httpcheck.ScenarioResult {
		return httpcheck.ScenarioResult{
			Scenario: scenario, Assessment: positive, Observation: observed, Findings: []httpcheck.FindingCode{},
		}
	}
	return httpcheck.Result{
		Assessment: positive,
		Scenarios: []httpcheck.ScenarioResult{
			row(httpcheck.ScenarioAcceptThenDisconnect,
				observation(1, 1, 0, 0, false, 0, httpcheck.CredentialSourceOnly)),
			row(httpcheck.ScenarioDisconnectBeforeAcceptance,
				observation(1, 0, 0, 0, false, 0, httpcheck.CredentialSourceOnly)),
			row(httpcheck.ScenarioChangedBodyRetry,
				observation(1, 1, 0, 0, false, 0, httpcheck.CredentialSourceOnly)),
			row(httpcheck.ScenarioCrossOriginRedirectCredentials,
				observation(2, 1, 2, 2, true, 0, httpcheck.CredentialAbsentAtTarget)),
			row(httpcheck.ScenarioRetryLimit,
				observation(2, 0, 2, 2, true, 0, httpcheck.CredentialSourceOnly)),
			row(httpcheck.ScenarioDelayedResponse,
				observation(1, 1, 1, 1, true, 1, httpcheck.CredentialSourceOnly)),
		},
	}
}
