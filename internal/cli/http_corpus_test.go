package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/evidence"
)

var httpCorpusRoot = filepath.Join("..", "..", "conformance", "http-retry-check", "v1")

type httpCorpusInvalidBundle struct {
	SchemaVersion string                  `json:"schema_version"`
	Category      string                  `json:"category"`
	Marker        string                  `json:"marker,omitempty"`
	Cases         []httpCorpusInvalidCase `json:"cases"`
}

type httpCorpusInvalidCase struct {
	ID               string   `json:"id"`
	Expected         string   `json:"expected"`
	Representability []string `json:"representability"`
	BaseCase         string   `json:"base_case,omitempty"`
	Operation        string   `json:"operation"`
	Target           string   `json:"target,omitempty"`
	Operand          string   `json:"operand,omitempty"`
	Count            uint64   `json:"count,omitempty"`
}

type httpCorpusExplainReport struct {
	Outcome   string `json:"outcome"`
	Scenarios []struct {
		Scenario   string `json:"scenario"`
		Assessment string `json:"assessment"`
		Findings   []struct {
			Code string `json:"code"`
			Text string `json:"text"`
		} `json:"findings"`
	} `json:"scenarios"`
}

func TestHTTPCorpusProjectionInputsRouteExactly(t *testing.T) {
	tests := []struct {
		name      string
		checkCode int
		checkLine string
	}{
		{name: "positive", checkCode: 0, checkLine: "HTTP Retry Check found no unsafe behavior\n"},
		{name: "unsafe", checkCode: 1, checkLine: "HTTP Retry Check found unsafe behavior\n"},
		{name: "inconclusive", checkCode: 1, checkLine: "HTTP Retry Check is inconclusive\n"},
		{name: "mixed", checkCode: 1, checkLine: "HTTP Retry Check found unsafe behavior\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reportRelative := filepath.Join("projections", test.name, "report.json")
			reportPath := filepath.Join(httpCorpusRoot, reportRelative)
			reportBytes := httpReadCorpus(t, reportRelative)
			httpAssertCorpusCommand(t, []string{"http", "validate", reportPath}, nil, 0, "HTTP Retry Check evidence is valid\n", "")
			httpAssertCorpusCommand(t, []string{"http", "check", reportPath}, nil, test.checkCode, test.checkLine, "")
			httpAssertCorpusCommand(t, []string{"http", "explain", "-"}, reportBytes, 0, httpCorpusExplain(t, reportBytes), "")

			artifactPath := filepath.Join(httpCorpusRoot, "projections", test.name)
			httpAssertCorpusCommand(t, []string{"http", "validate", artifactPath}, nil, 0, "HTTP Retry Check evidence is valid\n", "")
			httpAssertCorpusCommand(t, []string{"http", "check", artifactPath}, nil, test.checkCode, test.checkLine, "")

			destination := filepath.Join(httpRealTempDir(t), "artifact")
			httpAssertCorpusCommand(t, []string{"http", "artifact", "-", destination}, reportBytes, 0, "HTTP Retry Check artifact created\n", "")
			for _, name := range []string{"manifest.json", "report.json", "junit.xml", "summary.md"} {
				got, err := os.ReadFile(filepath.Join(destination, name))
				if err != nil {
					t.Fatal(err)
				}
				want := httpReadCorpus(t, filepath.Join("projections", test.name, name))
				if !bytes.Equal(got, want) {
					t.Fatalf("exported %s/%s differs from the frozen corpus", test.name, name)
				}
			}
		})
	}
}

