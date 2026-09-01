package httpcheck

import scenariosuite "github.com/aleksei-kislov/http-retry-check/internal/scenariosuite/v1"

const maxResultFindings = 18

func orderedScenarios() []ScenarioID {
	return []ScenarioID{
		ScenarioAcceptThenDisconnect,
		ScenarioDisconnectBeforeAcceptance,
		ScenarioChangedBodyRetry,
		ScenarioCrossOriginRedirectCredentials,
		ScenarioRetryLimit,
		ScenarioDelayedResponse,
	}
}

// Validate recomputes every finding and assessment from the observations.
// It performs no I/O and returns a RunError when the result is invalid.
func Validate(result Result) error {
	if result.Scenarios == nil || len(result.Scenarios) != len(orderedScenarios()) {
		return ErrInvalidResult
	}
	for _, row := range result.Scenarios {
		if row.Findings == nil || len(row.Findings) > maxResultFindings {
			return ErrInvalidResult
		}
	}
	if scenariosuite.Validate(toScenarioSuiteResult(result)) != nil {
		return ErrInvalidResult
	}
	return nil
}

func assess(scenario ScenarioID, observation Observation) (Assessment, []FindingCode) {
	row, ok := scenariosuite.EvaluateObservation(
		scenariosuite.ScenarioID(scenario),
		toScenarioSuiteObservation(observation),
	)
	if !ok {
		return "", nil
	}
	findings := make([]FindingCode, len(row.Findings))
	for index, finding := range row.Findings {
		findings[index] = FindingCode(finding)
	}
	return Assessment(row.Assessment), findings
}

func toScenarioSuiteResult(source Result) scenariosuite.Result {
	result := scenariosuite.Result{Assessment: scenariosuite.Assessment(source.Assessment)}
	if source.Scenarios == nil {
		return result
	}
	result.Scenarios = make([]scenariosuite.ScenarioResult, len(source.Scenarios))
	for index, row := range source.Scenarios {
		mapped := scenariosuite.ScenarioResult{
			Scenario:    scenariosuite.ScenarioID(row.Scenario),
			Assessment:  scenariosuite.Assessment(row.Assessment),
			Observation: toScenarioSuiteObservation(row.Observation),
		}
		if row.Findings != nil {
			mapped.Findings = make([]scenariosuite.FindingCode, len(row.Findings))
			for findingIndex, finding := range row.Findings {
				mapped.Findings[findingIndex] = scenariosuite.FindingCode(finding)
			}
		}
		result.Scenarios[index] = mapped
	}
	return result
}

func toScenarioSuiteObservation(value Observation) scenariosuite.Observation {
	return scenariosuite.Observation{
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
		Credential:                 scenariosuite.CredentialState(value.Credential),
		Cleanup:                    scenariosuite.CleanupState(value.Cleanup),
	}
}

func equalFindings(left, right []FindingCode) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
