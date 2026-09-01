package corpus

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProjectionCorpusMatchesSemanticOracleAndArtifactBindings(t *testing.T) {
	rows, _ := loadRows(t)
	results, _ := loadResults(t, rows)
	projectionCases := map[string]string{
		"positive":     "result_all_positive",
		"unsafe":       "result_unsafe_cross_origin_redirect_credentials",
		"inconclusive": "result_inconclusive_delayed_response",
		"mixed":        "result_mixed_unsafe_over_inconclusive",
	}
	for directory, resultID := range projectionCases {
		directory, resultID := directory, resultID
		t.Run(directory, func(t *testing.T) {
			base := filepath.ToSlash(filepath.Join("projections", directory))
			var report neutralReport
			reportBytes := readCanonical(t, base+"/report.json", &report)
			validateNeutralReport(t, report, rows, results[resultID])

			junit := readCorpusFile(t, base+"/junit.xml")
			if want := renderNeutralJUnit(t, report); !bytes.Equal(junit, want) {
				t.Fatalf("JUnit bytes disagree with the neutral renderer\n got: %q\nwant: %q", junit, want)
			}
			summary := readCorpusFile(t, base+"/summary.md")
			if want := renderNeutralMarkdown(report); !bytes.Equal(summary, want) {
				t.Fatalf("Markdown bytes disagree with the neutral renderer\n got: %q\nwant: %q", summary, want)
			}

			var manifest artifactManifest
			manifestBytes := readCanonical(t, base+"/manifest.json", &manifest)
			validateArtifactProjection(t, manifest, reportBytes, junit, summary)
			if len(reportBytes) > 256<<10 || len(junit) > 256<<10 || len(summary) > 256<<10 ||
				len(manifestBytes) > 256<<10 || len(reportBytes)+len(junit)+len(summary)+len(manifestBytes) > 1<<20 {
				t.Fatal("projection or aggregate bound exceeded")
			}
			for name, contents := range map[string][]byte{
				"report": reportBytes, "junit": junit, "summary": summary, "manifest": manifestBytes,
			} {
				if bytes.Contains(contents, []byte(markerValue)) {
					t.Fatalf("sanitation marker leaked into %s", name)
				}
			}
		})
	}
}

func validateNeutralReport(t *testing.T, value neutralReport, rows map[string]rowCase, result resultCase) {
	t.Helper()
	if value.SchemaVersion != reportIdentity || value.SuiteIdentity != suiteIdentity ||
		value.ExplanationIdentity != explanationIdentity || value.ClaimCeiling != claimCeiling ||
		value.Assessment != result.Assessment || value.AssessmentText != assessmentText(value.Assessment) ||
		len(value.Scenarios) != len(scenarios) || len(result.Rows) != len(scenarios) {
		t.Fatalf("report/result identity drift: report=%#v result=%#v", value, result)
	}
	wantSummary := neutralSummary{Scenarios: uint32(len(scenarios))}
	for index, reportRow := range value.Scenarios {
		row := rows[result.Rows[index]]
		if reportRow.Scenario != scenarios[index] || reportRow.Scenario != row.Scenario ||
			reportRow.ScenarioText != scenarioText(row.Scenario) || reportRow.Assessment != row.Assessment ||
			reportRow.AssessmentText != assessmentText(row.Assessment) || reportRow.Observation != row.Observation ||
			reportRow.Findings == nil || len(reportRow.Findings) != len(row.Findings) {
			t.Fatalf("report row %d disagrees with semantic row %q: %#v", index, row.ID, reportRow)
		}
		assessment, findings := assess(reportRow.Scenario, reportRow.Observation)
		if assessment != reportRow.Assessment || !equalStrings(findings, row.Findings) {
			t.Fatalf("report row %d does not independently reconstruct", index)
		}
		for findingIndex, finding := range reportRow.Findings {
			if finding.Code != row.Findings[findingIndex] || finding.Text != findingText(finding.Code) {
				t.Fatalf("report row %d finding %d drift: %#v", index, findingIndex, finding)
			}
		}
		switch reportRow.Assessment {
		case assessmentPositive:
			wantSummary.Passed++
		case assessmentUnsafe:
			wantSummary.Failed++
		case assessmentInconclusive:
			wantSummary.Inconclusive++
		default:
			t.Fatalf("unknown report assessment %q", reportRow.Assessment)
		}
	}
	wantOutcome := map[string]string{assessmentPositive: "pass", assessmentUnsafe: "fail", assessmentInconclusive: "inconclusive"}[value.Assessment]
	if value.Outcome != wantOutcome || value.Summary != wantSummary ||
		wantSummary.Passed+wantSummary.Failed+wantSummary.Inconclusive != uint32(len(scenarios)) {
		t.Fatalf("report outcome/summary = %q/%#v, want %q/%#v", value.Outcome, value.Summary, wantOutcome, wantSummary)
	}
}

type neutralJUnitDocument struct {
	XMLName    xml.Name               `xml:"testsuite"`
	Name       string                 `xml:"name,attr"`
	Tests      uint32                 `xml:"tests,attr"`
	Failures   uint32                 `xml:"failures,attr"`
	Errors     uint32                 `xml:"errors,attr"`
	Properties neutralJUnitProperties `xml:"properties"`
	Cases      []neutralJUnitCase     `xml:"testcase"`
}

type neutralJUnitProperties struct {
	Items []neutralJUnitProperty `xml:"property"`
}

type neutralJUnitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type neutralJUnitCase struct {
	Name      string             `xml:"name,attr"`
	Classname string             `xml:"classname,attr"`
	Failure   *neutralJUnitIssue `xml:"failure,omitempty"`
	Error     *neutralJUnitIssue `xml:"error,omitempty"`
}

type neutralJUnitIssue struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func renderNeutralJUnit(t *testing.T, report neutralReport) []byte {
	t.Helper()
	document := neutralJUnitDocument{
		Name: "HTTP Retry Check scenario suite", Tests: report.Summary.Scenarios,
		Failures: report.Summary.Failed, Errors: report.Summary.Inconclusive,
		Properties: neutralJUnitProperties{Items: []neutralJUnitProperty{
			{Name: "schema_version", Value: reportIdentity},
			{Name: "suite_identity", Value: suiteIdentity},
			{Name: "explanation_identity", Value: explanationIdentity},
			{Name: "claim_ceiling", Value: claimCeiling},
			{Name: "outcome", Value: report.Outcome},
		}},
		Cases: make([]neutralJUnitCase, len(report.Scenarios)),
	}
	for index, row := range report.Scenarios {
		current := neutralJUnitCase{Name: row.ScenarioText, Classname: reportIdentity}
		texts := make([]string, len(row.Findings))
		for findingIndex, finding := range row.Findings {
			texts[findingIndex] = finding.Text
		}
		issue := &neutralJUnitIssue{Message: row.AssessmentText, Text: strings.Join(texts, "\n")}
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
		t.Fatal(err)
	}
	encoded = append([]byte(xml.Header), encoded...)
	return append(encoded, '\n')
}

func renderNeutralMarkdown(report neutralReport) []byte {
	var output strings.Builder
	output.WriteString("# HTTP Retry Check report\n\n> ")
	output.WriteString(claimCeiling)
	output.WriteString("\n\n**Result:** `")
	output.WriteString(report.Outcome)
	output.WriteString("` — ")
	output.WriteString(strconv.FormatUint(uint64(report.Summary.Passed), 10))
	output.WriteString(" passed, ")
	output.WriteString(strconv.FormatUint(uint64(report.Summary.Failed), 10))
	output.WriteString(" unsafe, ")
	output.WriteString(strconv.FormatUint(uint64(report.Summary.Inconclusive), 10))
	output.WriteString(" inconclusive.\n\n| Scenario | Result | Findings |\n| --- | --- | --- |\n")
	for _, row := range report.Scenarios {
		output.WriteString("| `")
		output.WriteString(escapeNeutralMarkdown(row.Scenario))
		output.WriteString("` | ")
		output.WriteString(markdownResult(row.Assessment))
		output.WriteString(" | ")
		if len(row.Findings) == 0 {
			output.WriteString("None")
		} else {
			for index, finding := range row.Findings {
				if index != 0 {
					output.WriteString("<br>")
				}
				output.WriteByte('`')
				output.WriteString(escapeNeutralMarkdown(finding.Code))
				output.WriteString("`: ")
				output.WriteString(escapeNeutralMarkdown(finding.Text))
			}
		}
		output.WriteString(" |\n")
	}
	return []byte(output.String())
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

func escapeNeutralMarkdown(value string) string {
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

func validateArtifactProjection(t *testing.T, manifest artifactManifest, report, junit, summary []byte) {
	t.Helper()
	if manifest.SchemaVersion != artifactIdentity || manifest.ReportSchemaVersion != reportIdentity ||
		manifest.ClaimCeiling != claimCeiling || manifest.DigestDomain != artifactDigestDomain ||
		!lowerSHA256(manifest.AggregateSHA256) || len(manifest.Files) != 3 {
		t.Fatalf("artifact manifest identity/shape drift: %#v", manifest)
	}
	contents := [][]byte{report, junit, summary}
	names := [...]string{"report.json", "junit.xml", "summary.md"}
	mediaTypes := [...]string{"application/json", "application/xml", "text/markdown; charset=utf-8"}
	for index, descriptor := range manifest.Files {
		digest := sha256.Sum256(contents[index])
		if descriptor.Name != names[index] || descriptor.MediaType != mediaTypes[index] ||
			descriptor.SizeBytes != uint64(len(contents[index])) || descriptor.SizeBytes == 0 ||
			descriptor.SHA256 != hex.EncodeToString(digest[:]) {
			t.Fatalf("artifact descriptor %d drift: %#v", index, descriptor)
		}
	}
	digest := sha256.New()
	writeNeutralDigestAtom(digest, artifactDigestDomain)
	writeNeutralDigestAtom(digest, artifactIdentity)
	writeNeutralDigestAtom(digest, reportIdentity)
	writeNeutralDigestAtom(digest, claimCeiling)
	for _, descriptor := range manifest.Files {
		writeNeutralDigestAtom(digest, descriptor.Name)
		writeNeutralDigestAtom(digest, descriptor.MediaType)
		writeNeutralDigestAtom(digest, strconv.FormatUint(descriptor.SizeBytes, 10))
		writeNeutralDigestAtom(digest, descriptor.SHA256)
	}
	if got := hex.EncodeToString(digest.Sum(nil)); got != manifest.AggregateSHA256 {
		t.Fatalf("aggregate digest = %s, want %s", got, manifest.AggregateSHA256)
	}
}

func writeNeutralDigestAtom(destination interface{ Write([]byte) (int, error) }, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write([]byte(value))
}

func readCorpusFile(t *testing.T, relative string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(corpusRoot, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) == 0 {
		t.Fatalf("empty corpus file %s", relative)
	}
	return contents
}

func projectionDescription(directory, resultID string) string {
	return fmt.Sprintf("%s:%s", directory, resultID)
}
