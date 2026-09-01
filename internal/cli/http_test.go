package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"
	publicreport "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1/report"
)

func TestHTTPCommandUsageAndHelpNeedNoExternalAccess(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{args: []string{"http"}, want: helpTopicHTTP},
		{args: []string{"http", "PRIVATE"}, want: helpTopicHTTP},
		{args: []string{"http", "init"}, want: httpInitUsage},
		{args: []string{"http", "init", "PRIVATE", "/PRIVATE"}, want: httpInitUsage},
		{args: []string{"http", "init", "go"}, want: httpInitUsage},
		{args: []string{"http", "validate"}, want: httpValidateUsage},
		{args: []string{"http", "validate", "-", "PRIVATE"}, want: httpValidateUsage},
		{args: []string{"http", "check"}, want: httpCheckUsage},
		{args: []string{"http", "explain"}, want: httpExplainUsage},
		{args: []string{"http", "artifact", "-"}, want: httpArtifactUsage},
		{args: []string{"http", "artifact", "-", "/PRIVATE", "extra"}, want: httpArtifactUsage},
	}
	for _, test := range tests {
		reader := &httpForbiddenReader{}
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), test.args, reader, &stdout, &stderr)
		if code != 2 || reader.reads != 0 || stdout.Len() != 0 || stderr.String() != test.want ||
			strings.Contains(stderr.String(), "PRIVATE") {
			t.Fatalf("%v = exit %d reads %d stdout %q stderr %q", test.args, code, reader.reads, stdout.String(), stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"help", "http"}, &httpForbiddenReader{}, &stdout, &stderr); code != 0 ||
		stdout.String() != helpTopicHTTP || stderr.Len() != 0 {
		t.Fatalf("help http = %d/%q/%q", code, stdout.String(), stderr.String())
	}
}

