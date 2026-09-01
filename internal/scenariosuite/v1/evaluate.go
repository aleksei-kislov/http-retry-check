package scenariosuite

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

// Validate recomputes every finding and assessment from the closed observation
// tuple. It performs no I/O and returns only a fixed sanitized error.
func Validate(result Result) error {
	order := orderedScenarios()
	if len(result.Scenarios) != len(order) || result.Scenarios == nil {
		return ErrInvalidResult
	}
	aggregate := AssessmentNoUnsafeBehaviorObserved
	for index, scenario := range order {
		row := result.Scenarios[index]
		if row.Scenario != scenario || row.Findings == nil || !validObservation(scenario, row.Observation) {
			return ErrInvalidResult
		}
		assessment, findings := assess(scenario, row.Observation)
		if row.Assessment != assessment || !equalFindings(row.Findings, findings) {
			return ErrInvalidResult
		}
		aggregate = combineAssessment(aggregate, assessment)
	}
	if result.Assessment != aggregate {
		return ErrInvalidResult
	}
	return nil
}

// EvaluateObservation validates one closed observation tuple and derives its
// assessment and findings. The public package uses this internal entry point so
// both Go APIs share one semantic evaluator.
func EvaluateObservation(scenario ScenarioID, observation Observation) (ScenarioResult, bool) {
	return newScenarioResult(scenario, observation)
}

func newScenarioResult(scenario ScenarioID, observation Observation) (ScenarioResult, bool) {
	if !validObservation(scenario, observation) {
		return ScenarioResult{}, false
	}
	assessment, findings := assess(scenario, observation)
	return ScenarioResult{
		Scenario: scenario, Assessment: assessment, Observation: observation,
		Findings: findings,
	}, true
}

func newResult(rows []ScenarioResult) (Result, bool) {
	if len(rows) != len(orderedScenarios()) {
		return Result{}, false
	}
	aggregate := AssessmentNoUnsafeBehaviorObserved
	for _, row := range rows {
		aggregate = combineAssessment(aggregate, row.Assessment)
	}
	result := Result{Assessment: aggregate, Scenarios: append([]ScenarioResult(nil), rows...)}
	if Validate(result) != nil {
		return Result{}, false
	}
	return result, true
}

