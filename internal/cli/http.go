package cli

import (
	"errors"
	"io"

	"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/evidence"
	"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/localfs"
)

const (
	httpInitUsage     = "usage: http-retry-check http init <go|csharp> <new-scaffold-directory>\n"
	httpValidateUsage = "usage: http-retry-check http validate <report-path|artifact-directory|->\n"
	httpCheckUsage    = "usage: http-retry-check http check <report-path|artifact-directory|->\n"
	httpExplainUsage  = "usage: http-retry-check http explain <report-path|->\n"
	httpArtifactUsage = "usage: http-retry-check http artifact <report-path|-> <new-output-directory>\n"

	helpTopicHTTP = httpInitUsage + httpValidateUsage + httpCheckUsage + httpExplainUsage + httpArtifactUsage + `
Creates Go or C# test scaffolds and reads existing evidence; your test runs the client.
`
)

func httpCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		return httpFixedError(stderr, helpTopicHTTP)
	}
	switch args[1] {
	case "init":
		if len(args) != 4 || (args[2] != "go" && args[2] != "csharp") {
			return httpFixedError(stderr, httpInitUsage)
		}
		return httpInitialize(args[2], args[3], stdout, stderr)
	case "validate":
		if len(args) != 3 {
			return httpFixedError(stderr, httpValidateUsage)
		}
		return httpValidate(args[2], stdin, stdout, stderr)
	case "check":
		if len(args) != 3 {
			return httpFixedError(stderr, httpCheckUsage)
		}
		return httpCheck(args[2], stdin, stdout, stderr)
	case "explain":
		if len(args) != 3 {
			return httpFixedError(stderr, httpExplainUsage)
		}
		return httpExplain(args[2], stdin, stdout, stderr)
	case "artifact":
		if len(args) != 4 {
			return httpFixedError(stderr, httpArtifactUsage)
		}
		return httpArtifact(args[2], args[3], stdin, stdout, stderr)
	default:
		return httpFixedError(stderr, helpTopicHTTP)
	}
}

func httpInitialize(language, destination string, stdout, stderr io.Writer) int {
	err := localfs.CreateScaffold(language, destination)
	if err == nil {
		return httpOutput(stdout, stderr, []byte("HTTP Retry Check scaffold created\n"), 0)
	}
	if errors.Is(err, localfs.ErrTargetUnavailable) {
		return httpFixedError(stderr, "HTTP Retry Check scaffold target is unavailable\n")
	}
	return httpInternalError(stderr)
}

func httpValidate(source string, stdin io.Reader, stdout, stderr io.Writer) int {
	_, err := httpAdmit(source, stdin, true)
	if err != nil {
		return httpAdmissionError(err, stderr)
	}
	return httpOutput(stdout, stderr, []byte("HTTP Retry Check evidence is valid\n"), 0)
}

func httpCheck(source string, stdin io.Reader, stdout, stderr io.Writer) int {
	report, err := httpAdmit(source, stdin, true)
	if err != nil {
		return httpAdmissionError(err, stderr)
	}
	switch report.Outcome {
	case evidence.OutcomePass:
		return httpOutput(stdout, stderr, []byte("HTTP Retry Check found no unsafe behavior\n"), 0)
	case evidence.OutcomeFail:
		return httpOutput(stdout, stderr, []byte("HTTP Retry Check found unsafe behavior\n"), 1)
	case evidence.OutcomeInconclusive:
		return httpOutput(stdout, stderr, []byte("HTTP Retry Check is inconclusive\n"), 1)
	default:
		return httpInternalError(stderr)
	}
}

func httpExplain(source string, stdin io.Reader, stdout, stderr io.Writer) int {
	report, err := httpAdmit(source, stdin, false)
	if err != nil {
		return httpAdmissionError(err, stderr)
	}
	projection, err := evidence.Explain(report)
	if err != nil {
		return httpInternalError(stderr)
	}
	return httpOutput(stdout, stderr, projection, 0)
}

func httpArtifact(source, destination string, stdin io.Reader, stdout, stderr io.Writer) int {
	report, err := httpAdmit(source, stdin, false)
	if err != nil {
		return httpAdmissionError(err, stderr)
	}
	files, err := evidence.BuildArtifact(report)
	if err != nil || evidence.ValidateArtifact(files) != nil {
		return httpInternalError(stderr)
	}
	err = localfs.WriteArtifact(files, destination)
	if err == nil {
		return httpOutput(stdout, stderr, []byte("HTTP Retry Check artifact created\n"), 0)
	}
	if errors.Is(err, localfs.ErrTargetUnavailable) {
		return httpFixedError(stderr, "HTTP Retry Check artifact target is unavailable\n")
	}
	return httpInternalError(stderr)
}

func httpAdmit(source string, stdin io.Reader, allowArtifact bool) (evidence.Report, error) {
	admission, err := localfs.ReadEvidence(source, stdin)
	if err != nil {
		return evidence.Report{}, err
	}
	if admission.Artifact != nil {
		if !allowArtifact || evidence.ValidateArtifact(admission.Artifact) != nil {
			return evidence.Report{}, localfs.ErrInvalidSource
		}
		report, decodeErr := evidence.DecodeReport(admission.Artifact[1].Contents)
		if decodeErr != nil {
			return evidence.Report{}, localfs.ErrInternalFailure
		}
		return report, nil
	}
	return evidence.DecodeReport(admission.Report)
}

func httpAdmissionError(err error, stderr io.Writer) int {
	if errors.Is(err, localfs.ErrInternalFailure) {
		return httpInternalError(stderr)
	}
	return httpFixedError(stderr, "HTTP Retry Check evidence is invalid\n")
}

func httpFixedError(stderr io.Writer, message string) int {
	written, err := io.WriteString(stderr, message)
	if err != nil || written != len(message) {
		return 3
	}
	return 2
}

func httpInternalError(stderr io.Writer) int {
	const message = "http-retry-check internal failure\n"
	_, _ = io.WriteString(stderr, message)
	return 3
}

func httpOutput(stdout, stderr io.Writer, contents []byte, completedExit int) int {
	written, err := stdout.Write(contents)
	if err != nil || written != len(contents) {
		return httpInternalError(stderr)
	}
	return completedExit
}
