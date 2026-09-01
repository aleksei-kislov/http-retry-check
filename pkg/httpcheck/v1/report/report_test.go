package report

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

const sanitationMarker = "RAW-V1-REPORT-MARKER-127.0.0.1:49152-Authorization"

func TestNewValidateAndOutcomeSemantics(t *testing.T) {
	tests := []struct {
		name    string
		result  sourceResult
		outcome Outcome
		summary Summary
	}{
		{"positive", positiveResult(), OutcomePass, Summary{Scenarios: 6, Passed: 6}},
		{"unsafe", unsafeResult(), OutcomeFail, Summary{Scenarios: 6, Passed: 5, Failed: 1}},
		{"inconclusive", inconclusiveResult(), OutcomeInconclusive, Summary{Scenarios: 6, Passed: 5, Inconclusive: 1}},
		{"mixed", mixedResult(), OutcomeFail, Summary{Scenarios: 6, Passed: 4, Failed: 1, Inconclusive: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := cloneResult(test.result)
			value, err := newFromSource(test.result)
			if err != nil || Validate(value) != nil {
				t.Fatalf("New/Validate = %#v/%v", value, err)
			}
			if value.Outcome != test.outcome || value.Summary != test.summary ||
				value.SchemaVersion != SchemaVersion || value.SuiteIdentity != SuiteIdentity ||
				value.ExplanationIdentity != ExplanationIdentity || value.ClaimCeiling != ClaimCeiling ||
				string(value.Assessment) != test.result.Assessment ||
				!reflect.DeepEqual(test.result, before) {
				t.Fatalf("report drift: %#v", value)
			}
			for index, row := range value.Scenarios {
				source := test.result.Scenarios[index]
				if string(row.Scenario) != source.Scenario || string(row.Assessment) != source.Assessment ||
					row.Observation != reportObservation(source.Observation) || row.Findings == nil ||
					len(row.Findings) != len(source.Findings) {
					t.Fatalf("row %d drift: %#v", index, row)
				}
			}
			if len(value.Scenarios[0].Findings) != 0 {
				original := test.result.Scenarios[0].Findings[0]
				value.Scenarios[0].Findings[0].Code = "changed"
				if test.result.Scenarios[0].Findings[0] != original {
					t.Fatal("report aliases source findings")
				}
			}
		})
	}
}

func TestValidateRejectsChangesToDerivedFields(t *testing.T) {
	tests := []struct {
		name   string
		unsafe bool
		edit   func(*Report)
	}{
		{"schema", false, func(v *Report) { v.SchemaVersion = sanitationMarker }},
		{"suite", false, func(v *Report) { v.SuiteIdentity = sanitationMarker }},
		{"explanations", false, func(v *Report) { v.ExplanationIdentity = sanitationMarker }},
		{"claim", false, func(v *Report) { v.ClaimCeiling = sanitationMarker }},
		{"aggregate", false, func(v *Report) { v.Assessment = "inconclusive" }},
		{"aggregate text", false, func(v *Report) { v.AssessmentText = sanitationMarker }},
		{"outcome", false, func(v *Report) { v.Outcome = OutcomeFail }},
		{"summary scenarios", false, func(v *Report) { v.Summary.Scenarios = 5 }},
		{"summary passed", false, func(v *Report) { v.Summary.Passed = 5 }},
		{"summary failed", false, func(v *Report) { v.Summary.Failed = 1 }},
		{"summary inconclusive", false, func(v *Report) { v.Summary.Inconclusive = 1 }},
		{"nil rows", false, func(v *Report) { v.Scenarios = nil }},
		{"short rows", false, func(v *Report) { v.Scenarios = v.Scenarios[:5] }},
		{"extra rows", false, func(v *Report) { v.Scenarios = append(v.Scenarios, v.Scenarios[5]) }},
		{"row order", false, func(v *Report) { v.Scenarios[0], v.Scenarios[1] = v.Scenarios[1], v.Scenarios[0] }},
		{"scenario", false, func(v *Report) { v.Scenarios[0].Scenario = "unknown" }},
		{"scenario text", false, func(v *Report) { v.Scenarios[0].ScenarioText = sanitationMarker }},
		{"row assessment", false, func(v *Report) { v.Scenarios[0].Assessment = "inconclusive" }},
		{"row text", false, func(v *Report) { v.Scenarios[0].AssessmentText = sanitationMarker }},
		{"observation", false, func(v *Report) { v.Scenarios[0].Observation.EffectCount = 0 }},
		{"nil findings", false, func(v *Report) { v.Scenarios[0].Findings = nil }},
		{"finding code", true, func(v *Report) { v.Scenarios[4].Findings[0].Code = "unknown" }},
		{"finding text", true, func(v *Report) { v.Scenarios[4].Findings[0].Text = sanitationMarker }},
		{"finding missing", true, func(v *Report) { v.Scenarios[4].Findings = v.Scenarios[4].Findings[:0] }},
		{"finding duplicate", true, func(v *Report) { v.Scenarios[4].Findings = append(v.Scenarios[4].Findings, v.Scenarios[4].Findings[0]) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := positiveResult()
			if test.unsafe {
				result = unsafeResult()
			}
			value := mustReport(t, result)
			test.edit(&value)
			if err := Validate(value); err != invalidReport || strings.Contains(err.Error(), sanitationMarker) {
				t.Fatalf("Validate = %v", err)
			}
		})
	}
}

