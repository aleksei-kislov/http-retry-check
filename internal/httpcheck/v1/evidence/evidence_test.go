package evidence

import (
	"bytes"
	"strings"
	"testing"
)

func TestIndependentEvidenceCodecCoversAllOutcomes(t *testing.T) {
	tests := []struct {
		name        string
		report      Report
		outcome     string
		assessment  string
		junitMarker string
	}{
		{name: "positive", report: positiveReport(), outcome: OutcomePass, assessment: assessmentPositive},
		{name: "unsafe", report: unsafeReport(), outcome: OutcomeFail, assessment: assessmentUnsafe, junitMarker: "<failure"},
		{name: "inconclusive", report: inconclusiveReport(), outcome: OutcomeInconclusive, assessment: assessmentInconclusive, junitMarker: "<error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateReport(test.report); err != nil {
				t.Fatal(err)
			}
			if test.report.Outcome != test.outcome || test.report.Assessment != test.assessment {
				t.Fatalf("outcome/assessment = %q/%q", test.report.Outcome, test.report.Assessment)
			}
			encoded, err := EncodeReport(test.report)
			if err != nil || len(encoded) == 0 || encoded[len(encoded)-1] != '\n' {
				t.Fatalf("encode = %d bytes/%v", len(encoded), err)
			}
			decoded, err := DecodeReport(encoded)
			if err != nil {
				t.Fatal(err)
			}
			reencoded, err := EncodeReport(decoded)
			if err != nil || !bytes.Equal(encoded, reencoded) {
				t.Fatal("decode/encode was not a byte fixed point")
			}
			junit, err := JUnit(decoded)
			if err != nil || !bytes.HasSuffix(junit, []byte("\n")) ||
				(test.junitMarker != "" && !bytes.Contains(junit, []byte(test.junitMarker))) {
				t.Fatalf("JUnit = %q/%v", junit, err)
			}
			markdown, err := GitHubSummary(decoded)
			if err != nil || !bytes.HasSuffix(markdown, []byte("\n")) ||
				bytes.Contains(markdown, []byte("<failure")) {
				t.Fatalf("Markdown = %q/%v", markdown, err)
			}
			explanation, err := Explain(decoded)
			rowCount := bytes.Count(explanation, []byte("\nPASS ")) +
				bytes.Count(explanation, []byte("\nUNSAFE ")) +
				bytes.Count(explanation, []byte("\nINCONCLUSIVE "))
			if err != nil || !bytes.HasPrefix(explanation, []byte("HTTP Retry Check: "+test.outcome+"\n")) || rowCount != 6 ||
				!bytes.HasSuffix(explanation, []byte("\n")) {
				t.Fatalf("explanation shape = %q/%v", explanation, err)
			}
			files, err := BuildArtifact(decoded)
			if err != nil || ValidateArtifact(files) != nil || len(files) != 4 {
				t.Fatalf("artifact = %d/%v", len(files), err)
			}
		})
	}
}

func TestIncompleteChangedBodyReportRemainsValidUnsafeEvidence(t *testing.T) {
	report := positiveReport()
	observations := make([]Observation, len(report.Scenarios))
	for index := range report.Scenarios {
		observations[index] = report.Scenarios[index].Observation
	}
	observations[2] = Observation{
		CaptureComplete: false, AttemptCount: 1, AttemptLimit: 2,
		MethodConsistent: true, DestinationConsistent: true, BodyConsistent: false,
		Credential: credentialNotObserved, Cleanup: cleanupSucceeded,
	}
	report = reportFromObservations(observations)
	if err := ValidateReport(report); err != nil {
		t.Fatal(err)
	}
	wantFindings := []string{
		findingCaptureIncomplete,
		findingBodyChanged,
		findingCredentialNotObserved,
		findingEffectNotObserved,
	}
	row := report.Scenarios[2]
	if report.Assessment != assessmentUnsafe || report.Outcome != OutcomeFail ||
		report.Summary != (Summary{Scenarios: 6, Passed: 5, Failed: 1}) ||
		row.Assessment != assessmentUnsafe || len(row.Findings) != len(wantFindings) {
		t.Fatalf("incomplete changed-body report = %#v", report)
	}
	for index, code := range wantFindings {
		if row.Findings[index].Code != code || row.Findings[index].Text != findingText(code) {
			t.Fatalf("finding %d = %#v, want %q", index, row.Findings[index], code)
		}
	}
	encoded, err := EncodeReport(report)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeReport(encoded)
	if err != nil || decoded.Assessment != assessmentUnsafe ||
		decoded.Scenarios[2].Assessment != assessmentUnsafe {
		t.Fatalf("decode = %#v/%v", decoded, err)
	}
}