func validObservation(scenario ScenarioID, observation Observation) bool {
	laterAdmissions := uint32(0)
	if observation.AttemptCount != 0 {
		laterAdmissions = observation.AttemptCount - 1
	}
	if !knownScenario(scenario) || observation.AttemptCount > maxObservedAttempts ||
		observation.EffectCount > uint64(observation.AttemptCount) ||
		(observation.AttemptCount != 0 && observation.OverlapCount >= observation.AttemptCount) ||
		observation.RetryAfterEffectCount > laterAdmissions ||
		observation.RetryAfterUnconfirmedCount > laterAdmissions ||
		observation.RetryAfterEffectCount+observation.RetryAfterUnconfirmedCount > laterAdmissions ||
		observation.RetryBeforeResponseCount > observation.RetryAfterEffectCount ||
		(observation.RetryAfterEffectCount != 0 && observation.EffectCount == 0) ||
		observation.ResponseAttemptCount > observation.AttemptCount ||
		observation.ResponseCompleteCount > observation.ResponseAttemptCount ||
		(observation.FirstResponseComplete && observation.ResponseCompleteCount == 0) ||
		(observation.AttemptCount == 1 && observation.ResponseCompleteCount > 0 &&
			!observation.FirstResponseComplete) ||
		observation.DelayCompleteCount > observation.AttemptCount ||
		!knownCredentialState(observation.Credential) || !knownCleanup(observation.Cleanup) {
		return false
	}
	if observation.OverlapCount != 0 && observation.CaptureComplete {
		return false
	}
	if observation.CaptureComplete {
		later := uint32(0)
		if observation.AttemptCount != 0 {
			later = observation.AttemptCount - 1
		}
		switch scenario {
		case ScenarioAcceptThenDisconnect, ScenarioChangedBodyRetry:
			if observation.EffectCount != uint64(observation.AttemptCount) ||
				observation.RetryAfterEffectCount != later {
				return false
			}
		case ScenarioDisconnectBeforeAcceptance:
			if observation.RetryAfterUnconfirmedCount != later {
				return false
			}
		case ScenarioCrossOriginRedirectCredentials, ScenarioRetryLimit:
			if observation.ResponseAttemptCount != observation.AttemptCount {
				return false
			}
		case ScenarioDelayedResponse:
			if observation.EffectCount != uint64(observation.AttemptCount) ||
				observation.DelayCompleteCount != observation.AttemptCount ||
				observation.ResponseAttemptCount != observation.AttemptCount ||
				observation.RetryAfterEffectCount != later || observation.RetryBeforeResponseCount != 0 {
				return false
			}
		}
	}
	if observation.AttemptCount == 0 && (observation.EffectCount != 0 || observation.OverlapCount != 0 ||
		observation.RetryAfterEffectCount != 0 || observation.RetryAfterUnconfirmedCount != 0 ||
		observation.RetryBeforeResponseCount != 0 ||
		observation.ResponseAttemptCount != 0 || observation.ResponseCompleteCount != 0 ||
		observation.FirstResponseComplete || observation.DelayCompleteCount != 0 || !observation.MethodConsistent ||
		!observation.DestinationConsistent || !observation.BodyConsistent ||
		observation.Credential != CredentialNotObserved) {
		return false
	}
	switch scenario {
	case ScenarioAcceptThenDisconnect, ScenarioChangedBodyRetry:
		if observation.ResponseAttemptCount != 0 || observation.ResponseCompleteCount != 0 ||
			observation.DelayCompleteCount != 0 || observation.RetryAfterUnconfirmedCount != 0 ||
			observation.RetryBeforeResponseCount != 0 {
			return false
		}
	case ScenarioDisconnectBeforeAcceptance:
		if observation.ResponseAttemptCount != 0 || observation.ResponseCompleteCount != 0 ||
			observation.DelayCompleteCount != 0 || observation.RetryAfterEffectCount != 0 ||
			observation.RetryBeforeResponseCount != 0 {
			return false
		}
	case ScenarioCrossOriginRedirectCredentials:
		if observation.DelayCompleteCount != 0 || observation.RetryAfterEffectCount != 0 ||
			observation.RetryAfterUnconfirmedCount != 0 || observation.RetryBeforeResponseCount != 0 ||
			observation.EffectCount > uint64(observation.ResponseAttemptCount) ||
			(observation.AttemptCount == 1 && observation.EffectCount != 0) ||
			(observation.AttemptCount == 1 && (observation.Credential == CredentialAbsentAtTarget ||
				observation.Credential == CredentialExposedAtTarget)) ||
			(observation.Credential == CredentialSourceOnly && observation.EffectCount != 0) ||
			(observation.Credential == CredentialAbsentAtTarget &&
				(observation.AttemptCount < 2 || observation.EffectCount == 0 ||
					uint64(observation.ResponseAttemptCount) < observation.EffectCount+1)) ||
			((observation.Credential == CredentialSourceOnly || observation.Credential == CredentialMissing) &&
				observation.ResponseAttemptCount == 0) ||
			(observation.Credential == CredentialMissing && observation.EffectCount != 0 &&
				uint64(observation.ResponseAttemptCount) < observation.EffectCount+1) {
			return false
		}
	case ScenarioRetryLimit:
		if observation.DelayCompleteCount != 0 || observation.EffectCount != 0 ||
			observation.RetryAfterEffectCount != 0 || observation.RetryAfterUnconfirmedCount != 0 ||
			observation.RetryBeforeResponseCount != 0 {
			return false
		}
	case ScenarioDelayedResponse:
		if observation.RetryAfterUnconfirmedCount != 0 ||
			uint64(observation.DelayCompleteCount) > observation.EffectCount ||
			observation.ResponseAttemptCount > observation.DelayCompleteCount {
			return false
		}
	default:
		return false
	}
	if scenario == ScenarioDisconnectBeforeAcceptance && observation.EffectCount != 0 {
		return false
	}
	if scenario != ScenarioCrossOriginRedirectCredentials &&
		(observation.Credential == CredentialAbsentAtTarget || observation.Credential == CredentialExposedAtTarget) {
		return false
	}
	completeEffectScenario := scenario == ScenarioAcceptThenDisconnect ||
		scenario == ScenarioChangedBodyRetry || scenario == ScenarioDelayedResponse
	completeResponseScenario := scenario == ScenarioCrossOriginRedirectCredentials ||
		scenario == ScenarioRetryLimit
	if (observation.Credential == CredentialSourceOnly || observation.Credential == CredentialMissing) &&
		((completeEffectScenario && observation.EffectCount == 0) ||
			(completeResponseScenario && observation.ResponseAttemptCount == 0)) {
		return false
	}
	if !observation.BodyConsistent &&
		((completeEffectScenario && observation.EffectCount == 0) ||
			(completeResponseScenario && observation.ResponseAttemptCount == 0) ||
			(scenario == ScenarioDisconnectBeforeAcceptance &&
				observation.Credential != CredentialSourceOnly && observation.Credential != CredentialMissing)) {
		return false
	}
	if observation.Credential == CredentialNotObserved {
		switch scenario {
		case ScenarioAcceptThenDisconnect, ScenarioChangedBodyRetry, ScenarioDelayedResponse:
			if observation.EffectCount != 0 {
				return false
			}
		case ScenarioDisconnectBeforeAcceptance:
			if observation.RetryAfterUnconfirmedCount != 0 ||
				(observation.CaptureComplete && observation.AttemptCount != 0) {
				return false
			}
		case ScenarioCrossOriginRedirectCredentials:
			if uint64(observation.ResponseAttemptCount) > observation.EffectCount ||
				observation.FirstResponseComplete {
				return false
			}
		case ScenarioRetryLimit:
			if observation.ResponseAttemptCount != 0 {
				return false
			}
		}
	}
	return true
}

