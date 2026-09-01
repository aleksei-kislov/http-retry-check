package report

import httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"

const maxFindings = 18

type packageError uint8

const (
	invalidReport packageError = iota + 1
	invalidArtifact
)

func (failure packageError) Error() string {
	switch failure {
	case invalidReport:
		return "HTTP retry scenario suite report is invalid"
	case invalidArtifact:
		return "HTTP retry scenario suite artifact is invalid"
	default:
		return "HTTP retry scenario suite evidence failure is invalid"
	}
}

// New validates a suite result and copies it into a report.
func New(result httpcheck.Result) (Report, error) {
	if httpcheck.Validate(result) != nil {
		return Report{}, invalidReport
	}
	value := Report{
		SchemaVersion:       SchemaVersion,
		SuiteIdentity:       SuiteIdentity,
		ExplanationIdentity: ExplanationIdentity,
		ClaimCeiling:        ClaimCeiling,
		Assessment:          result.Assessment,
		AssessmentText:      httpcheck.AssessmentText(result.Assessment),
		Scenarios:           make([]Scenario, len(result.Scenarios)),
	}
	for index, row := range result.Scenarios {
		findings := make([]Finding, len(row.Findings))
		for findingIndex, code := range row.Findings {
			findings[findingIndex] = Finding{Code: code, Text: httpcheck.FindingText(code)}
		}
		value.Scenarios[index] = Scenario{
			Scenario:       row.Scenario,
			ScenarioText:   httpcheck.ScenarioText(row.Scenario),
			Assessment:     row.Assessment,
			AssessmentText: httpcheck.AssessmentText(row.Assessment),
			Observation:    observationFromResult(row.Observation),
			Findings:       findings,
		}
	}
	value.Outcome, _ = outcomeForAssessment(result.Assessment)
	value.Summary, _ = summarize(value.Scenarios)
	if Validate(value) != nil {
		return Report{}, invalidReport
	}
	return value, nil
}

// Validate reconstructs the suite result and verifies the report identities,
// descriptions, counts, assessments, findings, and outcome.
func Validate(value Report) error {
	if value.SchemaVersion != SchemaVersion || value.SuiteIdentity != SuiteIdentity ||
		value.ExplanationIdentity != ExplanationIdentity || value.ClaimCeiling != ClaimCeiling ||
		value.Scenarios == nil || len(value.Scenarios) != 6 {
		return invalidReport
	}
	if value.AssessmentText != httpcheck.AssessmentText(value.Assessment) {
		return invalidReport
	}
	rows := make([]httpcheck.ScenarioResult, len(value.Scenarios))
	for index, row := range value.Scenarios {
		if row.Findings == nil || len(row.Findings) > maxFindings ||
			row.ScenarioText != httpcheck.ScenarioText(row.Scenario) ||
			row.AssessmentText != httpcheck.AssessmentText(row.Assessment) {
			return invalidReport
		}
		findings := make([]httpcheck.FindingCode, len(row.Findings))
		for findingIndex, finding := range row.Findings {
			if finding.Text != httpcheck.FindingText(finding.Code) {
				return invalidReport
			}
			findings[findingIndex] = finding.Code
		}
		rows[index] = httpcheck.ScenarioResult{
			Scenario:    row.Scenario,
			Assessment:  row.Assessment,
			Observation: observationToResult(row.Observation),
			Findings:    findings,
		}
	}
	if httpcheck.Validate(httpcheck.Result{Assessment: value.Assessment, Scenarios: rows}) != nil {
		return invalidReport
	}
	wantOutcome, ok := outcomeForAssessment(value.Assessment)
	if !ok || value.Outcome != wantOutcome {
		return invalidReport
	}
	wantSummary, ok := summarize(value.Scenarios)
	if !ok || value.Summary != wantSummary {
		return invalidReport
	}
	return nil
}

func summarize(rows []Scenario) (Summary, bool) {
	summary := Summary{Scenarios: uint32(len(rows))}
	for _, row := range rows {
		switch row.Assessment {
		case httpcheck.AssessmentNoUnsafeBehaviorObserved:
			summary.Passed++
		case httpcheck.AssessmentUnsafeBehaviorObserved:
			summary.Failed++
		case httpcheck.AssessmentInconclusive:
			summary.Inconclusive++
		default:
			return Summary{}, false
		}
	}
	return summary, true
}

func outcomeForAssessment(assessment httpcheck.Assessment) (Outcome, bool) {
	switch assessment {
	case httpcheck.AssessmentNoUnsafeBehaviorObserved:
		return OutcomePass, true
	case httpcheck.AssessmentUnsafeBehaviorObserved:
		return OutcomeFail, true
	case httpcheck.AssessmentInconclusive:
		return OutcomeInconclusive, true
	default:
		return "", false
	}
}

func observationFromResult(observation httpcheck.Observation) Observation {
	return Observation{
		CaptureComplete: observation.CaptureComplete, AttemptCount: observation.AttemptCount,
		EffectCount: observation.EffectCount, OverlapCount: observation.OverlapCount,
		RetryAfterEffectCount:      observation.RetryAfterEffectCount,
		RetryAfterUnconfirmedCount: observation.RetryAfterUnconfirmedCount,
		RetryBeforeResponseCount:   observation.RetryBeforeResponseCount,
		ResponseAttemptCount:       observation.ResponseAttemptCount,
		ResponseCompleteCount:      observation.ResponseCompleteCount,
		FirstResponseComplete:      observation.FirstResponseComplete,
		DelayCompleteCount:         observation.DelayCompleteCount,
		MethodConsistent:           observation.MethodConsistent,
		DestinationConsistent:      observation.DestinationConsistent,
		BodyConsistent:             observation.BodyConsistent,
		Credential:                 observation.Credential,
		Cleanup:                    observation.Cleanup,
	}
}

func observationToResult(observation Observation) httpcheck.Observation {
	return httpcheck.Observation{
		CaptureComplete: observation.CaptureComplete, AttemptCount: observation.AttemptCount,
		EffectCount: observation.EffectCount, OverlapCount: observation.OverlapCount,
		RetryAfterEffectCount:      observation.RetryAfterEffectCount,
		RetryAfterUnconfirmedCount: observation.RetryAfterUnconfirmedCount,
		RetryBeforeResponseCount:   observation.RetryBeforeResponseCount,
		ResponseAttemptCount:       observation.ResponseAttemptCount,
		ResponseCompleteCount:      observation.ResponseCompleteCount,
		FirstResponseComplete:      observation.FirstResponseComplete,
		DelayCompleteCount:         observation.DelayCompleteCount,
		MethodConsistent:           observation.MethodConsistent,
		DestinationConsistent:      observation.DestinationConsistent,
		BodyConsistent:             observation.BodyConsistent,
		Credential:                 observation.Credential,
		Cleanup:                    observation.Cleanup,
	}
}