func TestReportSemanticAndCanonicalChangesAreRejected(t *testing.T) {
	base := positiveReport()
	mutations := map[string]func(*Report){
		"schema":         func(report *Report) { report.SchemaVersion = "PRIVATE" },
		"suite":          func(report *Report) { report.SuiteIdentity = "PRIVATE" },
		"explanation":    func(report *Report) { report.ExplanationIdentity = "PRIVATE" },
		"claim":          func(report *Report) { report.ClaimCeiling = "PRIVATE" },
		"aggregate":      func(report *Report) { report.Assessment = assessmentUnsafe },
		"aggregate text": func(report *Report) { report.AssessmentText = "PRIVATE" },
		"outcome":        func(report *Report) { report.Outcome = OutcomeFail },
		"summary":        func(report *Report) { report.Summary.Passed-- },
		"nil rows":       func(report *Report) { report.Scenarios = nil },
		"row order": func(report *Report) {
			report.Scenarios[0], report.Scenarios[1] = report.Scenarios[1], report.Scenarios[0]
		},
		"row text":        func(report *Report) { report.Scenarios[0].ScenarioText = "PRIVATE" },
		"row assessment":  func(report *Report) { report.Scenarios[0].Assessment = assessmentUnsafe },
		"observation":     func(report *Report) { report.Scenarios[0].Observation.AttemptCount = 3 },
		"nil findings":    func(report *Report) { report.Scenarios[0].Findings = nil },
		"unknown finding": func(report *Report) { report.Scenarios[0].Findings = []Finding{{Code: "PRIVATE", Text: "PRIVATE"}} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := cloneReport(base)
			mutate(&candidate)
			if ValidateReport(candidate) == nil {
				t.Fatal("semantic drift was accepted")
			}
			if _, err := EncodeReport(candidate); err == nil || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("encode error = %v", err)
			}
		})
	}

	canonical, err := EncodeReport(base)
	if err != nil {
		t.Fatal(err)
	}
	jsonMutations := map[string][]byte{
		"empty":          nil,
		"missing LF":     canonical[:len(canonical)-1],
		"leading space":  append([]byte(" "), canonical...),
		"trailing data":  append(append([]byte{}, canonical...), 'x'),
		"unknown member": bytes.Replace(canonical, []byte("{\n"), []byte("{\n  \"PRIVATE\": true,\n"), 1),
		"duplicate":      bytes.Replace(canonical, []byte("  \"schema_version\":"), []byte("  \"schema_version\": \"PRIVATE\",\n  \"schema_version\":"), 1),
		"null rows":      bytes.Replace(canonical, []byte("  \"scenarios\": ["), []byte("  \"scenarios\": null"), 1),
		"negative":       bytes.Replace(canonical, []byte("\"attempt_count\": 1"), []byte("\"attempt_count\": -1"), 1),
		"exponent":       bytes.Replace(canonical, []byte("\"attempt_count\": 1"), []byte("\"attempt_count\": 1e0"), 1),
		"invalid UTF-8":  append([]byte{0xff}, canonical...),
		"oversized":      bytes.Repeat([]byte("x"), MaxProjectionBytes+1),
	}
	for name, candidate := range jsonMutations {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeReport(candidate); err == nil || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("decode error = %v", err)
			}
		})
	}
}

func TestArtifactValidationRecomputesBytesAndReturnsIndependentValues(t *testing.T) {
	files, err := BuildArtifact(positiveReport())
	if err != nil {
		t.Fatal(err)
	}
	if ValidateArtifact(files) != nil {
		t.Fatal("fresh artifact was invalid")
	}
	first := files[0].Contents[0]
	files[1].Contents[0] ^= 0xff
	if files[0].Contents[0] != first {
		t.Fatal("artifact files alias")
	}
	if ValidateArtifact(files) == nil {
		t.Fatal("corrupt payload was accepted")
	}

	fresh, err := BuildArtifact(positiveReport())
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func([]ArtifactFile) []ArtifactFile{
		"nil":       func([]ArtifactFile) []ArtifactFile { return nil },
		"missing":   func(files []ArtifactFile) []ArtifactFile { return files[:3] },
		"extra":     func(files []ArtifactFile) []ArtifactFile { return append(files, files[3]) },
		"reordered": func(files []ArtifactFile) []ArtifactFile { files[0], files[1] = files[1], files[0]; return files },
		"name alias": func(files []ArtifactFile) []ArtifactFile {
			files[0].Name = "MANIFEST.JSON"
			return files
		},
		"media": func(files []ArtifactFile) []ArtifactFile { files[2].MediaType = "PRIVATE"; return files },
		"manifest": func(files []ArtifactFile) []ArtifactFile {
			files[0].Contents = bytes.Replace(files[0].Contents, []byte(ArtifactDigestDomain), []byte("PRIVATE"), 1)
			return files
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := cloneArtifact(fresh)
			candidate = mutate(candidate)
			if err := ValidateArtifact(candidate); err == nil || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("artifact error = %v", err)
			}
		})
	}
}

