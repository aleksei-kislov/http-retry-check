package evidence

import (
	"encoding/xml"
	"strconv"
	"strings"
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

func JUnit(report Report) ([]byte, error) {
	if ValidateReport(report) != nil {
		return nil, invalidReport
	}
	document := junitDocument{
		Name: junitSuiteName, Tests: report.Summary.Scenarios,
		Failures: report.Summary.Failed, Errors: report.Summary.Inconclusive,
		Properties: junitProperties{Items: []junitProperty{
			{Name: "schema_version", Value: SchemaVersion},
			{Name: "suite_identity", Value: SuiteIdentity},
			{Name: "explanation_identity", Value: ExplanationIdentity},
			{Name: "claim_ceiling", Value: ClaimCeiling},
			{Name: "outcome", Value: report.Outcome},
		}},
		Cases: make([]junitCase, len(report.Scenarios)),
	}
	for index, row := range report.Scenarios {
		current := junitCase{Name: row.ScenarioText, Classname: SchemaVersion}
		issue := &junitIssue{Message: row.AssessmentText, Text: joinedFindingText(row.Findings)}
		switch row.Assessment {
		case assessmentPositive:
			issue = nil
		case assessmentUnsafe:
			current.Failure = issue
		case assessmentInconclusive:
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

func GitHubSummary(report Report) ([]byte, error) {
	if ValidateReport(report) != nil {
		return nil, invalidReport
	}
	var summary strings.Builder
	summary.WriteString("# HTTP Retry Check report\n\n> ")
	summary.WriteString(ClaimCeiling)
	summary.WriteString("\n\n**Result:** `")
	summary.WriteString(report.Outcome)
	summary.WriteString("` — ")
	summary.WriteString(strconv.FormatUint(uint64(report.Summary.Passed), 10))
	summary.WriteString(" passed, ")
	summary.WriteString(strconv.FormatUint(uint64(report.Summary.Failed), 10))
	summary.WriteString(" unsafe, ")
	summary.WriteString(strconv.FormatUint(uint64(report.Summary.Inconclusive), 10))
	summary.WriteString(" inconclusive.\n\n| Scenario | Result | Findings |\n| --- | --- | --- |\n")
	for _, row := range report.Scenarios {
		summary.WriteString("| `")
		summary.WriteString(escapeMarkdownCell(row.Scenario))
		summary.WriteString("` | ")
		summary.WriteString(markdownResult(row.Assessment))
		summary.WriteString(" | ")
		if len(row.Findings) == 0 {
			summary.WriteString("None")
		} else {
			for index, finding := range row.Findings {
				if index != 0 {
					summary.WriteString("<br>")
				}
				summary.WriteByte('`')
				summary.WriteString(escapeMarkdownCell(finding.Code))
				summary.WriteString("`: ")
				summary.WriteString(escapeMarkdownCell(finding.Text))
			}
		}
		summary.WriteString(" |\n")
	}
	encoded := []byte(summary.String())
	if len(encoded) > MaxProjectionBytes {
		return nil, invalidReport
	}
	return encoded, nil
}

// Explain returns a concise line-by-line summary of a report.
func Explain(report Report) ([]byte, error) {
	if ValidateReport(report) != nil {
		return nil, invalidReport
	}
	var explanation strings.Builder
	explanation.WriteString("HTTP Retry Check: ")
	explanation.WriteString(report.Outcome)
	explanation.WriteByte('\n')
	for _, row := range report.Scenarios {
		explanation.WriteString(strings.ToUpper(markdownResult(row.Assessment)))
		explanation.WriteByte(' ')
		explanation.WriteString(row.Scenario)
		explanation.WriteByte('\n')
		for _, finding := range row.Findings {
			explanation.WriteString("  ")
			explanation.WriteString(finding.Code)
			explanation.WriteString(": ")
			explanation.WriteString(finding.Text)
			explanation.WriteByte('\n')
		}
	}
	encoded := []byte(explanation.String())
	if len(encoded) > MaxProjectionBytes {
		return nil, invalidReport
	}
	return encoded, nil
}

func markdownResult(assessment string) string {
	switch assessment {
	case assessmentPositive:
		return "Pass"
	case assessmentUnsafe:
		return "Unsafe"
	case assessmentInconclusive:
		return "Inconclusive"
	default:
		return "Unknown"
	}
}

func joinedFindingText(findings []Finding) string {
	text := make([]string, len(findings))
	for index, finding := range findings {
		text[index] = finding.Text
	}
	return strings.Join(text, "\n")
}

func escapeMarkdownCell(value string) string {
	var escaped strings.Builder
	for _, character := range value {
		switch character {
		case '&':
			escaped.WriteString("&amp;")
		case '<':
			escaped.WriteString("&lt;")
		case '>':
			escaped.WriteString("&gt;")
		case '\\':
			escaped.WriteString("\\\\")
		case '|':
			escaped.WriteString("\\|")
		case '\r', '\n':
			escaped.WriteByte(' ')
		default:
			escaped.WriteRune(character)
		}
	}
	return escaped.String()
}