func TestHTTPCorpusInvalidCanonicalReportVectorsAreRejected(t *testing.T) {
	canonical := httpReadCorpusInvalidBundle(t, "invalid/canonical-json.json")
	if len(canonical.Cases) != 35 {
		t.Fatalf("canonical invalid vector count = %d, want 35", len(canonical.Cases))
	}
	for _, candidate := range canonical.Cases {
		candidate := candidate
		t.Run(candidate.ID, func(t *testing.T) {
			if candidate.Expected != "invalid_report" {
				t.Fatalf("unexpected vector result %q", candidate.Expected)
			}
			httpAssertCorpusInvalidReport(t, httpMaterializeCanonicalInvalid(t, candidate))
		})
	}

	reportModel := httpReadCorpusInvalidBundle(t, "invalid/report-model.json")
	applied := 0
	for _, candidate := range reportModel.Cases {
		candidate := candidate
		if candidate.Operation == "construct_report" {
			continue
		}
		applied++
		t.Run(candidate.ID, func(t *testing.T) {
			if candidate.Expected != "invalid_report" {
				t.Fatalf("unexpected vector result %q", candidate.Expected)
			}
			httpAssertCorpusInvalidReport(t, httpMaterializeReportModelInvalid(t, candidate))
		})
	}
	if applied != 19 {
		t.Fatalf("applicable report-model vectors = %d, want 19", applied)
	}
}

func TestHTTPCorpusInvalidArtifactVectorsAreRejected(t *testing.T) {
	bundle := httpReadCorpusInvalidBundle(t, "invalid/artifact.json")
	applied := 0
	for _, candidate := range bundle.Cases {
		candidate := candidate
		t.Run(candidate.ID, func(t *testing.T) {
			artifactPath, ok := httpMaterializeInvalidArtifact(t, candidate)
			if !ok {
				return
			}
			applied++
			for _, command := range []string{"validate", "check"} {
				httpAssertCorpusCommand(t, []string{"http", command, artifactPath}, nil, 2, "", "HTTP Retry Check evidence is invalid\n")
			}
		})
	}
	if applied != 14 {
		t.Fatalf("applicable filesystem artifact vectors = %d, want 14", applied)
	}
}

func TestHTTPCorpusCLISanitationVectorsBindOwnedControls(t *testing.T) {
	bundle := httpReadCorpusInvalidBundle(t, "invalid/sanitation.json")
	if bundle.Marker == "" {
		t.Fatal("sanitation corpus marker is absent")
	}
	want := map[string]string{
		"sanitation_filesystem_location":    "filesystem_path",
		"sanitation_io_error":               "io_error",
		"sanitation_environment_proxy":      "environment_proxy",
		"sanitation_environment_credential": "environment_credential",
	}
	seen := make(map[string]bool, len(want))
	for _, candidate := range bundle.Cases {
		candidate := candidate
		if !httpCorpusRepresents(candidate, "cli") {
			continue
		}
		t.Run(candidate.ID, func(t *testing.T) {
			target, ok := want[candidate.ID]
			if !ok {
				t.Fatalf("unowned CLI sanitation case %q", candidate.ID)
			}
			if seen[candidate.ID] {
				t.Fatalf("duplicate CLI sanitation case %q", candidate.ID)
			}
			seen[candidate.ID] = true
			if candidate.Expected != "marker_absent" || candidate.Operation != "inject_marker" ||
				candidate.Target != target || candidate.Operand != bundle.Marker {
				t.Fatalf("CLI sanitation case drifted: %#v", candidate)
			}
			switch candidate.ID {
			case "sanitation_filesystem_location":
				httpAssertCorpusCLIFilesystemSanitation(t, bundle.Marker)
			case "sanitation_io_error":
				httpAssertCorpusCLIIOErrorSanitation(t, bundle.Marker)
			case "sanitation_environment_proxy":
				httpAssertCorpusCLIEnvironmentSanitation(t, bundle.Marker, []string{
					"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
					"http_proxy", "https_proxy", "all_proxy", "no_proxy",
				})
			case "sanitation_environment_credential":
				httpAssertCorpusCLIEnvironmentSanitation(t, bundle.Marker, []string{
					"HTTP_RETRY_CHECK_CREDENTIAL", "HTTP_RETRY_CHECK_AUTHORIZATION",
					"HTTP_AUTHORIZATION", "AUTHORIZATION",
				})
			}
		})
	}
	if len(seen) != len(want) {
		t.Fatalf("CLI sanitation cases = %#v, want all %#v", seen, want)
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("CLI sanitation case %q was not consumed", id)
		}
	}
}

