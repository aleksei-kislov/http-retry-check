package evidence

const (
	assessmentPositive     = "no_unsafe_behavior_observed"
	assessmentUnsafe       = "unsafe_behavior_observed"
	assessmentInconclusive = "inconclusive"

	cleanupSucceeded = "succeeded"
	cleanupFailed    = "failed"

	credentialNotObserved     = "not_observed"
	credentialSourceOnly      = "source_only"
	credentialAbsentAtTarget  = "absent_at_target"
	credentialExposedAtTarget = "exposed_at_target"
	credentialMissing         = "missing"

	maxObservedAttempts = 3
	maxFindings         = 18
)

const (
	scenarioAcceptThenDisconnect           = "accept_then_disconnect"
	scenarioDisconnectBeforeAcceptance     = "disconnect_before_acceptance"
	scenarioChangedBodyRetry               = "changed_body_retry"
	scenarioCrossOriginRedirectCredentials = "cross_origin_redirect_credentials"
	scenarioRetryLimit                     = "retry_limit"
	scenarioDelayedResponse                = "delayed_response"
)

const (
	findingAttemptNotObserved              = "attempt_not_observed"
	findingCaptureIncomplete               = "capture_incomplete"
	findingResponseIncomplete              = "response_incomplete"
	findingDelayIncomplete                 = "delay_incomplete"
	findingAttemptLimitExceeded            = "attempt_limit_exceeded"
	findingRetryBeforeResponse             = "retry_before_response"
	findingRetryAfterAcceptedRequest       = "retry_after_accepted_request"
	findingRetryAfterUnconfirmedAcceptance = "retry_after_unconfirmed_acceptance"
	findingMethodChanged                   = "method_changed"
	findingDestinationChanged              = "destination_changed"
	findingBodyChanged                     = "body_changed"
	findingCredentialNotObserved           = "credential_not_observed"
	findingCredentialMissing               = "credential_missing"
	findingCredentialExposedAtTarget       = "credential_exposed_at_target"
	findingEffectNotObserved               = "effect_not_observed"
	findingEffectLimitExceeded             = "effect_limit_exceeded"
	findingCleanupUnverified               = "cleanup_unverified"
	findingScenarioIncomplete              = "scenario_incomplete"
)

var canonicalScenarios = [...]string{
	scenarioAcceptThenDisconnect,
	scenarioDisconnectBeforeAcceptance,
	scenarioChangedBodyRetry,
	scenarioCrossOriginRedirectCredentials,
	scenarioRetryLimit,
	scenarioDelayedResponse,
}

// ValidateReport independently reconstructs every derived semantic member.
func ValidateReport(report Report) error {
	if report.SchemaVersion != SchemaVersion || report.SuiteIdentity != SuiteIdentity ||
		report.ExplanationIdentity != ExplanationIdentity || report.ClaimCeiling != ClaimCeiling ||
		report.AssessmentText != assessmentText(report.Assessment) ||
		report.Scenarios == nil || len(report.Scenarios) != len(canonicalScenarios) {
		return invalidReport
	}
	aggregate := assessmentPositive
	wantSummary := Summary{Scenarios: uint32(len(canonicalScenarios))}
	for index, scenario := range canonicalScenarios {
		row := report.Scenarios[index]
		if row.Scenario != scenario || row.ScenarioText != scenarioText(scenario) ||
			row.AssessmentText != assessmentText(row.Assessment) || row.Findings == nil ||
			len(row.Findings) > maxFindings || !validObservation(scenario, row.Observation) {
			return invalidReport
		}
		assessment, findings := assess(scenario, row.Observation)
		if row.Assessment != assessment || len(row.Findings) != len(findings) {
			return invalidReport
		}
		for findingIndex, code := range findings {
			if row.Findings[findingIndex].Code != code ||
				row.Findings[findingIndex].Text != findingText(code) {
				return invalidReport
			}
		}
		aggregate = combineAssessment(aggregate, assessment)
		switch assessment {
		case assessmentPositive:
			wantSummary.Passed++
		case assessmentUnsafe:
			wantSummary.Failed++
		case assessmentInconclusive:
			wantSummary.Inconclusive++
		default:
			return invalidReport
		}
	}
	if report.Assessment != aggregate || report.Summary != wantSummary ||
		report.Outcome != outcomeForAssessment(aggregate) {
		return invalidReport
	}
	return nil
}

