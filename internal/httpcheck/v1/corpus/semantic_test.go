package corpus

// This file is a test-only, language-neutral restatement of the v2 report
// semantic model. It deliberately does not import either Go product binding.

type observation struct {
	CaptureComplete            bool   `json:"capture_complete"`
	AttemptCount               uint32 `json:"attempt_count"`
	AttemptLimit               uint32 `json:"attempt_limit"`
	Protocol                   string `json:"protocol"`
	EffectCount                uint64 `json:"effect_count"`
	OverlapCount               uint32 `json:"overlap_count"`
	RetryAfterEffectCount      uint32 `json:"retry_after_effect_count"`
	RetryAfterUnconfirmedCount uint32 `json:"retry_after_unconfirmed_count"`
	RetryBeforeResponseCount   uint32 `json:"retry_before_response_count"`
	ResponseAttemptCount       uint32 `json:"response_attempt_count"`
	ResponseCompleteCount      uint32 `json:"response_complete_count"`
	FirstResponseComplete      bool   `json:"first_response_complete"`
	DelayCompleteCount         uint32 `json:"delay_complete_count"`
	MethodConsistent           bool   `json:"method_consistent"`
	DestinationConsistent      bool   `json:"destination_consistent"`
	BodyConsistent             bool   `json:"body_consistent"`
	Credential                 string `json:"credential"`
	Cleanup                    string `json:"cleanup"`
}

const (
	assessmentPositive     = "no_unsafe_behavior_observed"
	assessmentUnsafe       = "unsafe_behavior_observed"
	assessmentInconclusive = "inconclusive"
)

var scenarios = [...]string{
	"accept_then_disconnect",
	"disconnect_before_acceptance",
	"changed_body_retry",
	"cross_origin_redirect_credentials",
	"retry_limit",
	"delayed_response",
}

var findingOrder = [...]string{
	"attempt_not_observed",
	"capture_incomplete",
	"response_incomplete",
	"delay_incomplete",
	"attempt_limit_exceeded",
	"retry_before_response",
	"retry_after_accepted_request",
	"retry_after_unconfirmed_acceptance",
	"method_changed",
	"destination_changed",
	"body_changed",
	"credential_not_observed",
	"credential_missing",
	"credential_exposed_at_target",
	"effect_not_observed",
	"effect_limit_exceeded",
	"cleanup_unverified",
	"scenario_incomplete",
}