func TestHTTPCorpusCLIAuthorityVectorsBindOwnedControls(t *testing.T) {
	type authorityControl struct {
		expected  string
		operation string
		target    string
	}
	want := map[string]authorityControl{
		"authority_cli_no_runtime_import": {
			expected: "forbidden_absent", operation: "source_import", target: "native_http_runtime",
		},
		"authority_cli_no_execution": {
			expected: "forbidden_absent", operation: "source_capability", target: "process_plugin_rpc_package_test_network",
		},
		"authority_cli_no_follow_persistence": {
			expected: "native_control", operation: "filesystem_boundary", target: "no_follow_atomic_no_replace",
		},
	}
	bundle := httpReadCorpusInvalidBundle(t, "invalid/authority.json")
	seen := make(map[string]bool, len(want))
	for _, candidate := range bundle.Cases {
		candidate := candidate
		if !httpCorpusRepresents(candidate, "cli") {
			continue
		}
		t.Run(candidate.ID, func(t *testing.T) {
			control, ok := want[candidate.ID]
			if !ok {
				t.Fatalf("unowned CLI authority case %q", candidate.ID)
			}
			if seen[candidate.ID] {
				t.Fatalf("duplicate CLI authority case %q", candidate.ID)
			}
			seen[candidate.ID] = true
			if candidate.Expected != control.expected || candidate.Operation != control.operation ||
				candidate.Target != control.target || candidate.BaseCase != "" || candidate.Operand != "" || candidate.Count != 0 {
				t.Fatalf("CLI authority case drifted: %#v", candidate)
			}
			switch candidate.ID {
			case "authority_cli_no_runtime_import", "authority_cli_no_execution":
				httpAssertCLIHasOnlyStaticEvidenceScaffoldAndFilesystemAuthority(t)
			case "authority_cli_no_follow_persistence":
				httpAssertCorpusCLIPersistenceAuthority(t)
			}
		})
	}
	if len(seen) != len(want) {
		t.Fatalf("CLI authority cases = %#v, want all %#v", seen, want)
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("CLI authority case %q was not consumed", id)
		}
	}
}

func httpAssertCorpusCLIFilesystemSanitation(t *testing.T, marker string) {
	t.Helper()
	parent := httpRealTempDir(t)
	httpAssertCorpusSanitizedCommand(t,
		[]string{"http", "validate", filepath.Join(parent, marker+"-absent")},
		&httpForbiddenReader{}, 2, "", "HTTP Retry Check evidence is invalid\n", marker)

	reportBytes := httpReadCorpus(t, "projections/positive/report.json")
	destination := filepath.Join(parent, marker)
	httpAssertCorpusSanitizedCommand(t, []string{"http", "artifact", "-", destination},
		bytes.NewReader(reportBytes), 0, "HTTP Retry Check artifact created\n", "", marker)
	httpAssertCorpusSanitizedCommand(t, []string{"http", "artifact", "-", destination},
		bytes.NewReader(reportBytes), 2, "", "HTTP Retry Check artifact target is unavailable\n", marker)
}

func httpAssertCorpusCLIIOErrorSanitation(t *testing.T, marker string) {
	t.Helper()
	httpAssertCorpusSanitizedCommand(t, []string{"http", "validate", "-"},
		httpCorpusMarkerReader(marker), 3, "", "http-retry-check internal failure\n", marker)
}

func httpAssertCorpusCLIEnvironmentSanitation(t *testing.T, marker string, variables []string) {
	t.Helper()
	for _, variable := range variables {
		t.Setenv(variable, marker)
	}
	httpAssertCLIHasOnlyStaticEvidenceScaffoldAndFilesystemAuthority(t)
	httpAssertCorpusSanitizedCommand(t, []string{"http", "validate", "-"},
		bytes.NewReader(httpReadCorpus(t, "projections/positive/report.json")),
		0, "HTTP Retry Check evidence is valid\n", "", marker)
}