func validObservation(scenario string, observation Observation) bool {
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
		(observation.AttemptCount == 1 && observation.ResponseCompleteCount > 0 && !observation.FirstResponseComplete) ||
		observation.DelayCompleteCount > observation.AttemptCount ||
		!knownCredential(observation.Credential) || !knownCleanup(observation.Cleanup) {
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
		case scenarioAcceptThenDisconnect, scenarioChangedBodyRetry:
			if observation.EffectCount != uint64(observation.AttemptCount) ||
				observation.RetryAfterEffectCount != later {
				return false
			}
		case scenarioDisconnectBeforeAcceptance:
			if observation.RetryAfterUnconfirmedCount != later {
				return false
			}
		case scenarioCrossOriginRedirectCredentials, scenarioRetryLimit:
			if observation.ResponseAttemptCount != observation.AttemptCount {
				return false
			}
		case scenarioDelayedResponse:
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
		observation.RetryBeforeResponseCount != 0 || observation.ResponseAttemptCount != 0 ||
		observation.ResponseCompleteCount != 0 || observation.FirstResponseComplete ||
		observation.DelayCompleteCount != 0 || !observation.MethodConsistent ||
		!observation.DestinationConsistent || !observation.BodyConsistent ||
		observation.Credential != credentialNotObserved) {
		return false
	}
	switch scenario {
	case scenarioAcceptThenDisconnect, scenarioChangedBodyRetry:
		if observation.ResponseAttemptCount != 0 || observation.ResponseCompleteCount != 0 ||
			observation.DelayCompleteCount != 0 || observation.RetryAfterUnconfirmedCount != 0 ||
			observation.RetryBeforeResponseCount != 0 {
			return false
		}
	case scenarioDisconnectBeforeAcceptance:
		if observation.ResponseAttemptCount != 0 || observation.ResponseCompleteCount != 0 ||
			observation.DelayCompleteCount != 0 || observation.RetryAfterEffectCount != 0 ||
			observation.RetryBeforeResponseCount != 0 {
			return false
		}
	case scenarioCrossOriginRedirectCredentials:
		if observation.DelayCompleteCount != 0 || observation.RetryAfterEffectCount != 0 ||
			observation.RetryAfterUnconfirmedCount != 0 || observation.RetryBeforeResponseCount != 0 ||
			observation.EffectCount > uint64(observation.ResponseAttemptCount) ||
			(observation.AttemptCount == 1 && observation.EffectCount != 0) ||
			(observation.AttemptCount == 1 && (observation.Credential == credentialAbsentAtTarget ||
				observation.Credential == credentialExposedAtTarget)) ||
			(observation.Credential == credentialSourceOnly && observation.EffectCount != 0) ||
			(observation.Credential == credentialAbsentAtTarget &&
				(observation.AttemptCount < 2 || observation.EffectCount == 0 ||
					uint64(observation.ResponseAttemptCount) < observation.EffectCount+1)) ||
			((observation.Credential == credentialSourceOnly || observation.Credential == credentialMissing) &&
				observation.ResponseAttemptCount == 0) ||
			(observation.Credential == credentialMissing && observation.EffectCount != 0 &&
				uint64(observation.ResponseAttemptCount) < observation.EffectCount+1) {
			return false
		}
	case scenarioRetryLimit:
		if observation.DelayCompleteCount != 0 || observation.EffectCount != 0 ||
			observation.RetryAfterEffectCount != 0 || observation.RetryAfterUnconfirmedCount != 0 ||
			observation.RetryBeforeResponseCount != 0 {
			return false
		}
	case scenarioDelayedResponse:
		if observation.RetryAfterUnconfirmedCount != 0 ||
			uint64(observation.DelayCompleteCount) > observation.EffectCount ||
			observation.ResponseAttemptCount > observation.DelayCompleteCount {
			return false
		}
	default:
		return false
	}
	if scenario == scenarioDisconnectBeforeAcceptance && observation.EffectCount != 0 {
		return false
	}
	if scenario != scenarioCrossOriginRedirectCredentials &&
		(observation.Credential == credentialAbsentAtTarget || observation.Credential == credentialExposedAtTarget) {
		return false
	}
	completeEffectScenario := scenario == scenarioAcceptThenDisconnect || scenario == scenarioChangedBodyRetry ||
		scenario == scenarioDelayedResponse
	completeResponseScenario := scenario == scenarioCrossOriginRedirectCredentials || scenario == scenarioRetryLimit
	if (observation.Credential == credentialSourceOnly || observation.Credential == credentialMissing) &&
		((completeEffectScenario && observation.EffectCount == 0) ||
			(completeResponseScenario && observation.ResponseAttemptCount == 0)) {
		return false
	}
	if !observation.BodyConsistent &&
		((completeEffectScenario && observation.EffectCount == 0) ||
			(completeResponseScenario && observation.ResponseAttemptCount == 0) ||
			(scenario == scenarioDisconnectBeforeAcceptance &&
				observation.Credential != credentialSourceOnly && observation.Credential != credentialMissing)) {
		return false
	}
	if observation.Credential == credentialNotObserved {
		switch scenario {
		case scenarioAcceptThenDisconnect, scenarioChangedBodyRetry, scenarioDelayedResponse:
			if observation.EffectCount != 0 {
				return false
			}
		case scenarioDisconnectBeforeAcceptance:
			if observation.RetryAfterUnconfirmedCount != 0 ||
				(observation.CaptureComplete && observation.AttemptCount != 0) {
				return false
			}
		case scenarioCrossOriginRedirectCredentials:
			if uint64(observation.ResponseAttemptCount) > observation.EffectCount || observation.FirstResponseComplete {
				return false
			}
		case scenarioRetryLimit:
			if observation.ResponseAttemptCount != 0 {
				return false
			}
		}
	}
	return true
}

