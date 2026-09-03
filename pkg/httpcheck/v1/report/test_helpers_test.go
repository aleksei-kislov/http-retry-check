package report

import (
	"reflect"
	"testing"
)

type sourceObservation struct {
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
	Credential                 string
	Cleanup                    string
}

type sourceScenario struct {
	Scenario    string
	Assessment  string
	Observation sourceObservation
	Findings    []string
}

type sourceResult struct {
	Assessment string
	Scenarios  []sourceScenario
}

func positiveResult() sourceResult {
	rows := []sourceScenario{
		positiveRow("accept_then_disconnect", positiveObservation(1, 1, 0, 0, false, 0, "source_only")),
		positiveRow("disconnect_before_acceptance", positiveObservation(1, 0, 0, 0, false, 0, "source_only")),
		positiveRow("changed_body_retry", positiveObservation(1, 1, 0, 0, false, 0, "source_only")),
		positiveRow("cross_origin_redirect_credentials", positiveObservation(2, 1, 2, 2, true, 0, "absent_at_target")),
		positiveRow("retry_limit", positiveObservation(2, 0, 2, 2, true, 0, "source_only")),
		positiveRow("delayed_response", positiveObservation(1, 1, 1, 1, true, 1, "source_only")),
	}
	return sourceResult{Assessment: "no_unsafe_behavior_observed", Scenarios: rows}
}

func unsafeResult() sourceResult {
	result := positiveResult()
	row := &result.Scenarios[4]
	row.Assessment = "unsafe_behavior_observed"
	row.Observation.AttemptCount = 3
	row.Observation.ResponseAttemptCount = 3
	row.Observation.ResponseCompleteCount = 3
	row.Findings = []string{"attempt_limit_exceeded"}
	result.Assessment = "unsafe_behavior_observed"
	return result
}

func inconclusiveResult() sourceResult {
	result := positiveResult()
	row := &result.Scenarios[0]
	row.Assessment = "inconclusive"
	row.Observation.CaptureComplete = false
	row.Findings = []string{"capture_incomplete"}
	result.Assessment = "inconclusive"
	return result
}

func mixedResult() sourceResult {
	result := unsafeResult()
	row := &result.Scenarios[0]
	row.Assessment = "inconclusive"
	row.Observation.CaptureComplete = false
	row.Findings = []string{"capture_incomplete"}
	return result
}

func unsafeScenarioResult(index int) sourceResult {
	result := positiveResult()
	row := &result.Scenarios[index]
	row.Assessment = "unsafe_behavior_observed"
	switch row.Scenario {
	case "accept_then_disconnect":
		row.Observation.AttemptCount = 2
		row.Observation.EffectCount = 2
		row.Observation.RetryAfterEffectCount = 1
		row.Findings = []string{"retry_after_accepted_request", "effect_limit_exceeded"}
	case "disconnect_before_acceptance":
		row.Observation.AttemptCount = 2
		row.Observation.RetryAfterUnconfirmedCount = 1
		row.Findings = []string{"retry_after_unconfirmed_acceptance"}
	case "changed_body_retry":
		row.Observation.AttemptCount = 2
		row.Observation.EffectCount = 2
		row.Observation.RetryAfterEffectCount = 1
		row.Observation.BodyConsistent = false
		row.Findings = []string{"retry_after_accepted_request", "body_changed", "effect_limit_exceeded"}
	case "cross_origin_redirect_credentials":
		row.Observation.Credential = "exposed_at_target"
		row.Findings = []string{"credential_exposed_at_target"}
	case "retry_limit":
		row.Observation.AttemptCount = 3
		row.Observation.ResponseAttemptCount = 3
		row.Observation.ResponseCompleteCount = 3
		row.Findings = []string{"attempt_limit_exceeded"}
	case "delayed_response":
		row.Observation.AttemptCount = 2
		row.Observation.EffectCount = 2
		row.Observation.RetryAfterEffectCount = 1
		row.Observation.ResponseAttemptCount = 2
		row.Observation.ResponseCompleteCount = 2
		row.Observation.DelayCompleteCount = 2
		row.Findings = []string{"retry_after_accepted_request", "effect_limit_exceeded"}
	}
	result.Assessment = "unsafe_behavior_observed"
	return result
}

func inconclusiveScenarioResult(index int) sourceResult {
	result := positiveResult()
	row := &result.Scenarios[index]
	row.Assessment = "inconclusive"
	row.Observation.CaptureComplete = false
	row.Findings = []string{"capture_incomplete"}
	result.Assessment = "inconclusive"
	return result
}

func positiveRow(scenario string, observation sourceObservation) sourceScenario {
	return sourceScenario{
		Scenario: scenario, Assessment: "no_unsafe_behavior_observed",
		Observation: observation, Findings: []string{},
	}
}

func positiveObservation(
	attempts uint32,
	effects uint64,
	responseAttempts uint32,
	responseComplete uint32,
	firstResponse bool,
	delayComplete uint32,
	credential string,
) sourceObservation {
	return sourceObservation{
		CaptureComplete: true, AttemptCount: attempts, AttemptLimit: 2, Protocol: "HTTP/1.1", EffectCount: effects,
		ResponseAttemptCount: responseAttempts, ResponseCompleteCount: responseComplete,
		FirstResponseComplete: firstResponse, DelayCompleteCount: delayComplete,
		MethodConsistent: true, DestinationConsistent: true, BodyConsistent: true,
		Credential: credential, Cleanup: "succeeded",
	}
}