func httpAssertCorpusCLIPersistenceAuthority(t *testing.T) {
	t.Helper()
	reportBytes := httpReadCorpus(t, "projections/positive/report.json")
	root := httpRealTempDir(t)
	reportPath := filepath.Join(root, "report.json")
	if err := os.WriteFile(reportPath, reportBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	reportLink := filepath.Join(root, "report-link")
	if err := os.Symlink(reportPath, reportLink); err != nil {
		t.Fatal(err)
	}
	httpAssertCorpusCommand(t, []string{"http", "validate", reportLink}, nil, 2, "", "HTTP Retry Check evidence is invalid\n")

	realParent := filepath.Join(root, "real-parent")
	if err := os.Mkdir(realParent, 0o755); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(root, "linked-parent")
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	httpAssertCorpusCommand(t, []string{"http", "artifact", "-", filepath.Join(linkedParent, "artifact")},
		reportBytes, 2, "", "HTTP Retry Check artifact target is unavailable\n")
	entries, err := os.ReadDir(realParent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("no-follow refusal mutated symlink target: %#v/%v", entries, err)
	}

	existing := filepath.Join(root, "existing")
	const preserved = "caller-owned bytes"
	if err := os.WriteFile(existing, []byte(preserved), 0o600); err != nil {
		t.Fatal(err)
	}
	httpAssertCorpusCommand(t, []string{"http", "artifact", "-", existing},
		reportBytes, 2, "", "HTTP Retry Check artifact target is unavailable\n")
	contents, err := os.ReadFile(existing)
	if err != nil || string(contents) != preserved {
		t.Fatalf("no-overwrite refusal changed existing target: %q/%v", contents, err)
	}

	type commandResult struct {
		code           int
		stdout, stderr string
	}
	destination := filepath.Join(root, "race-artifact")
	start := make(chan struct{})
	results := make(chan commandResult, 2)
	for index := 0; index < 2; index++ {
		go func() {
			<-start
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"http", "artifact", "-", destination},
				bytes.NewReader(reportBytes), &stdout, &stderr)
			results <- commandResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
		}()
	}
	close(start)
	successes, refusals := 0, 0
	for index := 0; index < 2; index++ {
		result := <-results
		switch {
		case result.code == 0 && result.stdout == "HTTP Retry Check artifact created\n" && result.stderr == "":
			successes++
		case result.code == 2 && result.stdout == "" && result.stderr == "HTTP Retry Check artifact target is unavailable\n":
			refusals++
		default:
			t.Fatalf("atomic no-replace command = %d/%q/%q", result.code, result.stdout, result.stderr)
		}
	}
	if successes != 1 || refusals != 1 {
		t.Fatalf("atomic no-replace winners/refusals = %d/%d", successes, refusals)
	}
	httpAssertCorpusCommand(t, []string{"http", "validate", destination}, nil, 0, "HTTP Retry Check evidence is valid\n", "")
	if _, err := os.Lstat(filepath.Join(root, ".http-retry-check-artifact-stage")); !os.IsNotExist(err) {
		t.Fatalf("atomic no-replace left staging residue: %v", err)
	}
}

func httpAssertCorpusSanitizedCommand(
	t *testing.T,
	args []string,
	stdin interface{ Read([]byte) (int, error) },
	wantCode int,
	wantStdout, wantStderr, marker string,
) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, stdin, &stdout, &stderr)
	if code != wantCode || stdout.String() != wantStdout || stderr.String() != wantStderr {
		t.Fatalf("%v = exit %d stdout %q stderr %q", args, code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), marker) || strings.Contains(stderr.String(), marker) {
		t.Fatalf("%v leaked the corpus marker", args)
	}
	if forbidden, ok := stdin.(*httpForbiddenReader); ok && forbidden.reads != 0 {
		t.Fatalf("%v unexpectedly read stdin", args)
	}
}

func httpCorpusRepresents(candidate httpCorpusInvalidCase, binding string) bool {
	for _, represented := range candidate.Representability {
		if represented == binding {
			return true
		}
	}
	return false
}

type httpCorpusMarkerError string

func (failure httpCorpusMarkerError) Error() string { return string(failure) }

type httpCorpusMarkerReader string

func (reader httpCorpusMarkerReader) Read([]byte) (int, error) {
	return 0, httpCorpusMarkerError(reader)
}