func validObservation(scenario string, value observation) bool {
	laterAdmissions := uint32(0)
	if value.AttemptCount != 0 {
		laterAdmissions = value.AttemptCount - 1
	}
	if !knownScenario(scenario) || value.AttemptCount > 4 ||
		value.AttemptLimit < 1 || value.AttemptLimit > 3 ||
		value.AttemptCount > value.AttemptLimit+1 ||
		(value.Protocol != "" && value.Protocol != "HTTP/1.1") ||
		(value.AttemptCount == 0 && value.Protocol != "") ||
		(value.CaptureComplete && value.Protocol != "HTTP/1.1") ||
		(scenario == scenarios[3] && value.CaptureComplete && value.AttemptCount > 2 && value.DestinationConsistent) ||
		value.EffectCount > uint64(value.AttemptCount) ||
		(value.AttemptCount != 0 && value.OverlapCount >= value.AttemptCount) ||
		value.RetryAfterEffectCount > laterAdmissions ||
		value.RetryAfterUnconfirmedCount > laterAdmissions ||
		value.RetryAfterEffectCount+value.RetryAfterUnconfirmedCount > laterAdmissions ||
		value.RetryBeforeResponseCount > value.RetryAfterEffectCount ||
		(value.RetryAfterEffectCount != 0 && value.EffectCount == 0) ||
		value.ResponseAttemptCount > value.AttemptCount ||
		value.ResponseCompleteCount > value.ResponseAttemptCount ||
		(value.FirstResponseComplete && value.ResponseCompleteCount == 0) ||
		(value.AttemptCount == 1 && value.ResponseCompleteCount > 0 && !value.FirstResponseComplete) ||
		value.DelayCompleteCount > value.AttemptCount || !knownCredential(value.Credential) ||
		(value.Cleanup != "succeeded" && value.Cleanup != "failed") {
		return false
	}
	if value.OverlapCount != 0 && value.CaptureComplete {
		return false
	}
	if value.CaptureComplete {
		later := uint32(0)
		if value.AttemptCount != 0 {
			later = value.AttemptCount - 1
		}
		switch scenario {
		case scenarios[0], scenarios[2]:
			if value.EffectCount != uint64(value.AttemptCount) || value.RetryAfterEffectCount != later {
				return false
			}
		case scenarios[1]:
			if value.RetryAfterUnconfirmedCount != later {
				return false
			}
		case scenarios[3], scenarios[4]:
			if value.ResponseAttemptCount != value.AttemptCount {
				return false
			}
		case scenarios[5]:
			if value.EffectCount != uint64(value.AttemptCount) ||
				value.DelayCompleteCount != value.AttemptCount ||
				value.ResponseAttemptCount != value.AttemptCount ||
				value.RetryAfterEffectCount != later || value.RetryBeforeResponseCount != 0 {
				return false
			}
		}
	}
	if value.AttemptCount == 0 && (value.EffectCount != 0 || value.OverlapCount != 0 ||
		value.RetryAfterEffectCount != 0 || value.RetryAfterUnconfirmedCount != 0 ||
		value.RetryBeforeResponseCount != 0 || value.ResponseAttemptCount != 0 ||
		value.ResponseCompleteCount != 0 || value.FirstResponseComplete ||
		value.DelayCompleteCount != 0 || !value.MethodConsistent || !value.DestinationConsistent ||
		!value.BodyConsistent || value.Credential != "not_observed") {
		return false
	}
	switch scenario {
	case scenarios[0], scenarios[2]:
		if value.ResponseAttemptCount != 0 || value.ResponseCompleteCount != 0 ||
			value.DelayCompleteCount != 0 || value.RetryAfterUnconfirmedCount != 0 ||
			value.RetryBeforeResponseCount != 0 {
			return false
		}
	case scenarios[1]:
		if value.ResponseAttemptCount != 0 || value.ResponseCompleteCount != 0 ||
			value.DelayCompleteCount != 0 || value.RetryAfterEffectCount != 0 ||
			value.RetryBeforeResponseCount != 0 {
			return false
		}
	case scenarios[3]:
		if value.DelayCompleteCount != 0 || value.RetryAfterEffectCount != 0 ||
			value.RetryAfterUnconfirmedCount != 0 || value.RetryBeforeResponseCount != 0 ||
			value.EffectCount > uint64(value.ResponseAttemptCount) ||
			(value.AttemptCount == 1 && value.EffectCount != 0) ||
			(value.AttemptCount == 1 && (value.Credential == "absent_at_target" || value.Credential == "exposed_at_target")) ||
			(value.Credential == "source_only" && value.EffectCount != 0) ||
			(value.Credential == "absent_at_target" && (value.AttemptCount < 2 || value.EffectCount == 0 ||
				uint64(value.ResponseAttemptCount) < value.EffectCount+1)) ||
			((value.Credential == "source_only" || value.Credential == "missing") && value.ResponseAttemptCount == 0) ||
			(value.Credential == "missing" && value.EffectCount != 0 &&
				uint64(value.ResponseAttemptCount) < value.EffectCount+1) {
			return false
		}
	case scenarios[4]:
		if value.DelayCompleteCount != 0 || value.EffectCount != 0 ||
			value.RetryAfterEffectCount != 0 || value.RetryAfterUnconfirmedCount != 0 ||
			value.RetryBeforeResponseCount != 0 {
			return false
		}
	case scenarios[5]:
		if value.RetryAfterUnconfirmedCount != 0 ||
			uint64(value.DelayCompleteCount) > value.EffectCount ||
			value.ResponseAttemptCount > value.DelayCompleteCount {
			return false
		}
	default:
		return false
	}
	if scenario == scenarios[1] && value.EffectCount != 0 {
		return false
	}
	if scenario != scenarios[3] && (value.Credential == "absent_at_target" || value.Credential == "exposed_at_target") {
		return false
	}
	completeEffect := scenario == scenarios[0] || scenario == scenarios[2] || scenario == scenarios[5]
	completeResponse := scenario == scenarios[3] || scenario == scenarios[4]
	if (value.Credential == "source_only" || value.Credential == "missing") &&
		((completeEffect && value.EffectCount == 0) || (completeResponse && value.ResponseAttemptCount == 0)) {
		return false
	}
	if value.Credential == "not_observed" {
		switch scenario {
		case scenarios[0], scenarios[2], scenarios[5]:
			if value.EffectCount != 0 {
				return false
			}
		case scenarios[1]:
			if value.RetryAfterUnconfirmedCount != 0 || (value.CaptureComplete && value.AttemptCount != 0) {
				return false
			}
		case scenarios[3]:
			if uint64(value.ResponseAttemptCount) > value.EffectCount || value.FirstResponseComplete {
				return false
			}
		case scenarios[4]:
			if value.ResponseAttemptCount != 0 {
				return false
			}
		}
	}
	return true
}