func assess(scenario ScenarioID, observation Observation) (Assessment, []FindingCode) {
	findings := make([]FindingCode, 0, 8)
	unsafe := false
	inconclusive := false
	addInconclusive := func(code FindingCode) {
		findings = append(findings, code)
		inconclusive = true
	}
	addUnsafe := func(code FindingCode) {
		findings = append(findings, code)
		unsafe = true
	}

	if observation.AttemptCount == 0 {
		addInconclusive(FindingAttemptNotObserved)
	}
	if !observation.CaptureComplete {
		addInconclusive(FindingCaptureIncomplete)
	}

	responseIncomplete, delayIncomplete := incompleteStimulus(scenario, observation)
	if responseIncomplete {
		addInconclusive(FindingResponseIncomplete)
	}
	if delayIncomplete {
		addInconclusive(FindingDelayIncomplete)
	}
	if exceededAttemptLimit(scenario, observation.AttemptCount) {
		addUnsafe(FindingAttemptLimitExceeded)
	}
	if scenario == ScenarioDelayedResponse && observation.RetryBeforeResponseCount != 0 {
		addUnsafe(FindingRetryBeforeResponse)
	}
	if (scenario == ScenarioAcceptThenDisconnect || scenario == ScenarioChangedBodyRetry ||
		scenario == ScenarioDelayedResponse) && observation.RetryAfterEffectCount != 0 {
		addUnsafe(FindingRetryAfterAcceptedRequest)
	}
	if scenario == ScenarioDisconnectBeforeAcceptance && observation.RetryAfterUnconfirmedCount != 0 {
		addUnsafe(FindingRetryAfterUnconfirmedAcceptance)
	}
	if observation.AttemptCount != 0 && !observation.MethodConsistent {
		addUnsafe(FindingMethodChanged)
	}
	if observation.AttemptCount != 0 && !observation.DestinationConsistent {
		addUnsafe(FindingDestinationChanged)
	}
	if observation.AttemptCount != 0 && !observation.BodyConsistent {
		addUnsafe(FindingBodyChanged)
	}
	switch observation.Credential {
	case CredentialNotObserved:
		if observation.AttemptCount != 0 {
			addInconclusive(FindingCredentialNotObserved)
		}
	case CredentialMissing:
		addInconclusive(FindingCredentialMissing)
	case CredentialExposedAtTarget:
		addUnsafe(FindingCredentialExposedAtTarget)
	}
	if effectNotObserved(scenario, observation) {
		addInconclusive(FindingEffectNotObserved)
	}
	if effectLimitExceeded(scenario, observation.EffectCount) {
		addUnsafe(FindingEffectLimitExceeded)
	}
	if observation.Cleanup != CleanupSucceeded {
		addInconclusive(FindingCleanupUnverified)
	}
	if !unsafe && !inconclusive && !positiveTuple(scenario, observation) {
		addInconclusive(FindingScenarioIncomplete)
	}
	if unsafe {
		return AssessmentUnsafeBehaviorObserved, findings
	}
	if inconclusive {
		return AssessmentInconclusive, findings
	}
	return AssessmentNoUnsafeBehaviorObserved, findings
}

func incompleteStimulus(scenario ScenarioID, observation Observation) (response, delay bool) {
	if observation.AttemptCount == 0 {
		return false, false
	}
	switch scenario {
	case ScenarioCrossOriginRedirectCredentials:
		return observation.ResponseAttemptCount < observation.AttemptCount ||
			!observation.FirstResponseComplete ||
			observation.ResponseCompleteCount < observation.AttemptCount, false
	case ScenarioRetryLimit:
		return observation.ResponseAttemptCount < observation.AttemptCount ||
			!observation.FirstResponseComplete ||
			observation.ResponseCompleteCount < observation.AttemptCount, false
	case ScenarioDelayedResponse:
		return observation.ResponseAttemptCount < observation.AttemptCount,
			observation.DelayCompleteCount < observation.AttemptCount
	default:
		return false, false
	}
}

