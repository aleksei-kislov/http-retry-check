package httpcheck

import "net/http"

// Doer sends the HTTP requests made by Run.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// ScenarioID identifies a scenario in the suite.
type ScenarioID string

const (
	ScenarioAcceptThenDisconnect           ScenarioID = "accept_then_disconnect"
	ScenarioDisconnectBeforeAcceptance     ScenarioID = "disconnect_before_acceptance"
	ScenarioChangedBodyRetry               ScenarioID = "changed_body_retry"
	ScenarioCrossOriginRedirectCredentials ScenarioID = "cross_origin_redirect_credentials"
	ScenarioRetryLimit                     ScenarioID = "retry_limit"
	ScenarioDelayedResponse                ScenarioID = "delayed_response"
)

// Assessment describes the result of a scenario or the complete suite.
type Assessment string

const (
	AssessmentNoUnsafeBehaviorObserved Assessment = "no_unsafe_behavior_observed"
	AssessmentUnsafeBehaviorObserved   Assessment = "unsafe_behavior_observed"
	AssessmentInconclusive             Assessment = "inconclusive"
)

// CleanupState reports whether the suite stopped and cleaned up its resources
// before recording the observation.
type CleanupState string

const (
	CleanupSucceeded CleanupState = "succeeded"
	CleanupFailed    CleanupState = "failed"
)

// CredentialState describes where the suite's synthetic credential was seen.
type CredentialState string

const (
	CredentialNotObserved     CredentialState = "not_observed"
	CredentialSourceOnly      CredentialState = "source_only"
	CredentialAbsentAtTarget  CredentialState = "absent_at_target"
	CredentialExposedAtTarget CredentialState = "exposed_at_target"
	CredentialMissing         CredentialState = "missing"
)

// FindingCode identifies why a scenario did not pass.
type FindingCode string

const (
	FindingAttemptNotObserved              FindingCode = "attempt_not_observed"
	FindingCaptureIncomplete               FindingCode = "capture_incomplete"
	FindingResponseIncomplete              FindingCode = "response_incomplete"
	FindingDelayIncomplete                 FindingCode = "delay_incomplete"
	FindingAttemptLimitExceeded            FindingCode = "attempt_limit_exceeded"
	FindingRetryBeforeResponse             FindingCode = "retry_before_response"
	FindingRetryAfterAcceptedRequest       FindingCode = "retry_after_accepted_request"
	FindingRetryAfterUnconfirmedAcceptance FindingCode = "retry_after_unconfirmed_acceptance"
	FindingMethodChanged                   FindingCode = "method_changed"
	FindingDestinationChanged              FindingCode = "destination_changed"
	FindingBodyChanged                     FindingCode = "body_changed"
	FindingCredentialNotObserved           FindingCode = "credential_not_observed"
	FindingCredentialMissing               FindingCode = "credential_missing"
	FindingCredentialExposedAtTarget       FindingCode = "credential_exposed_at_target"
	FindingEffectNotObserved               FindingCode = "effect_not_observed"
	FindingEffectLimitExceeded             FindingCode = "effect_limit_exceeded"
	FindingCleanupUnverified               FindingCode = "cleanup_unverified"
	FindingScenarioIncomplete              FindingCode = "scenario_incomplete"
)

// Observation records the facts collected during one scenario.
type Observation struct {
	CaptureComplete            bool
	AttemptCount               uint32
	AttemptLimit               uint32
	Protocol                   string
	EffectCount                uint64
	OverlapCount               uint32
	RetryAfterEffectCount      uint32
	RetryAfterUnconfirmedCount uint32
	RetryBeforeResponseCount   uint32
	ResponseAttemptCount       uint32
	ResponseCompleteCount      uint32
	FirstResponseComplete      bool
	DelayCompleteCount         uint32
	MethodConsistent           bool
	DestinationConsistent      bool
	BodyConsistent             bool
	Credential                 CredentialState
	Cleanup                    CleanupState
}

// ScenarioResult contains the observation and assessment for one scenario.
// Findings is non-nil in every valid result.
type ScenarioResult struct {
	Scenario    ScenarioID
	Assessment  Assessment
	Observation Observation
	Findings    []FindingCode
}

// Result contains the overall assessment and all six scenarios in suite order.
type Result struct {
	Assessment Assessment
	Scenarios  []ScenarioResult
}

// RunError identifies a suite failure returned by Run or Validate.
type RunError uint8

const (
	ErrInvalidCall RunError = iota + 1
	ErrSuiteUnavailable
	ErrInternalFailure
	ErrInvalidResult
)

// Error returns the message for this failure type.
func (failure RunError) Error() string {
	switch failure {
	case ErrInvalidCall:
		return "HTTP scenario suite call is invalid"
	case ErrSuiteUnavailable:
		return "HTTP scenario suite is unavailable"
	case ErrInternalFailure:
		return "HTTP scenario suite failed internally"
	case ErrInvalidResult:
		return "HTTP scenario suite result is invalid"
	default:
		return "HTTP scenario suite failure is invalid"
	}
}
