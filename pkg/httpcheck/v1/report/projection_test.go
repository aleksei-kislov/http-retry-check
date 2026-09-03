package report

import (
	"bytes"
	"encoding/xml"
	"reflect"
	"strings"
	"testing"
)

func TestJUnitMappingPropertiesAndEscaping(t *testing.T) {
	for _, result := range []sourceResult{positiveResult(), unsafeResult(), inconclusiveResult(), mixedResult()} {
		value := mustReport(t, result)
		encoded, err := JUnit(value)
		if err != nil || len(encoded) > MaxProjectionBytes || !bytes.HasPrefix(encoded, []byte(xml.Header)) ||
			encoded[len(encoded)-1] != '\n' {
			t.Fatalf("JUnit envelope = %q/%v", encoded, err)
		}
		for _, forbidden := range []string{" time=", " timestamp=", " hostname=", "<system-out", "<system-err", sanitationMarker} {
			if bytes.Contains(encoded, []byte(forbidden)) {
				t.Fatalf("JUnit contains forbidden %q", forbidden)
			}
		}
		var document junitDocument
		if err := xml.Unmarshal(encoded, &document); err != nil {
			t.Fatal(err)
		}
		if document.XMLName.Local != "testsuite" || document.Name != junitSuiteName ||
			document.Tests != 6 || document.Failures != value.Summary.Failed ||
			document.Errors != value.Summary.Inconclusive || len(document.Cases) != 6 {
			t.Fatalf("JUnit aggregate drift: %#v", document)
		}
		wantProperties := []junitProperty{
			{Name: "schema_version", Value: SchemaVersion},
			{Name: "suite_identity", Value: SuiteIdentity},
			{Name: "explanation_identity", Value: ExplanationIdentity},
			{Name: "claim_ceiling", Value: ClaimCeiling},
			{Name: "outcome", Value: string(value.Outcome)},
		}
		if !reflect.DeepEqual(document.Properties.Items, wantProperties) {
			t.Fatalf("JUnit properties = %#v", document.Properties.Items)
		}
		for index, current := range document.Cases {
			row := value.Scenarios[index]
			if current.Name != row.ScenarioText || current.Classname != SchemaVersion {
				t.Fatalf("JUnit case %d identity drift", index)
			}
			switch row.Assessment {
			case "no_unsafe_behavior_observed":
				if current.Failure != nil || current.Error != nil {
					t.Fatalf("positive case %d became non-green", index)
				}
			case "unsafe_behavior_observed":
				assertJUnitIssue(t, current.Failure, current.Error, row)
			case "inconclusive":
				assertJUnitIssue(t, current.Error, current.Failure, row)
			}
		}
	}

	marker := `<marker>&"'`
	document := junitDocument{
		XMLName: xml.Name{Local: "testsuite"}, Name: marker, Tests: 1,
		Properties: junitProperties{Items: []junitProperty{{Name: marker, Value: marker}}},
		Cases:      []junitCase{{Name: marker, Classname: marker, Failure: &junitIssue{Message: marker, Text: marker}}},
	}
	encoded, err := xml.Marshal(document)
	if err != nil || bytes.Contains(encoded, []byte(`<marker>`)) || bytes.Contains(encoded, []byte(`="<marker>`)) {
		t.Fatalf("XML marker escaping = %q/%v", encoded, err)
	}
	var roundTrip junitDocument
	if err := xml.Unmarshal(encoded, &roundTrip); err != nil || !reflect.DeepEqual(roundTrip, document) {
		t.Fatalf("escaped XML round trip = %#v/%v", roundTrip, err)
	}
}

func TestGitHubSummaryMappingAndEscaping(t *testing.T) {
	for _, result := range []sourceResult{positiveResult(), unsafeResult(), inconclusiveResult(), mixedResult()} {
		value := mustReport(t, result)
		encoded, err := GitHubSummary(value)
		if err != nil {
			t.Fatal(err)
		}
		text := string(encoded)
		wantPrefix := "# HTTP Retry Check report\n\n> " + ClaimCeiling +
			"\n\n**Result:** `" + string(value.Outcome) + "` — "
		if !strings.HasPrefix(text, wantPrefix) || !strings.HasSuffix(text, " |\n") ||
			strings.Count(text, "\n| `") != 6 ||
			strings.Contains(text, "<table") || strings.Contains(text, sanitationMarker) {
			t.Fatalf("Markdown shape drift:\n%s", text)
		}
		for _, row := range value.Scenarios {
			if strings.Count(text, expectedMarkdownRow(row)) != 1 {
				t.Fatalf("Markdown row count for %s drifted", row.Scenario)
			}
		}
	}
	if got, want := escapeMarkdownCell("a|b\\c\r\n<&>"), `a\|b\\c  &lt;&amp;&gt;`; got != want {
		t.Fatalf("Markdown escape = %q, want %q", got, want)
	}
}

func assertJUnitIssue(t *testing.T, issue, forbidden *junitIssue, row Scenario) {
	t.Helper()
	if issue == nil || forbidden != nil || issue.Message != row.AssessmentText || issue.Text != findingText(row.Findings) {
		t.Fatalf("JUnit issue drift: %#v/%#v", issue, forbidden)
	}
}

func expectedMarkdownRow(row Scenario) string {
	findings := noFindingsText
	if len(row.Findings) != 0 {
		texts := make([]string, len(row.Findings))
		for index, finding := range row.Findings {
			texts[index] = "`" + string(finding.Code) + "`: " + finding.Text
		}
		findings = strings.Join(texts, "<br>")
	}
	return "| `" + string(row.Scenario) + "` | " + markdownResult(row.Assessment) + " | " + findings + " |\n"
}
