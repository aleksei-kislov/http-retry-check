package report

import (
	"encoding/xml"
	"strings"

	httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"
)

const junitSuiteName = "HTTP Retry Check scenario suite"

type junitDocument struct {
	XMLName    xml.Name        `xml:"testsuite"`
	Name       string          `xml:"name,attr"`
	Tests      uint32          `xml:"tests,attr"`
	Failures   uint32          `xml:"failures,attr"`
	Errors     uint32          `xml:"errors,attr"`
	Properties junitProperties `xml:"properties"`
	Cases      []junitCase     `xml:"testcase"`
}

type junitProperties struct {
	Items []junitProperty `xml:"property"`
}

type junitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type junitCase struct {
	Name      string      `xml:"name,attr"`
	Classname string      `xml:"classname,attr"`
	Failure   *junitIssue `xml:"failure,omitempty"`
	Error     *junitIssue `xml:"error,omitempty"`
}

type junitIssue struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

// JUnit validates and returns deterministic JUnit XML. Positive rows pass,
// unsafe rows are failures, and inconclusive rows are errors.
func JUnit(value Report) ([]byte, error) {
	if Validate(value) != nil {
		return nil, invalidReport
	}
	document := junitDocument{
		Name: junitSuiteName, Tests: value.Summary.Scenarios,
		Failures: value.Summary.Failed, Errors: value.Summary.Inconclusive,
		Properties: junitProperties{Items: []junitProperty{
			{Name: "schema_version", Value: SchemaVersion},
			{Name: "suite_identity", Value: SuiteIdentity},
			{Name: "explanation_identity", Value: ExplanationIdentity},
			{Name: "claim_ceiling", Value: ClaimCeiling},
			{Name: "outcome", Value: string(value.Outcome)},
		}},
		Cases: make([]junitCase, len(value.Scenarios)),
	}
	for index, row := range value.Scenarios {
		current := junitCase{Name: row.ScenarioText, Classname: SchemaVersion}
		issue := &junitIssue{Message: row.AssessmentText, Text: findingText(row.Findings)}
		switch row.Assessment {
		case httpcheck.AssessmentNoUnsafeBehaviorObserved:
			issue = nil
		case httpcheck.AssessmentUnsafeBehaviorObserved:
			current.Failure = issue
		case httpcheck.AssessmentInconclusive:
			current.Error = issue
		}
		document.Cases[index] = current
	}
	encoded, err := xml.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, invalidReport
	}
	encoded = append([]byte(xml.Header), encoded...)
	encoded = append(encoded, '\n')
	if len(encoded) > MaxProjectionBytes {
		return nil, invalidReport
	}
	return encoded, nil
}

func findingText(findings []Finding) string {
	text := make([]string, len(findings))
	for index, finding := range findings {
		text[index] = finding.Text
	}
	return strings.Join(text, "\n")
}
