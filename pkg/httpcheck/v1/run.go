package httpcheck

import (
	"context"

	scenariosuite "github.com/aleksei-kislov/http-retry-check/internal/scenariosuite/v1"
)

// Run executes all six scenarios with the caller-provided Doer.
func Run(parent context.Context, doer Doer) (Result, error) {
	if parent == nil || doer == nil || parent.Err() != nil {
		return Result{}, ErrInvalidCall
	}
	suiteResult, err := scenariosuite.Run(parent, doer)
	if err != nil {
		return Result{}, mapScenarioSuiteRunError(err)
	}
	result, ok := mapScenarioSuiteResult(suiteResult)
	if !ok {
		return Result{}, ErrInternalFailure
	}
	return result, nil
}

func mapScenarioSuiteRunError(err error) RunError {
	switch err {
	case scenariosuite.ErrInvalidCall:
		return ErrInvalidCall
	case scenariosuite.ErrSuiteUnavailable:
		return ErrSuiteUnavailable
	case scenariosuite.ErrInternalFailure:
		return ErrInternalFailure
	case scenariosuite.ErrInvalidResult:
		return ErrInvalidResult
	default:
		return ErrInternalFailure
	}
}

func mapScenarioSuiteResult(source scenariosuite.Result) (Result, bool) {
	if scenariosuite.Validate(source) != nil || source.Scenarios == nil {
		return Result{}, false
	}
	aggregate, ok := mapScenarioSuiteAssessment(source.Assessment)
	if !ok {
		return Result{}, false
	}
	rows := make([]ScenarioResult, len(source.Scenarios))
	for index, suiteRow := range source.Scenarios {
		scenario, scenarioOK := mapScenarioSuiteScenario(suiteRow.Scenario)
		assessment, assessmentOK := mapScenarioSuiteAssessment(suiteRow.Assessment)
		observation, observationOK := mapScenarioSuiteObservation(suiteRow.Observation)
		if !scenarioOK || !assessmentOK || !observationOK || suiteRow.Findings == nil {
			return Result{}, false
		}
		findingCapacity := len(suiteRow.Findings)
		if findingCapacity == 0 {
			findingCapacity = 1
		}
		findings := make([]FindingCode, len(suiteRow.Findings), findingCapacity)
		for findingIndex, suiteFinding := range suiteRow.Findings {
			finding, findingOK := mapScenarioSuiteFinding(suiteFinding)
			if !findingOK {
				return Result{}, false
			}
			findings[findingIndex] = finding
		}
		rows[index] = ScenarioResult{
			Scenario: scenario, Assessment: assessment, Observation: observation, Findings: findings,
		}
	}
	result := Result{Assessment: aggregate, Scenarios: rows}
	if Validate(result) != nil {
		return Result{}, false
	}
	return result, true
}

func mapScenarioSuiteScenario(value scenariosuite.ScenarioID) (ScenarioID, bool) {
	switch value {
	case scenariosuite.ScenarioAcceptThenDisconnect:
		return ScenarioAcceptThenDisconnect, true
	case scenariosuite.ScenarioDisconnectBeforeAcceptance:
		return ScenarioDisconnectBeforeAcceptance, true
	case scenariosuite.ScenarioChangedBodyRetry:
		return ScenarioChangedBodyRetry, true
	case scenariosuite.ScenarioCrossOriginRedirectCredentials:
		return ScenarioCrossOriginRedirectCredentials, true
	case scenariosuite.ScenarioRetryLimit:
		return ScenarioRetryLimit, true
	case scenariosuite.ScenarioDelayedResponse:
		return ScenarioDelayedResponse, true
	default:
		return "", false
	}
}

func mapScenarioSuiteAssessment(value scenariosuite.Assessment) (Assessment, bool) {
	switch value {
	case scenariosuite.AssessmentNoUnsafeBehaviorObserved:
		return AssessmentNoUnsafeBehaviorObserved, true
	case scenariosuite.AssessmentUnsafeBehaviorObserved:
		return AssessmentUnsafeBehaviorObserved, true
	case scenariosuite.AssessmentInconclusive:
		return AssessmentInconclusive, true
	default:
		return "", false
	}
}

