package report

import (
	"strconv"
	"strings"

	httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"
)

const noFindingsText = "None"

// GitHubSummary validates and returns deterministic GitHub-flavored Markdown.
func GitHubSummary(value Report) ([]byte, error) {
	if Validate(value) != nil {
		return nil, invalidReport
	}
	var summary strings.Builder
	summary.WriteString("# HTTP Retry Check report\n\n")
	summary.WriteString("> ")
	summary.WriteString(ClaimCeiling)
	summary.WriteString("\n\n**Result:** `")
	summary.WriteString(string(value.Outcome))
	summary.WriteString("` — ")
	summary.WriteString(strconv.FormatUint(uint64(value.Summary.Passed), 10))
	summary.WriteString(" passed, ")
	summary.WriteString(strconv.FormatUint(uint64(value.Summary.Failed), 10))
	summary.WriteString(" unsafe, ")
	summary.WriteString(strconv.FormatUint(uint64(value.Summary.Inconclusive), 10))
	summary.WriteString(" inconclusive.\n\n| Scenario | Result | Findings |\n")
	summary.WriteString("| --- | --- | --- |\n")
	for _, row := range value.Scenarios {
		summary.WriteString("| `")
		summary.WriteString(escapeMarkdownCell(string(row.Scenario)))
		summary.WriteString("` | ")
		summary.WriteString(markdownResult(row.Assessment))
		summary.WriteString(" | ")
		if len(row.Findings) == 0 {
			summary.WriteString(noFindingsText)
		} else {
			for index, finding := range row.Findings {
				if index != 0 {
					summary.WriteString("<br>")
				}
				summary.WriteByte('`')
				summary.WriteString(escapeMarkdownCell(string(finding.Code)))
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

func markdownResult(assessment httpcheck.Assessment) string {
	switch assessment {
	case httpcheck.AssessmentNoUnsafeBehaviorObserved:
		return "Pass"
	case httpcheck.AssessmentUnsafeBehaviorObserved:
		return "Unsafe"
	case httpcheck.AssessmentInconclusive:
		return "Inconclusive"
	default:
		return "Unknown"
	}
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