func assess(scenario string, observation Observation) (string, []string) {
	findings := make([]string, 0, 8)
	unsafeFound := false
	inconclusiveFound := false
	addInconclusive := func(code string) {
		findings = append(findings, code)
		inconclusiveFound = true
	}
	addUnsafe := func(code string) {
		findings = append(findings, code)
		unsafeFound = true
	}
	if observation.AttemptCount == 0 {
		addInconclusive(findingAttemptNotObserved)
	}
	if !observation.CaptureComplete {
		addInconclusive(findingCaptureIncomplete)
	}
	responseIncomplete, delayIncomplete := incompleteStimulus(scenario, observation)
	if responseIncomplete {
		addInconclusive(findingResponseIncomplete)
	}
	if delayIncomplete {
		addInconclusive(findingDelayIncomplete)
	}
	if observation.AttemptCount > 2 {
		addUnsafe(findingAttemptLimitExceeded)
	}
	if scenario == scenarioDelayedResponse && observation.RetryBeforeResponseCount != 0 {
		addUnsafe(findingRetryBeforeResponse)
	}
	if (scenario == scenarioAcceptThenDisconnect || scenario == scenarioChangedBodyRetry ||
		scenario == scenarioDelayedResponse) && observation.RetryAfterEffectCount != 0 {
		addUnsafe(findingRetryAfterAcceptedRequest)
	}
	if scenario == scenarioDisconnectBeforeAcceptance && observation.RetryAfterUnconfirmedCount != 0 {
		addUnsafe(findingRetryAfterUnconfirmedAcceptance)
	}
	if observation.AttemptCount != 0 && !observation.MethodConsistent {
		addUnsafe(findingMethodChanged)
	}
	if observation.AttemptCount != 0 && !observation.DestinationConsistent {
		addUnsafe(findingDestinationChanged)
	}
	if observation.AttemptCount != 0 && !observation.BodyConsistent {
		addUnsafe(findingBodyChanged)
	}
	switch observation.Credential {
	case credentialNotObserved:
		if observation.AttemptCount != 0 {
			addInconclusive(findingCredentialNotObserved)
		}
	case credentialMissing:
		addInconclusive(findingCredentialMissing)
	case credentialExposedAtTarget:
		addUnsafe(findingCredentialExposedAtTarget)
	}
	if effectNotObserved(scenario, observation) {
		addInconclusive(findingEffectNotObserved)
	}
	if effectLimitExceeded(scenario, observation.EffectCount) {
		addUnsafe(findingEffectLimitExceeded)
	}
	if observation.Cleanup != cleanupSucceeded {
		addInconclusive(findingCleanupUnverified)
	}
	if !unsafeFound && !inconclusiveFound && !positiveTuple(scenario, observation) {
		addInconclusive(findingScenarioIncomplete)
	}
	if unsafeFound {
		return assessmentUnsafe, findings
	}
	if inconclusiveFound {
		return assessmentInconclusive, findings
	}
	return assessmentPositive, findings
}