func mapScenarioSuiteCredential(value scenariosuite.CredentialState) (CredentialState, bool) {
	switch value {
	case scenariosuite.CredentialNotObserved:
		return CredentialNotObserved, true
	case scenariosuite.CredentialSourceOnly:
		return CredentialSourceOnly, true
	case scenariosuite.CredentialAbsentAtTarget:
		return CredentialAbsentAtTarget, true
	case scenariosuite.CredentialExposedAtTarget:
		return CredentialExposedAtTarget, true
	case scenariosuite.CredentialMissing:
		return CredentialMissing, true
	default:
		return "", false
	}
}

func mapScenarioSuiteCleanup(value scenariosuite.CleanupState) (CleanupState, bool) {
	switch value {
	case scenariosuite.CleanupSucceeded:
		return CleanupSucceeded, true
	case scenariosuite.CleanupFailed:
		return CleanupFailed, true
	default:
		return "", false
	}
}

func mapScenarioSuiteObservation(value scenariosuite.Observation) (Observation, bool) {
	credential, credentialOK := mapScenarioSuiteCredential(value.Credential)
	cleanup, cleanupOK := mapScenarioSuiteCleanup(value.Cleanup)
	if !credentialOK || !cleanupOK {
		return Observation{}, false
	}
	return Observation{
		CaptureComplete:            value.CaptureComplete,
		AttemptCount:               value.AttemptCount,
		EffectCount:                value.EffectCount,
		OverlapCount:               value.OverlapCount,
		RetryAfterEffectCount:      value.RetryAfterEffectCount,
		RetryAfterUnconfirmedCount: value.RetryAfterUnconfirmedCount,
		RetryBeforeResponseCount:   value.RetryBeforeResponseCount,
		ResponseAttemptCount:       value.ResponseAttemptCount,
		ResponseCompleteCount:      value.ResponseCompleteCount,
		FirstResponseComplete:      value.FirstResponseComplete,
		DelayCompleteCount:         value.DelayCompleteCount,
		MethodConsistent:           value.MethodConsistent,
		DestinationConsistent:      value.DestinationConsistent,
		BodyConsistent:             value.BodyConsistent,
		Credential:                 credential,
		Cleanup:                    cleanup,
	}, true
}

func mapScenarioSuiteFinding(value scenariosuite.FindingCode) (FindingCode, bool) {
	switch value {
	case scenariosuite.FindingAttemptNotObserved:
		return FindingAttemptNotObserved, true
	case scenariosuite.FindingCaptureIncomplete:
		return FindingCaptureIncomplete, true
	case scenariosuite.FindingResponseIncomplete:
		return FindingResponseIncomplete, true
	case scenariosuite.FindingDelayIncomplete:
		return FindingDelayIncomplete, true
	case scenariosuite.FindingAttemptLimitExceeded:
		return FindingAttemptLimitExceeded, true
	case scenariosuite.FindingRetryBeforeResponse:
		return FindingRetryBeforeResponse, true
	case scenariosuite.FindingRetryAfterAcceptedRequest:
		return FindingRetryAfterAcceptedRequest, true
	case scenariosuite.FindingRetryAfterUnconfirmedAcceptance:
		return FindingRetryAfterUnconfirmedAcceptance, true
	case scenariosuite.FindingMethodChanged:
		return FindingMethodChanged, true
	case scenariosuite.FindingDestinationChanged:
		return FindingDestinationChanged, true
	case scenariosuite.FindingBodyChanged:
		return FindingBodyChanged, true
	case scenariosuite.FindingCredentialNotObserved:
		return FindingCredentialNotObserved, true
	case scenariosuite.FindingCredentialMissing:
		return FindingCredentialMissing, true
	case scenariosuite.FindingCredentialExposedAtTarget:
		return FindingCredentialExposedAtTarget, true
	case scenariosuite.FindingEffectNotObserved:
		return FindingEffectNotObserved, true
	case scenariosuite.FindingEffectLimitExceeded:
		return FindingEffectLimitExceeded, true
	case scenariosuite.FindingCleanupUnverified:
		return FindingCleanupUnverified, true
	case scenariosuite.FindingScenarioIncomplete:
		return FindingScenarioIncomplete, true
	default:
		return "", false
	}
}