func TestValidateRechecksEveryObservationField(t *testing.T) {
	mutations := []struct {
		name string
		edit func(*Observation)
	}{
		{"capture_complete", func(v *Observation) { v.CaptureComplete = false }},
		{"attempt_count", func(v *Observation) { v.AttemptCount++ }},
		{"effect_count", func(v *Observation) { v.EffectCount = 0 }},
		{"overlap_count", func(v *Observation) { v.OverlapCount = 1 }},
		{"retry_after_effect_count", func(v *Observation) { v.RetryAfterEffectCount = 1 }},
		{"retry_after_unconfirmed_count", func(v *Observation) { v.RetryAfterUnconfirmedCount = 1 }},
		{"retry_before_response_count", func(v *Observation) { v.RetryBeforeResponseCount = 1 }},
		{"response_attempt_count", func(v *Observation) { v.ResponseAttemptCount = 1 }},
		{"response_complete_count", func(v *Observation) { v.ResponseCompleteCount = 1 }},
		{"first_response_complete", func(v *Observation) { v.FirstResponseComplete = true }},
		{"delay_complete_count", func(v *Observation) { v.DelayCompleteCount = 1 }},
		{"method_consistent", func(v *Observation) { v.MethodConsistent = false }},
		{"destination_consistent", func(v *Observation) { v.DestinationConsistent = false }},
		{"body_consistent", func(v *Observation) { v.BodyConsistent = false }},
		{"credential", func(v *Observation) { v.Credential = "missing" }},
		{"cleanup", func(v *Observation) { v.Cleanup = "failed" }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			value := mustReport(t, positiveResult())
			mutation.edit(&value.Scenarios[0].Observation)
			if Validate(value) != invalidReport {
				t.Fatal("field drift accepted")
			}
		})
	}
}

func TestOperationsReturnOnlyTheirDocumentedErrors(t *testing.T) {
	invalidResult := positiveResult()
	invalidResult.Assessment = sanitationMarker
	if value, err := newFromSource(invalidResult); !reflect.DeepEqual(value, Report{}) || err != invalidReport {
		t.Fatalf("New = %#v/%v", value, err)
	}
	invalidValue := mustReport(t, positiveResult())
	invalidValue.ClaimCeiling = sanitationMarker
	if Validate(invalidValue) != invalidReport {
		t.Fatal("Validate accepted invalid report")
	}
	for name, call := range map[string]func() ([]byte, error){
		"Encode":        func() ([]byte, error) { return Encode(invalidValue) },
		"JUnit":         func() ([]byte, error) { return JUnit(invalidValue) },
		"GitHubSummary": func() ([]byte, error) { return GitHubSummary(invalidValue) },
	} {
		t.Run(name, func(t *testing.T) {
			output, err := call()
			if output != nil || err != invalidReport || strings.Contains(fmt.Sprintf("%#v", err), sanitationMarker) {
				t.Fatalf("failure = %q/%v", output, err)
			}
		})
	}
	if value, err := Decode([]byte(sanitationMarker)); !reflect.DeepEqual(value, Report{}) || err != invalidReport {
		t.Fatalf("Decode = %#v/%v", value, err)
	}
	if files, err := BuildArtifact(invalidValue); files != nil || err != invalidArtifact {
		t.Fatalf("BuildArtifact = %#v/%v", files, err)
	}
	if err := ValidateArtifact([]ArtifactFile{{Name: sanitationMarker, Contents: []byte(sanitationMarker)}}); err != invalidArtifact ||
		strings.Contains(err.Error(), sanitationMarker) {
		t.Fatalf("ValidateArtifact = %v", err)
	}
	if invalidReport.Error() != "HTTP retry scenario suite report is invalid" ||
		invalidArtifact.Error() != "HTTP retry scenario suite artifact is invalid" ||
		packageError(0).Error() != "HTTP retry scenario suite evidence failure is invalid" {
		t.Fatal("fixed error text drift")
	}
}