func incompleteStimulus(scenario string, observation Observation) (bool, bool) {
	if observation.AttemptCount == 0 {
		return false, false
	}
	switch scenario {
	case scenarioCrossOriginRedirectCredentials, scenarioRetryLimit:
		return observation.ResponseAttemptCount < observation.AttemptCount ||
			!observation.FirstResponseComplete ||
			observation.ResponseCompleteCount < observation.AttemptCount, false
	case scenarioDelayedResponse:
		return observation.ResponseAttemptCount < observation.AttemptCount,
			observation.DelayCompleteCount < observation.AttemptCount
	default:
		return false, false
	}
}

func effectNotObserved(scenario string, observation Observation) bool {
	switch scenario {
	case scenarioAcceptThenDisconnect, scenarioChangedBodyRetry, scenarioDelayedResponse:
		return observation.AttemptCount != 0 && observation.EffectCount == 0
	case scenarioCrossOriginRedirectCredentials:
		return observation.AttemptCount >= 2 && observation.EffectCount == 0
	default:
		return false
	}
}

func effectLimitExceeded(scenario string, effects uint64) bool {
	if scenario == scenarioDisconnectBeforeAcceptance || scenario == scenarioRetryLimit {
		return effects != 0
	}
	return effects > 1
}

func positiveTuple(scenario string, observation Observation) bool {
	if !observation.CaptureComplete || observation.OverlapCount != 0 ||
		observation.RetryAfterEffectCount != 0 || observation.RetryAfterUnconfirmedCount != 0 ||
		observation.RetryBeforeResponseCount != 0 || !observation.MethodConsistent ||
		!observation.DestinationConsistent || !observation.BodyConsistent || observation.Cleanup != cleanupSucceeded {
		return false
	}
	switch scenario {
	case scenarioAcceptThenDisconnect, scenarioChangedBodyRetry:
		return observation.AttemptCount == 1 && observation.EffectCount == 1 &&
			observation.ResponseAttemptCount == 0 && observation.ResponseCompleteCount == 0 &&
			!observation.FirstResponseComplete && observation.DelayCompleteCount == 0 &&
			observation.Credential == credentialSourceOnly
	case scenarioDisconnectBeforeAcceptance:
		return observation.AttemptCount == 1 && observation.EffectCount == 0 &&
			observation.ResponseAttemptCount == 0 && observation.ResponseCompleteCount == 0 &&
			!observation.FirstResponseComplete && observation.DelayCompleteCount == 0 &&
			observation.Credential == credentialSourceOnly
	case scenarioCrossOriginRedirectCredentials:
		refused := observation.AttemptCount == 1 && observation.EffectCount == 0 &&
			observation.ResponseAttemptCount == 1 && observation.ResponseCompleteCount == 1 &&
			observation.FirstResponseComplete && observation.Credential == credentialSourceOnly
		followed := observation.AttemptCount == 2 && observation.EffectCount == 1 &&
			observation.ResponseAttemptCount == 2 && observation.ResponseCompleteCount == 2 &&
			observation.FirstResponseComplete && observation.Credential == credentialAbsentAtTarget
		return observation.DelayCompleteCount == 0 && (refused || followed)
	case scenarioRetryLimit:
		return observation.AttemptCount >= 1 && observation.AttemptCount <= 2 &&
			observation.EffectCount == 0 && observation.ResponseAttemptCount == observation.AttemptCount &&
			observation.ResponseCompleteCount == observation.AttemptCount && observation.FirstResponseComplete &&
			observation.DelayCompleteCount == 0 && observation.Credential == credentialSourceOnly
	case scenarioDelayedResponse:
		return observation.AttemptCount == 1 && observation.EffectCount == 1 &&
			observation.ResponseAttemptCount == 1 && observation.ResponseCompleteCount <= 1 &&
			observation.FirstResponseComplete == (observation.ResponseCompleteCount == 1) &&
			observation.DelayCompleteCount == 1 && observation.Credential == credentialSourceOnly
	default:
		return false
	}
}