func httpAssertCorpusInvalidReport(t *testing.T, contents []byte) {
	t.Helper()
	for _, command := range []string{"validate", "check", "explain"} {
		httpAssertCorpusCommand(t, []string{"http", command, "-"}, contents, 2, "", "HTTP Retry Check evidence is invalid\n")
	}
	destination := filepath.Join(httpRealTempDir(t), "must-not-be-created")
	httpAssertCorpusCommand(t, []string{"http", "artifact", "-", destination}, contents, 2, "", "HTTP Retry Check evidence is invalid\n")
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("invalid report reached artifact destination: %v", err)
	}
}

func httpAssertCorpusCommand(t *testing.T, args []string, input []byte, wantCode int, wantStdout, wantStderr string) {
	t.Helper()
	stdin := &httpForbiddenReader{}
	var reader interface{ Read([]byte) (int, error) } = stdin
	if input != nil {
		reader = bytes.NewReader(input)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, reader, &stdout, &stderr)
	if code != wantCode || stdout.String() != wantStdout || stderr.String() != wantStderr {
		t.Fatalf("%v = exit %d stdout %q stderr %q", args, code, stdout.String(), stderr.String())
	}
	if input == nil && stdin.reads != 0 {
		t.Fatalf("%v unexpectedly read stdin", args)
	}
}

func httpCorpusExplain(t *testing.T, reportBytes []byte) string {
	t.Helper()
	var report httpCorpusExplainReport
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	output.WriteString("HTTP Retry Check: ")
	output.WriteString(report.Outcome)
	output.WriteByte('\n')
	for _, row := range report.Scenarios {
		output.WriteString(strings.ToUpper(httpCorpusResult(row.Assessment)))
		output.WriteByte(' ')
		output.WriteString(row.Scenario)
		output.WriteByte('\n')
		for _, finding := range row.Findings {
			output.WriteString("  ")
			output.WriteString(finding.Code)
			output.WriteString(": ")
			output.WriteString(finding.Text)
			output.WriteByte('\n')
		}
	}
	return output.String()
}

func httpCorpusResult(assessment string) string {
	switch assessment {
	case "no_unsafe_behavior_observed":
		return "Pass"
	case "unsafe_behavior_observed":
		return "Unsafe"
	case "inconclusive":
		return "Inconclusive"
	default:
		return "Unknown"
	}
}

func httpMaterializeCanonicalInvalid(t *testing.T, candidate httpCorpusInvalidCase) []byte {
	t.Helper()
	base := httpReadCorpus(t, "projections/positive/report.json")
	switch candidate.Operation {
	case "replace_base64":
		return httpDecodeBase64(t, candidate.Operand)
	case "oversize_ascii":
		return bytes.Repeat([]byte(candidate.Operand), int(candidate.Count))
	case "prefix_base64":
		return append(httpDecodeBase64(t, candidate.Operand), base...)
	case "suffix_base64":
		return append(append([]byte{}, base...), httpDecodeBase64(t, candidate.Operand)...)
	case "remove_terminal_lf":
		return bytes.TrimSuffix(base, []byte("\n"))
	case "reindent":
		report, err := evidence.DecodeReport(base)
		if err != nil {
			t.Fatal(err)
		}
		width, err := strconv.Atoi(candidate.Operand)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.MarshalIndent(report, "", strings.Repeat(" ", width))
		if err != nil {
			t.Fatal(err)
		}
		return append(encoded, '\n')
	case "insert_whitespace":
		return httpInsertAfterFirstColon(t, base, candidate.Operand)
	case "swap_members":
		return httpSwapFirstTwoObjectLines(t, base)
	case "replace_escape":
		return []byte(strings.Replace(
			string(base), evidence.ClaimCeiling, candidate.Operand+evidence.ClaimCeiling[1:], 1))
	case "duplicate_member":
		return httpDuplicateFirstObjectLine(t, base)
	case "add_member":
		return append([]byte("{\n  \"marker_unknown\": "+candidate.Operand+",\n"), base[2:]...)
	case "replace_json":
		return []byte(candidate.Operand)
	case "set":
		return httpReplaceCorpusJSONValue(t, base, candidate.Target, candidate.Operand)
	case "replace_object_members":
		var output strings.Builder
		output.WriteByte('{')
		for index := uint64(0); index < candidate.Count; index++ {
			if index != 0 {
				output.WriteByte(',')
			}
			output.WriteString(strconv.Quote(candidate.Operand + strconv.FormatUint(index, 10)))
			output.WriteString(":0")
		}
		output.WriteByte('}')
		return []byte(output.String())
	case "replace_array_items":
		items := make([]string, candidate.Count)
		for index := range items {
			items[index] = candidate.Operand
		}
		return []byte("[" + strings.Join(items, ",") + "]")
	default:
		t.Fatalf("unsupported canonical corpus operation %q", candidate.Operation)
		return nil
	}
}

