package cli

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestHelpReferenceAndTopics(t *testing.T) {
	const wantReference = `HTTP Retry Check command reference

usage:
  http-retry-check help [command]
  http-retry-check <command> [arguments]

commands:
  help [command]       show this reference or help for one command
  version              print the build version
  http <subcommand>    create test scaffolds or read HTTP evidence

aliases:
  -h, --help           show this complete command reference
  --version            print the build version

Use "http-retry-check help <command>" for help with a command.
`
	if helpReference != wantReference {
		t.Fatalf("help reference = %q", helpReference)
	}

	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), args, panicCLIReader{}, &stdout, &stderr); code != 0 || stdout.String() != wantReference || stderr.Len() != 0 {
			t.Errorf("Run(%q) = %d/%q/%q", args, code, stdout.String(), stderr.String())
		}
	}

	topics := map[string]string{
		"help":    helpTopicHelp,
		"version": helpTopicVersion,
		"http":    helpTopicHTTP,
	}
	for command, want := range topics {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), []string{"help", command}, panicCLIReader{}, &stdout, &stderr); code != 0 || stdout.String() != want || stderr.Len() != 0 {
			t.Errorf("help %s = %d/%q/%q", command, code, stdout.String(), stderr.String())
		}
		if got, ok := helpTopic(command); !ok || got != want {
			t.Errorf("helpTopic(%q) = %q/%t", command, got, ok)
		}
	}
}

func TestSelectBuildVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "v0.1.0", want: "v0.1.0"},
		{input: "v1.2.3-rc.1", want: "v1.2.3-rc.1"},
		{input: "", want: developmentVersion},
		{input: "(devel)", want: developmentVersion},
		{input: "0.1.0", want: developmentVersion},
		{input: "v0.0.0-20260831111912-768f4650f2fb", want: developmentVersion},
		{input: "v0.1.0+dirty", want: developmentVersion},
		{input: "v0.1.0 unexpected", want: developmentVersion},
	}
	for _, test := range tests {
		if got := selectBuildVersion(test.input); got != test.want {
			t.Errorf("selectBuildVersion(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestUnknownHelpTopicsAreRejectedWithoutEcho(t *testing.T) {
	for _, args := range [][]string{{"help", "unknown-command"}, {"help", "http", "extra"}, {"--help", "extra"}} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), args, panicCLIReader{}, &stdout, &stderr)
		if code != 2 || stdout.Len() != 0 || stderr.String() != helpUsage {
			t.Errorf("Run(%q) = %d/%q/%q", args, code, stdout.String(), stderr.String())
		}
	}
	if topic, ok := helpTopic("unknown-command"); ok || topic != "" {
		t.Fatalf("unknown topic = %q/%t", topic, ok)
	}
}

type panicCLIReader struct{}

func (panicCLIReader) Read([]byte) (int, error) {
	panic("help and version must not read stdin")
}

var _ io.Reader = panicCLIReader{}