func exceededAttemptLimit(scenario ScenarioID, attempts uint32) bool {
	return attempts > 2
}

func effectLimitExceeded(scenario ScenarioID, effects uint64) bool {
	switch scenario {
	case ScenarioDisconnectBeforeAcceptance, ScenarioRetryLimit:
		return effects != 0
	default:
		return effects > 1
	}
}

func effectNotObserved(scenario ScenarioID, observation Observation) bool {
	switch scenario {
	case ScenarioAcceptThenDisconnect, ScenarioChangedBodyRetry, ScenarioDelayedResponse:
		return observation.AttemptCount != 0 && observation.EffectCount == 0
	case ScenarioCrossOriginRedirectCredentials:
		return observation.AttemptCount >= 2 && observation.EffectCount == 0
	default:
		return false
	}
}

func positiveTuple(scenario ScenarioID, observation Observation) bool {
	if !observation.CaptureComplete || observation.OverlapCount != 0 ||
		observation.RetryAfterEffectCount != 0 || observation.RetryAfterUnconfirmedCount != 0 ||
		observation.RetryBeforeResponseCount != 0 ||
		!observation.MethodConsistent || !observation.DestinationConsistent ||
		!observation.BodyConsistent || observation.Cleanup != CleanupSucceeded {
		return false
	}
	switch scenario {
	case ScenarioAcceptThenDisconnect, ScenarioChangedBodyRetry:
		return observation.AttemptCount == 1 && observation.EffectCount == 1 &&
			observation.ResponseAttemptCount == 0 && observation.ResponseCompleteCount == 0 &&
			!observation.FirstResponseComplete && observation.DelayCompleteCount == 0 &&
			observation.Credential == CredentialSourceOnly
	case ScenarioDisconnectBeforeAcceptance:
		return observation.AttemptCount == 1 && observation.EffectCount == 0 &&
			observation.ResponseAttemptCount == 0 && observation.ResponseCompleteCount == 0 &&
			!observation.FirstResponseComplete && observation.DelayCompleteCount == 0 &&
			observation.Credential == CredentialSourceOnly
	case ScenarioCrossOriginRedirectCredentials:
		refused := observation.AttemptCount == 1 && observation.EffectCount == 0 &&
			observation.ResponseAttemptCount == 1 && observation.ResponseCompleteCount == 1 &&
			observation.FirstResponseComplete && observation.Credential == CredentialSourceOnly
		followed := observation.AttemptCount == 2 && observation.EffectCount == 1 &&
			observation.ResponseAttemptCount == 2 && observation.ResponseCompleteCount == 2 &&
			observation.FirstResponseComplete && observation.Credential == CredentialAbsentAtTarget
		return observation.DelayCompleteCount == 0 && (refused || followed)
	case ScenarioRetryLimit:
		return observation.AttemptCount >= 1 && observation.AttemptCount <= 2 &&
			observation.EffectCount == 0 && observation.ResponseAttemptCount == observation.AttemptCount &&
			observation.ResponseCompleteCount == observation.AttemptCount &&
			observation.FirstResponseComplete && observation.DelayCompleteCount == 0 &&
			observation.Credential == CredentialSourceOnly
	case ScenarioDelayedResponse:
		return observation.AttemptCount == 1 && observation.EffectCount == 1 &&
			observation.ResponseAttemptCount == 1 && observation.ResponseCompleteCount <= 1 &&
			observation.FirstResponseComplete == (observation.ResponseCompleteCount == 1) &&
			observation.DelayCompleteCount == 1 && observation.Credential == CredentialSourceOnly
	default:
		return false
	}
}

func combineAssessment(left, right Assessment) Assessment {
	if left == AssessmentUnsafeBehaviorObserved || right == AssessmentUnsafeBehaviorObserved {
		return AssessmentUnsafeBehaviorObserved
	}
	if left == AssessmentInconclusive || right == AssessmentInconclusive {
		return AssessmentInconclusive
	}
	return AssessmentNoUnsafeBehaviorObserved
}

func knownScenario(value ScenarioID) bool {
	for _, scenario := range orderedScenarios() {
		if value == scenario {
			return true
		}
	}
	return false
}

func knownCredentialState(value CredentialState) bool {
	switch value {
	case CredentialNotObserved, CredentialSourceOnly, CredentialAbsentAtTarget,
		CredentialExposedAtTarget, CredentialMissing:
		return true
	default:
		return false
	}
}

func knownCleanup(value CleanupState) bool {
	return value == CleanupSucceeded || value == CleanupFailed
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