func knownScenario(value string) bool {
	for _, scenario := range canonicalScenarios {
		if value == scenario {
			return true
		}
	}
	return false
}

func knownCredential(value string) bool {
	switch value {
	case credentialNotObserved, credentialSourceOnly, credentialAbsentAtTarget,
		credentialExposedAtTarget, credentialMissing:
		return true
	default:
		return false
	}
}

func knownCleanup(value string) bool {
	return value == cleanupSucceeded || value == cleanupFailed
}

func combineAssessment(left, right string) string {
	if left == assessmentUnsafe || right == assessmentUnsafe {
		return assessmentUnsafe
	}
	if left == assessmentInconclusive || right == assessmentInconclusive {
		return assessmentInconclusive
	}
	return assessmentPositive
}

func outcomeForAssessment(assessment string) string {
	switch assessment {
	case assessmentPositive:
		return OutcomePass
	case assessmentUnsafe:
		return OutcomeFail
	case assessmentInconclusive:
		return OutcomeInconclusive
	default:
		return ""
	}
}

func scenarioText(scenario string) string {
	switch scenario {
	case scenarioAcceptThenDisconnect:
		return "Tests whether the client retries after the origin accepts a request and disconnects before responding."
	case scenarioDisconnectBeforeAcceptance:
		return "Tests whether the client retries after the connection ends before request acceptance can be confirmed."
	case scenarioChangedBodyRetry:
		return "Tests whether a retry changes the body after the origin accepts a request and disconnects."
	case scenarioCrossOriginRedirectCredentials:
		return "Tests redirect behavior and whether the synthetic credential reaches a different controlled origin."
	case scenarioRetryLimit:
		return "Tests how many attempts the client makes after a controlled service-unavailable response."
	case scenarioDelayedResponse:
		return "Tests whether the client retries while an accepted request is waiting for a delayed response."
	default:
		return "Unknown HTTP client scenario."
	}
}

func assessmentText(assessment string) string {
	switch assessment {
	case assessmentPositive:
		return "No unsafe HTTP behavior was observed."
	case assessmentUnsafe:
		return "Unsafe HTTP behavior was observed."
	case assessmentInconclusive:
		return "The result is inconclusive."
	default:
		return "Unknown HTTP client scenario assessment."
	}
}

func findingText(finding string) string {
	switch finding {
	case findingAttemptNotObserved:
		return "No request reached the test server."
	case findingCaptureIncomplete:
		return "The test did not capture a complete scenario result."
	case findingResponseIncomplete:
		return "The required response attempt or completion was not observed."
	case findingDelayIncomplete:
		return "The test server did not finish its delayed-response phase."
	case findingAttemptLimitExceeded:
		return "The client made more than two attempts."
	case findingRetryBeforeResponse:
		return "The client retried while an earlier response was still pending."
	case findingRetryAfterAcceptedRequest:
		return "The client retried after the server accepted the request."
	case findingRetryAfterUnconfirmedAcceptance:
		return "The client retried without knowing whether the server accepted the earlier request."
	case findingMethodChanged:
		return "The request method did not match the expected method."
	case findingDestinationChanged:
		return "The request was sent to an unexpected destination."
	case findingBodyChanged:
		return "A request body differed from the original."
	case findingCredentialNotObserved:
		return "The test could not determine where the synthetic credential was sent."
	case findingCredentialMissing:
		return "The original request did not contain exactly one expected synthetic Authorization value."
	case findingCredentialExposedAtTarget:
		return "The synthetic Authorization value reached the redirect target."
	case findingEffectNotObserved:
		return "The test did not observe the expected server-side effect."
	case findingEffectLimitExceeded:
		return "The test server recorded more effects than this scenario allows."
	case findingCleanupUnverified:
		return "The test could not verify that all scenario resources were cleaned up."
	case findingScenarioIncomplete:
		return "The scenario did not collect enough evidence to pass."
	default:
		return "Unknown HTTP client scenario finding."
	}
}