func TestHTTPInitCreatesGoAndCSharpScaffolds(t *testing.T) {
	for _, test := range []struct {
		language string
		names    []string
	}{
		{language: "go", names: []string{"README.md", "http_retry_test.go"}},
		{language: "csharp", names: []string{"HttpRetryTests.cs", "README.md"}},
	} {
		t.Run(test.language, func(t *testing.T) {
			parent := httpRealTempDir(t)
			destination := filepath.Join(parent, test.language)
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"http", "init", test.language, destination}, &httpForbiddenReader{}, &stdout, &stderr)
			if code != 0 || stdout.String() != "HTTP Retry Check scaffold created\n" || stderr.Len() != 0 {
				t.Fatalf("init = %d/%q/%q", code, stdout.String(), stderr.String())
			}
			entries, err := os.ReadDir(destination)
			if err != nil || len(entries) != 2 || entries[0].Name() != test.names[0] || entries[1].Name() != test.names[1] {
				t.Fatalf("entries = %#v/%v", entries, err)
			}
			stdout.Reset()
			code = Run(context.Background(), []string{"http", "init", test.language, destination}, &httpForbiddenReader{}, &stdout, &stderr)
			if code != 2 || stdout.Len() != 0 || stderr.String() != "HTTP Retry Check scaffold target is unavailable\n" {
				t.Fatalf("second init = %d/%q/%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestHTTPEvidenceCommandsPreserveThreeOutcomeMeanings(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		checkCode int
		checkLine string
	}{
		{name: "pass", input: httpReportBytes(t, httpPositiveResult()), checkCode: 0, checkLine: "HTTP Retry Check found no unsafe behavior\n"},
		{name: "fail", input: httpReportBytes(t, httpUnsafeResult()), checkCode: 1, checkLine: "HTTP Retry Check found unsafe behavior\n"},
		{name: "inconclusive", input: httpReportBytes(t, httpInconclusiveResult()), checkCode: 1, checkLine: "HTTP Retry Check is inconclusive\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, source := range []struct {
				name  string
				value string
				stdin io.Reader
			}{
				{name: "stdin", value: "-", stdin: bytes.NewReader(test.input)},
				{name: "file", value: httpWriteReport(t, test.input), stdin: &httpForbiddenReader{}},
			} {
				var stdout, stderr bytes.Buffer
				code := Run(context.Background(), []string{"http", "validate", source.value}, source.stdin, &stdout, &stderr)
				if code != 0 || stdout.String() != "HTTP Retry Check evidence is valid\n" || stderr.Len() != 0 {
					t.Fatalf("validate/%s = %d/%q/%q", source.name, code, stdout.String(), stderr.String())
				}
			}
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"http", "check", "-"}, bytes.NewReader(test.input), &stdout, &stderr)
			if code != test.checkCode || stdout.String() != test.checkLine || stderr.Len() != 0 {
				t.Fatalf("check = %d/%q/%q", code, stdout.String(), stderr.String())
			}
			stdout.Reset()
			code = Run(context.Background(), []string{"http", "explain", "-"}, bytes.NewReader(test.input), &stdout, &stderr)
			rowCount := bytes.Count(stdout.Bytes(), []byte("\nPASS ")) +
				bytes.Count(stdout.Bytes(), []byte("\nUNSAFE ")) +
				bytes.Count(stdout.Bytes(), []byte("\nINCONCLUSIVE "))
			if code != 0 || rowCount != 6 ||
				!bytes.HasPrefix(stdout.Bytes(), []byte("HTTP Retry Check: ")) || stderr.Len() != 0 {
				t.Fatalf("explain = %d/%q/%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestHTTPCommandsAcceptIncompleteChangedBodyUnsafeReport(t *testing.T) {
	input := httpReportBytes(t, httpIncompleteChangedBodyResult())
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"http", "validate", "-"}, bytes.NewReader(input), &stdout, &stderr)
	if code != 0 || stdout.String() != "HTTP Retry Check evidence is valid\n" || stderr.Len() != 0 {
		t.Fatalf("validate = %d/%q/%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	code = Run(context.Background(), []string{"http", "check", "-"}, bytes.NewReader(input), &stdout, &stderr)
	if code != 1 || stdout.String() != "HTTP Retry Check found unsafe behavior\n" || stderr.Len() != 0 {
		t.Fatalf("check = %d/%q/%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	code = Run(context.Background(), []string{"http", "explain", "-"}, bytes.NewReader(input), &stdout, &stderr)
	want := []string{
		"UNSAFE changed_body_retry\n",
		"  capture_incomplete: ",
		"  body_changed: ",
		"  credential_not_observed: ",
		"  effect_not_observed: ",
	}
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("explain = %d/%q/%q", code, stdout.String(), stderr.String())
	}
	for _, fragment := range want {
		if !strings.Contains(stdout.String(), fragment) {
			t.Fatalf("explain missing %q: %q", fragment, stdout.String())
		}
	}
}

func TestHTTPArtifactRoundTripIsExplicitAndNoOverwrite(t *testing.T) {
	input := httpReportBytes(t, httpPositiveResult())
	parent := httpRealTempDir(t)
	destination := filepath.Join(parent, "artifact")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"http", "artifact", "-", destination}, bytes.NewReader(input), &stdout, &stderr)
	if code != 0 || stdout.String() != "HTTP Retry Check artifact created\n" || stderr.Len() != 0 {
		t.Fatalf("artifact = %d/%q/%q", code, stdout.String(), stderr.String())
	}
	for _, command := range []string{"validate", "check"} {
		stdout.Reset()
		stderr.Reset()
		code = Run(context.Background(), []string{"http", command, destination}, &httpForbiddenReader{}, &stdout, &stderr)
		if code != 0 || stderr.Len() != 0 {
			t.Fatalf("%s artifact = %d/%q/%q", command, code, stdout.String(), stderr.String())
		}
	}
	for _, command := range []string{"explain", "artifact"} {
		stdout.Reset()
		stderr.Reset()
		args := []string{"http", command, destination}
		if command == "artifact" {
			args = append(args, filepath.Join(parent, "copy"))
		}
		code = Run(context.Background(), args, &httpForbiddenReader{}, &stdout, &stderr)
		if code != 2 || stdout.Len() != 0 || stderr.String() != "HTTP Retry Check evidence is invalid\n" {
			t.Fatalf("%s artifact input = %d/%q/%q", command, code, stdout.String(), stderr.String())
		}
	}
	stdout.Reset()
	stderr.Reset()
	code = Run(context.Background(), []string{"http", "artifact", "-", destination}, bytes.NewReader(input), &stdout, &stderr)
	if code != 2 || stdout.Len() != 0 || stderr.String() != "HTTP Retry Check artifact target is unavailable\n" {
		t.Fatalf("artifact overwrite = %d/%q/%q", code, stdout.String(), stderr.String())
	}
}

func TestHTTPInvalidAndInternalFailuresAreSanitized(t *testing.T) {
	markerRoot := httpRealTempDir(t)
	markerPath := filepath.Join(markerRoot, "PRIVATE-SOURCE")
	if err := os.WriteFile(markerPath, []byte(`{"PRIVATE":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(markerRoot, "PRIVATE-LINK")
	if err := os.Symlink(markerPath, symlink); err != nil {
		t.Fatal(err)
	}
	for _, source := range []struct {
		value string
		stdin io.Reader
	}{
		{value: "-", stdin: bytes.NewReader(nil)},
		{value: "-", stdin: bytes.NewReader(bytes.Repeat([]byte("x"), publicreport.MaxProjectionBytes+1))},
		{value: markerPath, stdin: &httpForbiddenReader{}},
		{value: symlink, stdin: &httpForbiddenReader{}},
		{value: filepath.Join(markerRoot, "PRIVATE-ABSENT"), stdin: &httpForbiddenReader{}},
	} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), []string{"http", "validate", source.value}, source.stdin, &stdout, &stderr)
		if code != 2 || stdout.Len() != 0 || stderr.String() != "HTTP Retry Check evidence is invalid\n" ||
			strings.Contains(stdout.String()+stderr.String(), "PRIVATE") {
			t.Fatalf("invalid %q = %d/%q/%q", source.value, code, stdout.String(), stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"http", "validate", "-"}, httpFailingReader{}, &stdout, &stderr)
	if code != 3 || stdout.Len() != 0 || stderr.String() != "http-retry-check internal failure\n" {
		t.Fatalf("read failure = %d/%q/%q", code, stdout.String(), stderr.String())
	}
}

func TestHTTPOutputFailuresReturnInternalExit(t *testing.T) {
	input := httpReportBytes(t, httpPositiveResult())
	for _, output := range []io.Writer{httpFailingWriter{}, httpShortWriter{}} {
		stderr := &httpCountingWriter{}
		code := Run(context.Background(), []string{"http", "validate", "-"}, bytes.NewReader(input), output, stderr)
		if code != 3 || stderr.writes != 1 || string(stderr.contents) != "http-retry-check internal failure\n" {
			t.Fatalf("%T stdout failure = %d/%d/%q", output, code, stderr.writes, stderr.contents)
		}
	}
	for _, diagnostic := range []io.Writer{httpFailingWriter{}, httpShortWriter{}} {
		code := Run(context.Background(), []string{"http", "validate"}, &httpForbiddenReader{}, io.Discard, diagnostic)
		if code != 3 {
			t.Fatalf("%T diagnostic failure = %d", diagnostic, code)
		}
	}
}

func httpReportBytes(t *testing.T, result httpcheck.Result) []byte {
	t.Helper()
	report, err := publicreport.New(result)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := publicreport.Encode(report)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func httpPositiveResult() httpcheck.Result {
	return httpcheck.Result{
		Assessment: httpcheck.AssessmentNoUnsafeBehaviorObserved,
		Scenarios: []httpcheck.ScenarioResult{
			httpPositiveRow(httpcheck.ScenarioAcceptThenDisconnect, httpObservation(1, 1, 0, 0, false, 0, httpcheck.CredentialSourceOnly)),
			httpPositiveRow(httpcheck.ScenarioDisconnectBeforeAcceptance, httpObservation(1, 0, 0, 0, false, 0, httpcheck.CredentialSourceOnly)),
			httpPositiveRow(httpcheck.ScenarioChangedBodyRetry, httpObservation(1, 1, 0, 0, false, 0, httpcheck.CredentialSourceOnly)),
			httpPositiveRow(httpcheck.ScenarioCrossOriginRedirectCredentials, httpObservation(2, 1, 2, 2, true, 0, httpcheck.CredentialAbsentAtTarget)),
			httpPositiveRow(httpcheck.ScenarioRetryLimit, httpObservation(2, 0, 2, 2, true, 0, httpcheck.CredentialSourceOnly)),
			httpPositiveRow(httpcheck.ScenarioDelayedResponse, httpObservation(1, 1, 1, 1, true, 1, httpcheck.CredentialSourceOnly)),
		},
	}
}

func httpUnsafeResult() httpcheck.Result {
	result := httpPositiveResult()
	result.Assessment = httpcheck.AssessmentUnsafeBehaviorObserved
	row := &result.Scenarios[4]
	row.Assessment = httpcheck.AssessmentUnsafeBehaviorObserved
	row.Observation.AttemptCount = 3
	row.Observation.ResponseAttemptCount = 3
	row.Observation.ResponseCompleteCount = 3
	row.Findings = []httpcheck.FindingCode{httpcheck.FindingAttemptLimitExceeded}
	return result
}

func httpInconclusiveResult() httpcheck.Result {
	result := httpPositiveResult()
	result.Assessment = httpcheck.AssessmentInconclusive
	result.Scenarios[0] = httpcheck.ScenarioResult{
		Scenario: httpcheck.ScenarioAcceptThenDisconnect, Assessment: httpcheck.AssessmentInconclusive,
		Observation: httpcheck.Observation{
			MethodConsistent: true, DestinationConsistent: true, BodyConsistent: true,
			Credential: httpcheck.CredentialNotObserved, Cleanup: httpcheck.CleanupSucceeded,
		},
		Findings: []httpcheck.FindingCode{httpcheck.FindingAttemptNotObserved, httpcheck.FindingCaptureIncomplete},
	}
	return result
}

func httpIncompleteChangedBodyResult() httpcheck.Result {
	result := httpPositiveResult()
	result.Assessment = httpcheck.AssessmentUnsafeBehaviorObserved
	result.Scenarios[2] = httpcheck.ScenarioResult{
		Scenario: httpcheck.ScenarioChangedBodyRetry, Assessment: httpcheck.AssessmentUnsafeBehaviorObserved,
		Observation: httpcheck.Observation{
			CaptureComplete: false, AttemptCount: 1,
			MethodConsistent: true, DestinationConsistent: true, BodyConsistent: false,
			Credential: httpcheck.CredentialNotObserved, Cleanup: httpcheck.CleanupSucceeded,
		},
		Findings: []httpcheck.FindingCode{
			httpcheck.FindingCaptureIncomplete,
			httpcheck.FindingBodyChanged,
			httpcheck.FindingCredentialNotObserved,
			httpcheck.FindingEffectNotObserved,
		},
	}
	return result
}

func httpObservation(attempts uint32, effects uint64, responseAttempts, responseComplete uint32, first bool, delay uint32, credential httpcheck.CredentialState) httpcheck.Observation {
	return httpcheck.Observation{
		CaptureComplete: true, AttemptCount: attempts, EffectCount: effects,
		ResponseAttemptCount: responseAttempts, ResponseCompleteCount: responseComplete,
		FirstResponseComplete: first, DelayCompleteCount: delay,
		MethodConsistent: true, DestinationConsistent: true, BodyConsistent: true,
		Credential: credential, Cleanup: httpcheck.CleanupSucceeded,
	}
}

func httpPositiveRow(scenario httpcheck.ScenarioID, observation httpcheck.Observation) httpcheck.ScenarioResult {
	return httpcheck.ScenarioResult{
		Scenario: scenario, Assessment: httpcheck.AssessmentNoUnsafeBehaviorObserved,
		Observation: observation, Findings: []httpcheck.FindingCode{},
	}
}

func httpWriteReport(t *testing.T, contents []byte) string {
	t.Helper()
	path := filepath.Join(httpRealTempDir(t), "report.json")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func httpRealTempDir(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

type httpForbiddenReader struct{ reads int }

func (reader *httpForbiddenReader) Read([]byte) (int, error) {
	reader.reads++
	return 0, errors.New("HTTP command must not read this source")
}

type httpFailingReader struct{}

func (httpFailingReader) Read([]byte) (int, error) { return 0, errors.New("PRIVATE read failure") }

type httpFailingWriter struct{}

func (httpFailingWriter) Write([]byte) (int, error) { return 0, errors.New("PRIVATE write failure") }

type httpShortWriter struct{}

func (httpShortWriter) Write(value []byte) (int, error) {
	if len(value) == 0 {
		return 0, nil
	}
	return len(value) - 1, nil
}

type httpCountingWriter struct {
	writes   int
	contents []byte
}

func (writer *httpCountingWriter) Write(value []byte) (int, error) {
	writer.writes++
	writer.contents = append(writer.contents, value...)
	return len(value), nil
}
