// Package cli implements the focused HTTP Retry Check command surface.
package cli

import (
	"context"
	"io"
)

const usage = `usage: http-retry-check <command>

commands:
  help [command]  show the command reference
  version         print the build version
  http            create test scaffolds or read HTTP evidence
`

// Run executes one CLI invocation. Args excludes the executable name.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if ctx == nil || stdin == nil || stdout == nil || stderr == nil {
		return 3
	}
	if len(args) == 0 {
		return usageError(stderr)
	}
	switch args[0] {
	case "help", "-h", "--help":
		return helpCommand(args, stdout, stderr)
	case "version", "--version":
		return versionCommand(args, stdout, stderr)
	case "http":
		return httpCommand(args, stdin, stdout, stderr)
	default:
		return usageError(stderr)
	}
}

func usageError(stderr io.Writer) int {
	written, err := io.WriteString(stderr, usage)
	if err != nil || written != len(usage) {
		return 3
	}
	return 2
}

func internalError(stderr io.Writer) int {
	const message = "http-retry-check internal failure\n"
	_, _ = io.WriteString(stderr, message)
	return 3
}

func writeOutput(stdout, stderr io.Writer, contents []byte) int {
	written, err := stdout.Write(contents)
	if err != nil || written != len(contents) {
		return internalError(stderr)
	}
	return 0
}