func TestUnsafeAndInconclusiveScenariosNeverPass(t *testing.T) {
	for index := range positiveResult().Scenarios {
		unsafe := mustReport(t, unsafeScenarioResult(index))
		if unsafe.Outcome != OutcomeFail || unsafe.Summary.Failed != 1 ||
			unsafe.Scenarios[index].Assessment != "unsafe_behavior_observed" {
			t.Fatalf("unsafe scenario %d became green", index)
		}
		unsafeJUnit, err := JUnit(unsafe)
		if err != nil || !bytes.Contains(unsafeJUnit, []byte("<failure")) {
			t.Fatalf("unsafe scenario %d JUnit = %q/%v", index, unsafeJUnit, err)
		}
		unsafeMarkdown, err := GitHubSummary(unsafe)
		if err != nil || strings.Count(string(unsafeMarkdown), expectedMarkdownRow(unsafe.Scenarios[index])) != 1 {
			t.Fatalf("unsafe scenario %d Markdown drift", index)
		}

		inconclusive := mustReport(t, inconclusiveScenarioResult(index))
		if inconclusive.Outcome != OutcomeInconclusive || inconclusive.Summary.Inconclusive != 1 ||
			inconclusive.Scenarios[index].Assessment != "inconclusive" {
			t.Fatalf("inconclusive scenario %d became green", index)
		}
		inconclusiveJUnit, err := JUnit(inconclusive)
		if err != nil || !bytes.Contains(inconclusiveJUnit, []byte("<error")) {
			t.Fatalf("inconclusive scenario %d JUnit = %q/%v", index, inconclusiveJUnit, err)
		}
		inconclusiveMarkdown, err := GitHubSummary(inconclusive)
		if err != nil || strings.Count(string(inconclusiveMarkdown), expectedMarkdownRow(inconclusive.Scenarios[index])) != 1 {
			t.Fatalf("inconclusive scenario %d Markdown drift", index)
		}
	}
}

func TestReportOperationsAreDeterministicIndependentAndConcurrent(t *testing.T) {
	result := mixedResult()
	before := cloneResult(result)
	want := mustReport(t, result)
	wantJSON, _ := Encode(want)
	wantJUnit, _ := JUnit(want)
	wantSummary, _ := GitHubSummary(want)
	wantArtifact, _ := BuildArtifact(want)

	const workers = 32
	var wait sync.WaitGroup
	failures := make(chan string, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, err := newFromSource(result)
			jsonBytes, jsonErr := Encode(value)
			junit, junitErr := JUnit(value)
			summary, summaryErr := GitHubSummary(value)
			artifact, artifactErr := BuildArtifact(value)
			if err != nil || jsonErr != nil || junitErr != nil || summaryErr != nil || artifactErr != nil ||
				!reflect.DeepEqual(value, want) || !bytes.Equal(jsonBytes, wantJSON) ||
				!bytes.Equal(junit, wantJUnit) || !bytes.Equal(summary, wantSummary) ||
				!equalArtifacts(artifact, wantArtifact) {
				failures <- "projection drift"
			}
		}()
	}
	wait.Wait()
	close(failures)
	for failure := range failures {
		t.Fatal(failure)
	}
	if !reflect.DeepEqual(result, before) {
		t.Fatal("operations mutated source")
	}

	first, _ := newFromSource(result)
	second, _ := newFromSource(result)
	first.Scenarios[0].Findings[0].Code = "mutated"
	if reflect.DeepEqual(first, second) || Validate(second) != nil {
		t.Fatal("repeated report values alias")
	}
}