func assess(scenario string, value observation) (string, []string) {
	findings := make([]string, 0, 8)
	unsafe, inconclusive := false, false
	addInconclusive := func(code string) { findings = append(findings, code); inconclusive = true }
	addUnsafe := func(code string) { findings = append(findings, code); unsafe = true }
	if value.AttemptCount == 0 {
		addInconclusive(findingOrder[0])
	}
	if !value.CaptureComplete {
		addInconclusive(findingOrder[1])
	}
	responseIncomplete, delayIncomplete := incompleteStimulus(scenario, value)
	if responseIncomplete {
		addInconclusive(findingOrder[2])
	}
	if delayIncomplete {
		addInconclusive(findingOrder[3])
	}
	if value.AttemptCount > value.AttemptLimit {
		addUnsafe(findingOrder[4])
	}
	if scenario == scenarios[5] && value.RetryBeforeResponseCount != 0 {
		addUnsafe(findingOrder[5])
	}
	if (scenario == scenarios[0] || scenario == scenarios[2] || scenario == scenarios[5]) && value.RetryAfterEffectCount != 0 {
		addUnsafe(findingOrder[6])
	}
	if scenario == scenarios[1] && value.RetryAfterUnconfirmedCount != 0 {
		addUnsafe(findingOrder[7])
	}
	if value.AttemptCount != 0 && !value.MethodConsistent {
		addUnsafe(findingOrder[8])
	}
	if value.AttemptCount != 0 && !value.DestinationConsistent {
		addUnsafe(findingOrder[9])
	}
	if value.AttemptCount != 0 && !value.BodyConsistent {
		addUnsafe(findingOrder[10])
	}
	switch value.Credential {
	case "not_observed":
		if value.AttemptCount != 0 {
			addInconclusive(findingOrder[11])
		}
	case "missing":
		addInconclusive(findingOrder[12])
	case "exposed_at_target":
		addUnsafe(findingOrder[13])
	}
	if effectNotObserved(scenario, value) {
		addInconclusive(findingOrder[14])
	}
	if effectLimitExceeded(value.EffectCount) {
		addUnsafe(findingOrder[15])
	}
	if value.Cleanup != "succeeded" {
		addInconclusive(findingOrder[16])
	}
	if !unsafe && !inconclusive && !positiveTuple(scenario, value) {
		addInconclusive(findingOrder[17])
	}
	if unsafe {
		return assessmentUnsafe, findings
	}
	if inconclusive {
		return assessmentInconclusive, findings
	}
	return assessmentPositive, findings
}