func newFromSource(result sourceResult) (Report, error) {
	function := reflect.ValueOf(New)
	input := reflect.New(function.Type().In(0)).Elem()
	input.FieldByName("Assessment").SetString(result.Assessment)
	rows := reflect.MakeSlice(input.FieldByName("Scenarios").Type(), len(result.Scenarios), len(result.Scenarios))
	for index, sourceRow := range result.Scenarios {
		row := rows.Index(index)
		row.FieldByName("Scenario").SetString(sourceRow.Scenario)
		row.FieldByName("Assessment").SetString(sourceRow.Assessment)
		setObservation(row.FieldByName("Observation"), sourceRow.Observation)
		findings := reflect.MakeSlice(row.FieldByName("Findings").Type(), len(sourceRow.Findings), len(sourceRow.Findings))
		for findingIndex, finding := range sourceRow.Findings {
			findings.Index(findingIndex).SetString(finding)
		}
		row.FieldByName("Findings").Set(findings)
	}
	input.FieldByName("Scenarios").Set(rows)
	outputs := function.Call([]reflect.Value{input})
	value := outputs[0].Interface().(Report)
	if outputs[1].IsNil() {
		return value, nil
	}
	return value, outputs[1].Interface().(error)
}

func setObservation(destination reflect.Value, source sourceObservation) {
	destination.FieldByName("CaptureComplete").SetBool(source.CaptureComplete)
	destination.FieldByName("AttemptCount").SetUint(uint64(source.AttemptCount))
	destination.FieldByName("AttemptLimit").SetUint(uint64(source.AttemptLimit))
	destination.FieldByName("Protocol").SetString(source.Protocol)
	destination.FieldByName("EffectCount").SetUint(source.EffectCount)
	destination.FieldByName("OverlapCount").SetUint(uint64(source.OverlapCount))
	destination.FieldByName("RetryAfterEffectCount").SetUint(uint64(source.RetryAfterEffectCount))
	destination.FieldByName("RetryAfterUnconfirmedCount").SetUint(uint64(source.RetryAfterUnconfirmedCount))
	destination.FieldByName("RetryBeforeResponseCount").SetUint(uint64(source.RetryBeforeResponseCount))
	destination.FieldByName("ResponseAttemptCount").SetUint(uint64(source.ResponseAttemptCount))
	destination.FieldByName("ResponseCompleteCount").SetUint(uint64(source.ResponseCompleteCount))
	destination.FieldByName("FirstResponseComplete").SetBool(source.FirstResponseComplete)
	destination.FieldByName("DelayCompleteCount").SetUint(uint64(source.DelayCompleteCount))
	destination.FieldByName("MethodConsistent").SetBool(source.MethodConsistent)
	destination.FieldByName("DestinationConsistent").SetBool(source.DestinationConsistent)
	destination.FieldByName("BodyConsistent").SetBool(source.BodyConsistent)
	destination.FieldByName("Credential").SetString(source.Credential)
	destination.FieldByName("Cleanup").SetString(source.Cleanup)
}

func reportObservation(source sourceObservation) Observation {
	result := Observation{
		CaptureComplete: source.CaptureComplete, AttemptCount: source.AttemptCount,
		AttemptLimit: source.AttemptLimit, Protocol: source.Protocol,
		EffectCount: source.EffectCount, OverlapCount: source.OverlapCount,
		RetryAfterEffectCount:      source.RetryAfterEffectCount,
		RetryAfterUnconfirmedCount: source.RetryAfterUnconfirmedCount,
		RetryBeforeResponseCount:   source.RetryBeforeResponseCount,
		ResponseAttemptCount:       source.ResponseAttemptCount,
		ResponseCompleteCount:      source.ResponseCompleteCount,
		FirstResponseComplete:      source.FirstResponseComplete,
		DelayCompleteCount:         source.DelayCompleteCount,
		MethodConsistent:           source.MethodConsistent,
		DestinationConsistent:      source.DestinationConsistent,
		BodyConsistent:             source.BodyConsistent,
	}
	value := reflect.ValueOf(&result).Elem()
	value.FieldByName("Credential").SetString(source.Credential)
	value.FieldByName("Cleanup").SetString(source.Cleanup)
	return result
}

func mustReport(t *testing.T, result sourceResult) Report {
	t.Helper()
	value, err := newFromSource(result)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustArtifact(t *testing.T, result sourceResult) []ArtifactFile {
	t.Helper()
	files, err := BuildArtifact(mustReport(t, result))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func cloneResult(source sourceResult) sourceResult {
	cloned := sourceResult{Assessment: source.Assessment, Scenarios: make([]sourceScenario, len(source.Scenarios))}
	copy(cloned.Scenarios, source.Scenarios)
	for index := range cloned.Scenarios {
		cloned.Scenarios[index].Findings = append([]string{}, source.Scenarios[index].Findings...)
	}
	return cloned
}
