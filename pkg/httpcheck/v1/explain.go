package httpcheck

// ScenarioText returns the fixed explanation for a scenario identifier.
func ScenarioText(scenario ScenarioID) string {
	switch scenario {
	case ScenarioAcceptThenDisconnect:
		return "Tests whether the client retries after the origin accepts a request and disconnects before responding."
	case ScenarioDisconnectBeforeAcceptance:
		return "Tests whether the client retries after the connection ends before request acceptance can be confirmed."
	case ScenarioChangedBodyRetry:
		return "Tests whether a retry changes the body after the origin accepts a request and disconnects."
	case ScenarioCrossOriginRedirectCredentials:
		return "Tests redirect behavior and whether the synthetic credential reaches a different controlled origin."
	case ScenarioRetryLimit:
		return "Tests how many attempts the client makes after a controlled service-unavailable response."
	case ScenarioDelayedResponse:
		return "Tests whether the client retries while an accepted request is waiting for a delayed response."
	default:
		return "Unknown HTTP client scenario."
	}
}

// AssessmentText returns the fixed explanation for a scenario assessment.
func AssessmentText(assessment Assessment) string {
	switch assessment {
	case AssessmentNoUnsafeBehaviorObserved:
		return "No unsafe HTTP behavior was observed."
	case AssessmentUnsafeBehaviorObserved:
		return "Unsafe HTTP behavior was observed."
	case AssessmentInconclusive:
		return "The result is inconclusive."
	default:
		return "Unknown HTTP client scenario assessment."
	}
}

// FindingText returns the fixed explanation for a finding code.
func FindingText(finding FindingCode) string {
	switch finding {
	case FindingAttemptNotObserved:
		return "No request reached the test server."
	case FindingCaptureIncomplete:
		return "The test did not capture a complete scenario result."
	case FindingResponseIncomplete:
		return "The required response attempt or completion was not observed."
	case FindingDelayIncomplete:
		return "The test server did not finish its delayed-response phase."
	case FindingAttemptLimitExceeded:
		return "The client made more than two attempts."
	case FindingRetryBeforeResponse:
		return "The client retried while an earlier response was still pending."
	case FindingRetryAfterAcceptedRequest:
		return "The client retried after the server accepted the request."
	case FindingRetryAfterUnconfirmedAcceptance:
		return "The client retried without knowing whether the server accepted the earlier request."
	case FindingMethodChanged:
		return "The request method did not match the expected method."
	case FindingDestinationChanged:
		return "The request was sent to an unexpected destination."
	case FindingBodyChanged:
		return "A request body differed from the original."
	case FindingCredentialNotObserved:
		return "The test could not determine where the synthetic credential was sent."
	case FindingCredentialMissing:
		return "The original request did not contain exactly one expected synthetic Authorization value."
	case FindingCredentialExposedAtTarget:
		return "The synthetic Authorization value reached the redirect target."
	case FindingEffectNotObserved:
		return "The test did not observe the expected server-side effect."
	case FindingEffectLimitExceeded:
		return "The test server recorded more effects than this scenario allows."
	case FindingCleanupUnverified:
		return "The test could not verify that all scenario resources were cleaned up."
	case FindingScenarioIncomplete:
		return "The scenario did not collect enough evidence to pass."
	default:
		return "Unknown HTTP client scenario finding."
	}
}
