package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestFocusedTopLevelRouting(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, strings.NewReader("unused"), &stdout, &stderr); code != 0 {
		t.Fatalf("version exit = %d, stderr = %q", code, stderr.String())
	}
	if stdout.String() != "http-retry-check 0.0.0-dev\n" || stderr.Len() != 0 {
		t.Fatalf("version output = %q/%q", stdout.String(), stderr.String())
	}

	for _, args := range [][]string{
		nil,
		{"doctor"},
		{"run", "-"},
		{"pack-run", "fixtures"},
		{"version", "extra"},
		{"--version", "extra"},
		{"unknown"},
	} {
		stdout.Reset()
		stderr.Reset()
		code := Run(context.Background(), args, strings.NewReader("unused"), &stdout, &stderr)
		if code != 2 || stdout.Len() != 0 || stderr.String() != usage {
			t.Errorf("Run(%q) = %d/%q/%q", args, code, stdout.String(), stderr.String())
		}
	}
}

func TestRunRejectsNilDependencies(t *testing.T) {
	ctx := context.Background()
	reader := strings.NewReader("")
	var stdout, stderr bytes.Buffer
	for _, call := range []func() int{
		func() int { return Run(nil, []string{"version"}, reader, &stdout, &stderr) },
		func() int { return Run(ctx, []string{"version"}, nil, &stdout, &stderr) },
		func() int { return Run(ctx, []string{"version"}, reader, nil, &stderr) },
		func() int { return Run(ctx, []string{"version"}, reader, &stdout, nil) },
	} {
		if code := call(); code != 3 {
			t.Fatalf("nil dependency exit = %d", code)
		}
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("nil dependency output = %q/%q", stdout.String(), stderr.String())
	}
}

func TestCLIOutputFailuresReturnInternalExit(t *testing.T) {
	var diagnostic bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, strings.NewReader(""), failingCLIWriter{}, &diagnostic); code != 3 {
		t.Fatalf("stdout failure exit = %d", code)
	}
	if diagnostic.String() != "http-retry-check internal failure\n" {
		t.Fatalf("stdout failure diagnostic = %q", diagnostic.String())
	}
	if code := Run(context.Background(), nil, strings.NewReader(""), io.Discard, failingCLIWriter{}); code != 3 {
		t.Fatalf("stderr failure exit = %d", code)
	}
}

type failingCLIWriter struct{}

func (failingCLIWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}
