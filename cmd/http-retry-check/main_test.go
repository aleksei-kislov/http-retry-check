package main

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuiltBinaryExposesOnlyFocusedHTTPCommands(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "http-retry-check")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, output)
	}

	tests := []struct {
		args       []string
		wantExit   int
		wantStdout string
		wantStderr string
	}{
		{args: []string{"version"}, wantStdout: "http-retry-check 0.0.0-dev\n"},
		{args: []string{"help", "http"}, wantStdout: "usage: http-retry-check http init <go|csharp> <new-scaffold-directory>\n"},
		{args: []string{"doctor"}, wantExit: 2, wantStderr: "usage: http-retry-check <command>\n"},
	}
	for _, test := range tests {
		command := exec.Command(binary, test.args...)
		var stdout, stderr bytes.Buffer
		command.Stdout = &stdout
		command.Stderr = &stderr
		err := command.Run()
		exit := 0
		if err != nil {
			var exitError *exec.ExitError
			if !errors.As(err, &exitError) {
				t.Fatalf("run %q: %v", test.args, err)
			}
			exit = exitError.ExitCode()
		}
		if exit != test.wantExit || !strings.HasPrefix(stdout.String(), test.wantStdout) ||
			!strings.HasPrefix(stderr.String(), test.wantStderr) ||
			(test.wantExit == 0 && stderr.Len() != 0) || (test.wantExit != 0 && stdout.Len() != 0) {
			t.Errorf("run %q = exit %d, stdout %q, stderr %q", test.args, exit, stdout.String(), stderr.String())
		}
	}
}