func incompleteStimulus(scenario string, value observation) (bool, bool) {
	if value.AttemptCount == 0 {
		return false, false
	}
	switch scenario {
	case scenarios[3], scenarios[4]:
		return value.ResponseAttemptCount < value.AttemptCount || !value.FirstResponseComplete ||
			value.ResponseCompleteCount < value.AttemptCount, false
	case scenarios[5]:
		return value.ResponseAttemptCount < value.AttemptCount, value.DelayCompleteCount < value.AttemptCount
	default:
		return false, false
	}
}

func effectNotObserved(scenario string, value observation) bool {
	switch scenario {
	case scenarios[0], scenarios[2], scenarios[5]:
		return value.AttemptCount != 0 && value.EffectCount == 0
	case scenarios[3]:
		return value.AttemptCount >= 2 && value.EffectCount == 0
	default:
		return false
	}
}

func effectLimitExceeded(effects uint64) bool {
	return effects > 1
}

func positiveTuple(scenario string, value observation) bool {
	if !value.CaptureComplete || value.OverlapCount != 0 || value.RetryAfterEffectCount != 0 ||
		value.RetryAfterUnconfirmedCount != 0 || value.RetryBeforeResponseCount != 0 ||
		!value.MethodConsistent || !value.DestinationConsistent || !value.BodyConsistent ||
		value.Cleanup != "succeeded" {
		return false
	}
	switch scenario {
	case scenarios[0], scenarios[2]:
		return value.AttemptCount == 1 && value.EffectCount == 1 && value.ResponseAttemptCount == 0 &&
			value.ResponseCompleteCount == 0 && !value.FirstResponseComplete && value.DelayCompleteCount == 0 &&
			value.Credential == "source_only"
	case scenarios[1]:
		return value.AttemptCount == 1 && value.EffectCount == 0 && value.ResponseAttemptCount == 0 &&
			value.ResponseCompleteCount == 0 && !value.FirstResponseComplete && value.DelayCompleteCount == 0 &&
			value.Credential == "source_only"
	case scenarios[3]:
		refused := value.AttemptCount == 1 && value.EffectCount == 0 && value.ResponseAttemptCount == 1 &&
			value.ResponseCompleteCount == 1 && value.FirstResponseComplete && value.Credential == "source_only"
		followed := value.AttemptCount == 2 && value.EffectCount == 1 && value.ResponseAttemptCount == 2 &&
			value.ResponseCompleteCount == 2 && value.FirstResponseComplete && value.Credential == "absent_at_target"
		return value.DelayCompleteCount == 0 && (refused || followed)
	case scenarios[4]:
		return value.AttemptCount >= 1 && value.AttemptCount <= value.AttemptLimit && value.EffectCount == 0 &&
			value.ResponseAttemptCount == value.AttemptCount && value.ResponseCompleteCount == value.AttemptCount &&
			value.FirstResponseComplete && value.DelayCompleteCount == 0 && value.Credential == "source_only"
	case scenarios[5]:
		return value.AttemptCount == 1 && value.EffectCount == 1 && value.ResponseAttemptCount == 1 &&
			value.ResponseCompleteCount <= 1 && value.FirstResponseComplete == (value.ResponseCompleteCount == 1) &&
			value.DelayCompleteCount == 1 && value.Credential == "source_only"
	default:
		return false
	}
}

func knownScenario(value string) bool {
	for _, candidate := range scenarios {
		if value == candidate {
			return true
		}
	}
	return false
}

func knownCredential(value string) bool {
	switch value {
	case "not_observed", "source_only", "absent_at_target", "exposed_at_target", "missing":
		return true
	default:
		return false
	}
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

func equalStrings(left, right []string) bool {
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