func positiveReport() Report {
	observations := []Observation{
		positiveObservation(scenarioAcceptThenDisconnect),
		positiveObservation(scenarioDisconnectBeforeAcceptance),
		positiveObservation(scenarioChangedBodyRetry),
		positiveObservation(scenarioCrossOriginRedirectCredentials),
		positiveObservation(scenarioRetryLimit),
		positiveObservation(scenarioDelayedResponse),
	}
	return reportFromObservations(observations)
}

func unsafeReport() Report {
	report := positiveReport()
	observations := make([]Observation, len(report.Scenarios))
	for index := range report.Scenarios {
		observations[index] = report.Scenarios[index].Observation
	}
	observations[4].AttemptCount = 3
	observations[4].ResponseAttemptCount = 3
	observations[4].ResponseCompleteCount = 3
	return reportFromObservations(observations)
}

func inconclusiveReport() Report {
	report := positiveReport()
	observations := make([]Observation, len(report.Scenarios))
	for index := range report.Scenarios {
		observations[index] = report.Scenarios[index].Observation
	}
	observations[0] = Observation{
		AttemptLimit: 2, MethodConsistent: true, DestinationConsistent: true, BodyConsistent: true,
		Credential: credentialNotObserved, Cleanup: cleanupSucceeded,
	}
	return reportFromObservations(observations)
}

func reportFromObservations(observations []Observation) Report {
	report := Report{
		SchemaVersion: SchemaVersion, SuiteIdentity: SuiteIdentity,
		ExplanationIdentity: ExplanationIdentity, ClaimCeiling: ClaimCeiling,
		Assessment: assessmentPositive, Scenarios: make([]Scenario, len(canonicalScenarios)),
	}
	for index, scenario := range canonicalScenarios {
		assessment, codes := assess(scenario, observations[index])
		findings := make([]Finding, len(codes))
		for findingIndex, code := range codes {
			findings[findingIndex] = Finding{Code: code, Text: findingText(code)}
		}
		report.Scenarios[index] = Scenario{
			Scenario: scenario, ScenarioText: scenarioText(scenario), Assessment: assessment,
			AssessmentText: assessmentText(assessment), Observation: observations[index], Findings: findings,
		}
		report.Assessment = combineAssessment(report.Assessment, assessment)
	}
	report.AssessmentText = assessmentText(report.Assessment)
	report.Outcome = outcomeForAssessment(report.Assessment)
	report.Summary = Summary{Scenarios: 6}
	for _, row := range report.Scenarios {
		switch row.Assessment {
		case assessmentPositive:
			report.Summary.Passed++
		case assessmentUnsafe:
			report.Summary.Failed++
		case assessmentInconclusive:
			report.Summary.Inconclusive++
		}
	}
	return report
}

func positiveObservation(scenario string) Observation {
	base := Observation{
		CaptureComplete: true, AttemptCount: 1, AttemptLimit: 2, Protocol: "HTTP/1.1", MethodConsistent: true,
		DestinationConsistent: true, BodyConsistent: true,
		Credential: credentialSourceOnly, Cleanup: cleanupSucceeded,
	}
	switch scenario {
	case scenarioAcceptThenDisconnect, scenarioChangedBodyRetry:
		base.EffectCount = 1
	case scenarioCrossOriginRedirectCredentials:
		base.AttemptCount = 2
		base.EffectCount = 1
		base.ResponseAttemptCount = 2
		base.ResponseCompleteCount = 2
		base.FirstResponseComplete = true
		base.Credential = credentialAbsentAtTarget
	case scenarioRetryLimit:
		base.AttemptCount = 2
		base.ResponseAttemptCount = 2
		base.ResponseCompleteCount = 2
		base.FirstResponseComplete = true
	case scenarioDelayedResponse:
		base.EffectCount = 1
		base.ResponseAttemptCount = 1
		base.ResponseCompleteCount = 1
		base.FirstResponseComplete = true
		base.DelayCompleteCount = 1
	}
	return base
}