func httpMaterializeReportModelInvalid(t *testing.T, candidate httpCorpusInvalidCase) []byte {
	t.Helper()
	baseCase := candidate.BaseCase
	if baseCase == "" {
		baseCase = "positive"
	}
	base := httpReadCorpus(t, filepath.Join("projections", baseCase, "report.json"))
	if candidate.ID == "report_nested_missing" {
		start := bytes.Index(base, []byte("      \"observation\": {\n"))
		if start < 0 {
			t.Fatal("observation member not found")
		}
		endRelative := bytes.Index(base[start:], []byte("\n      },\n"))
		if endRelative < 0 {
			t.Fatal("observation member end not found")
		}
		end := start + endRelative + len("\n      },\n")
		return append(append([]byte{}, base[:start]...), base[end:]...)
	}
	if candidate.ID == "report_nested_duplicate" {
		line := []byte("      \"assessment\": \"no_unsafe_behavior_observed\",\n")
		index := bytes.Index(base, line)
		if index < 0 {
			t.Fatal("nested assessment member not found")
		}
		return append(append(append([]byte{}, base[:index]...), line...), base[index:]...)
	}
	report, err := evidence.DecodeReport(base)
	if err != nil {
		t.Fatal(err)
	}
	switch candidate.ID {
	case "report_outcome_zero":
		report.Outcome = ""
	case "report_outcome_unknown":
		report.Outcome = "marker_unknown"
	case "report_outcome_successor":
		report.Outcome = "pass_v2"
	case "report_schema_drift":
		report.SchemaVersion = "http_retry_check.report.v1"
	case "report_suite_drift":
		report.SuiteIdentity = "http_retry_check.scenario_suite.v2"
	case "report_explanation_identity_drift":
		report.ExplanationIdentity = "http_retry_check.scenario_explanations.v2"
	case "report_claim_ceiling_drift":
		report.ClaimCeiling = "marker_claim"
	case "report_scenario_text_drift":
		report.Scenarios[0].ScenarioText = "marker_text"
	case "report_assessment_text_drift":
		report.AssessmentText = "marker_text"
	case "report_row_assessment_text_drift":
		report.Scenarios[0].AssessmentText = "marker_text"
	case "report_finding_text_drift":
		report.Scenarios[5].Findings[0].Text = "marker_text"
	case "report_aggregate_drift":
		report.Assessment = "inconclusive"
	case "report_summary_drift":
		report.Summary.Passed = 5
	case "report_scenarios_null":
		report.Scenarios = nil
	case "report_findings_null":
		report.Scenarios[0].Findings = nil
	case "report_rows_reordered":
		report.Scenarios[0], report.Scenarios[1] = report.Scenarios[1], report.Scenarios[0]
	case "report_findings_reordered":
		findings := report.Scenarios[5].Findings
		findings[0], findings[1] = findings[1], findings[0]
	default:
		t.Fatalf("unsupported report-model corpus vector %q", candidate.ID)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}

func httpMaterializeInvalidArtifact(t *testing.T, candidate httpCorpusInvalidCase) (string, bool) {
	t.Helper()
	switch candidate.Operation {
	case "native_alias_probe", "build_artifact", "build_artifact_null", "construct_file_null",
		"construct_file_malformed", "validate_null", "validate_nil", "mutate_after_validate":
		return "", false
	case "mutate_file_name":
		if filepath.Clean(candidate.Operand) == filepath.Clean(candidate.Target) {
			return "", false
		}
	}
	root := filepath.Join(httpRealTempDir(t), "artifact")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manifest.json", "report.json", "junit.xml", "summary.md"} {
		contents := httpReadCorpus(t, filepath.Join("projections", "positive", name))
		if err := os.WriteFile(filepath.Join(root, name), contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(root, candidate.Target)
	switch candidate.Operation {
	case "remove_file":
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
	case "add_file":
		if err := os.WriteFile(target, []byte("marker"), 0o600); err != nil {
			t.Fatal(err)
		}
	case "swap_files":
		httpSwapFileContents(t, target, filepath.Join(root, candidate.Operand))
	case "alias_contents":
		contents, err := os.ReadFile(filepath.Join(root, candidate.Operand))
		if err != nil || os.WriteFile(target, contents, 0o600) != nil {
			t.Fatalf("alias artifact contents: %v", err)
		}
	case "duplicate_file":
		contents, err := os.ReadFile(target)
		if err != nil || os.WriteFile(filepath.Join(root, "duplicate-report.json"), contents, 0o600) != nil {
			t.Fatalf("duplicate artifact file: %v", err)
		}
	case "flip_byte":
		contents, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		index, err := strconv.Atoi(candidate.Operand)
		if err != nil || index < 0 || index >= len(contents) {
			t.Fatalf("invalid flip index %q", candidate.Operand)
		}
		contents[index] ^= 1
		if err := os.WriteFile(target, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	case "insert_whitespace":
		contents, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, httpInsertAfterFirstColon(t, contents, candidate.Operand), 0o600); err != nil {
			t.Fatal(err)
		}
	case "swap_members":
		contents, err := os.ReadFile(filepath.Join(root, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), httpSwapFirstTwoObjectLines(t, contents), 0o600); err != nil {
			t.Fatal(err)
		}
	case "add_member":
		path := filepath.Join(root, "manifest.json")
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		contents = append([]byte("{\n  \"marker_unknown\": "+candidate.Operand+",\n"), contents[2:]...)
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	case "set":
		path := filepath.Join(root, "manifest.json")
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, httpReplaceCorpusJSONValue(t, contents, candidate.Target, candidate.Operand), 0o600); err != nil {
			t.Fatal(err)
		}
	case "replace_file_from_case":
		contents := httpReadCorpus(t, filepath.Join("projections", candidate.Operand, candidate.Target))
		if err := os.WriteFile(target, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	case "resize_file":
		if err := os.WriteFile(target, bytes.Repeat([]byte(candidate.Operand), int(candidate.Count)), 0o600); err != nil {
			t.Fatal(err)
		}
	case "resize_set":
		remaining := int(candidate.Count)
		names := []string{"manifest.json", "report.json", "junit.xml", "summary.md"}
		for index, name := range names {
			size := remaining
			minimum := remaining - evidence.MaxProjectionBytes*(len(names)-index-1)
			if size > evidence.MaxProjectionBytes && minimum <= evidence.MaxProjectionBytes {
				size = evidence.MaxProjectionBytes
			} else if minimum > evidence.MaxProjectionBytes {
				size = minimum
			}
			if err := os.WriteFile(filepath.Join(root, name), bytes.Repeat([]byte(candidate.Operand), size), 0o600); err != nil {
				t.Fatal(err)
			}
			remaining -= size
		}
		if remaining != 0 {
			t.Fatalf("aggregate resize left %d bytes", remaining)
		}
	case "mutate_file_name":
		if err := os.Rename(target, filepath.Join(root, candidate.Operand)); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unsupported artifact corpus operation %q", candidate.Operation)
	}
	return root, true
}

func httpReadCorpusInvalidBundle(t *testing.T, relative string) httpCorpusInvalidBundle {
	t.Helper()
	var bundle httpCorpusInvalidBundle
	if err := json.Unmarshal(httpReadCorpus(t, relative), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.SchemaVersion != "http_retry_check.conformance_invalid.v1" || bundle.Category == "" || bundle.Cases == nil {
		t.Fatalf("invalid corpus bundle %s has drifted", relative)
	}
	return bundle
}

func httpReadCorpus(t *testing.T, relative string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(httpCorpusRoot, filepath.FromSlash(relative)))
	if err != nil || len(contents) == 0 {
		t.Fatalf("read corpus %s: %v", relative, err)
	}
	return contents
}

func httpDecodeBase64(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func httpInsertAfterFirstColon(t *testing.T, source []byte, insertion string) []byte {
	t.Helper()
	index := bytes.IndexByte(source, ':')
	if index < 0 {
		t.Fatal("JSON colon not found")
	}
	return append(append(append([]byte{}, source[:index+1]...), insertion...), source[index+1:]...)
}

func httpSwapFirstTwoObjectLines(t *testing.T, source []byte) []byte {
	t.Helper()
	firstStart := bytes.IndexByte(source, '\n') + 1
	firstEndRelative := bytes.IndexByte(source[firstStart:], '\n')
	if firstStart == 0 || firstEndRelative < 0 {
		t.Fatal("first JSON member line not found")
	}
	firstEnd := firstStart + firstEndRelative + 1
	secondEndRelative := bytes.IndexByte(source[firstEnd:], '\n')
	if secondEndRelative < 0 {
		t.Fatal("second JSON member line not found")
	}
	secondEnd := firstEnd + secondEndRelative + 1
	result := append([]byte{}, source[:firstStart]...)
	result = append(result, source[firstEnd:secondEnd]...)
	result = append(result, source[firstStart:firstEnd]...)
	return append(result, source[secondEnd:]...)
}

func httpDuplicateFirstObjectLine(t *testing.T, source []byte) []byte {
	t.Helper()
	start := bytes.IndexByte(source, '\n') + 1
	endRelative := bytes.IndexByte(source[start:], '\n')
	if start == 0 || endRelative < 0 {
		t.Fatal("JSON member line not found")
	}
	end := start + endRelative + 1
	return append(append(append([]byte{}, source[:end]...), source[start:end]...), source[end:]...)
}

func httpReplaceCorpusJSONValue(t *testing.T, source []byte, pointer, replacement string) []byte {
	t.Helper()
	if pointer == "/summary" {
		start := bytes.Index(source, []byte("  \"summary\": "))
		end := bytes.Index(source[start:], []byte("\n  },\n"))
		if start < 0 || end < 0 {
			t.Fatal("summary value not found")
		}
		valueStart := start + len("  \"summary\": ")
		valueEnd := start + end + len("\n  }")
		return append(append(append([]byte{}, source[:valueStart]...), replacement...), source[valueEnd:]...)
	}
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	name := parts[len(parts)-1]
	indent := 2
	if strings.HasPrefix(pointer, "/summary/") {
		indent = 4
	} else if strings.HasPrefix(pointer, "/scenarios/0/observation/") {
		indent = 8
	}
	pattern := []byte("\n" + strings.Repeat(" ", indent) + strconv.Quote(name) + ": ")
	start := bytes.Index(source, pattern)
	if start < 0 {
		t.Fatalf("JSON pointer %s not found", pointer)
	}
	valueStart := start + len(pattern)
	valueEnd := valueStart
	inString := false
	escaped := false
	for valueEnd < len(source) {
		character := source[valueEnd]
		if inString {
			if escaped {
				escaped = false
			} else if character == '\\' {
				escaped = true
			} else if character == '"' {
				inString = false
			}
		} else if character == '"' {
			inString = true
		} else if character == ',' || character == '\n' {
			break
		}
		valueEnd++
	}
	if valueEnd == valueStart || inString {
		t.Fatalf("JSON pointer %s has no scalar value", pointer)
	}
	return append(append(append([]byte{}, source[:valueStart]...), replacement...), source[valueEnd:]...)
}

func httpSwapFileContents(t *testing.T, left, right string) {
	t.Helper()
	leftContents, err := os.ReadFile(left)
	if err != nil {
		t.Fatal(err)
	}
	rightContents, err := os.ReadFile(right)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(left, rightContents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(right, leftContents, 0o600); err != nil {
		t.Fatal(err)
	}
}
