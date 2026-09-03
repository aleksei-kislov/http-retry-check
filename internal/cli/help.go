package cli

import (
	"io"
	"regexp"
	"runtime/debug"
	"strings"
)

const developmentVersion = "0.0.0-dev"

var pseudoVersionPattern = regexp.MustCompile(`[.-][0-9]{14}-[0-9a-f]{12,}(?:$|\+)`)

const helpUsage = "usage: http-retry-check help [command]\n"

const helpReference = `HTTP Retry Check command reference

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

const (
	helpTopicHelp = `usage: http-retry-check help [command]

Shows this complete reference or help for one top-level command.
`
	helpTopicVersion = `usage: http-retry-check version

Prints the tagged module version or the local development fallback.
`
)

// helpCommand prints the command reference without reading anything else.
func helpCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		return writeOutput(stdout, stderr, []byte(helpReference))
	}
	if args[0] != "help" || len(args) != 2 {
		return helpUsageError(stderr)
	}
	topic, ok := helpTopic(args[1])
	if !ok {
		return helpUsageError(stderr)
	}
	return writeOutput(stdout, stderr, []byte(topic))
}

func helpTopic(command string) (string, bool) {
	switch command {
	case "help":
		return helpTopicHelp, true
	case "version":
		return helpTopicVersion, true
	case "http":
		return helpTopicHTTP, true
	default:
		return "", false
	}
}

func helpUsageError(stderr io.Writer) int {
	written, err := io.WriteString(stderr, helpUsage)
	if err != nil || written != len(helpUsage) {
		return 3
	}
	return 2
}

func versionCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		return usageError(stderr)
	}
	return writeOutput(stdout, stderr, []byte("http-retry-check "+buildVersion()+"\n"))
}

func buildVersion() string {
	information, ok := debug.ReadBuildInfo()
	if !ok {
		return developmentVersion
	}
	return selectBuildVersion(information.Main.Version)
}

func selectBuildVersion(version string) string {
	if version == "" || version == "(devel)" || !strings.HasPrefix(version, "v") ||
		strings.ContainsAny(version, " \t\r\n") || strings.Contains(version, "+dirty") ||
		pseudoVersionPattern.MatchString(version) {
		return developmentVersion
	}
	return version
}
