//go:build darwin || (linux && (amd64 || arm64))

package localfs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestWriteArtifactCleansOwnedEmptyStageWhenProbeIsUnsupported(t *testing.T) {
	_, files := validEvidence(t)
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "EINVAL", err: syscall.EINVAL},
		{name: "ENOTSUP", err: syscall.ENOTSUP},
		{name: "EOPNOTSUPP", err: syscall.EOPNOTSUPP},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent := realTempDir(t)
			destination := filepath.Join(parent, "artifact")
			calls := 0
			err := writeArtifact(files, destination, func(_ *os.File, oldName, newName string) error {
				calls++
				if oldName != stagingLeaf || newName != stagingLeaf {
					t.Errorf("probe rename = %q -> %q", oldName, newName)
				}
				stagePath := filepath.Join(parent, stagingLeaf)
				identity, inspectErr := os.Lstat(stagePath)
				entries, readErr := os.ReadDir(stagePath)
				if inspectErr != nil || readErr != nil || !identity.IsDir() || len(entries) != 0 {
					t.Errorf("probe stage was not an owned empty directory: identity=%v entries=%d inspect=%v read=%v",
						identity, len(entries), inspectErr, readErr)
				}
				return test.err
			})
			if !errors.Is(err, ErrTargetUnavailable) || calls != 1 {
				t.Fatalf("unsupported probe = %v after %d calls", err, calls)
			}
			assertArtifactPathsAbsent(t, parent, destination)
		})
	}
}

func TestWriteArtifactMapsFinalExistToUnavailableAndCleansStage(t *testing.T) {
	_, files := validEvidence(t)
	parent := realTempDir(t)
	destination := filepath.Join(parent, "artifact")
	calls := 0
	var concurrentIdentity os.FileInfo
	err := writeArtifact(files, destination, func(_ *os.File, oldName, newName string) error {
		calls++
		switch calls {
		case 1:
			if oldName != stagingLeaf || newName != stagingLeaf {
				t.Errorf("probe rename = %q -> %q", oldName, newName)
			}
		case 2:
			if oldName != stagingLeaf || newName != filepath.Base(destination) {
				t.Errorf("final rename = %q -> %q", oldName, newName)
			}
			entries, readErr := os.ReadDir(filepath.Join(parent, stagingLeaf))
			if readErr != nil || len(entries) != len(files) {
				t.Errorf("final rename stage inventory = %d/%v", len(entries), readErr)
			}
			file, createErr := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if createErr == nil {
				_, createErr = file.Write([]byte("concurrent caller"))
			}
			if createErr == nil {
				concurrentIdentity, createErr = file.Stat()
			}
			if file != nil {
				if closeErr := file.Close(); createErr == nil {
					createErr = closeErr
				}
			}
			if createErr != nil {
				t.Errorf("concurrent final creation failed: %v", createErr)
			}
		default:
			t.Errorf("unexpected rename call %d", calls)
		}
		return syscall.EEXIST
	})
	if !errors.Is(err, ErrTargetUnavailable) || calls != 2 {
		t.Fatalf("final EEXIST = %v after %d calls", err, calls)
	}
	if _, err := os.Lstat(filepath.Join(parent, stagingLeaf)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging path was not cleaned: %v", err)
	}
	current, err := os.Lstat(destination)
	if err != nil || concurrentIdentity == nil || !os.SameFile(concurrentIdentity, current) {
		t.Fatalf("concurrent final identity changed: %v", err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil || string(contents) != "concurrent caller" {
		t.Fatalf("concurrent final bytes changed: %q/%v", contents, err)
	}
}

func TestWriteArtifactTreatsUnexpectedRenameErrorsAsInternal(t *testing.T) {
	_, files := validEvidence(t)
	for _, test := range []struct {
		name     string
		finalErr error
	}{
		{name: "probe", finalErr: nil},
		{name: "final", finalErr: syscall.EIO},
	} {
		t.Run(test.name, func(t *testing.T) {
			parent := realTempDir(t)
			destination := filepath.Join(parent, "artifact")
			calls := 0
			err := writeArtifact(files, destination, func(_ *os.File, _, _ string) error {
				calls++
				if calls == 1 && test.finalErr == nil {
					return syscall.EPERM
				}
				if calls == 1 {
					return syscall.EEXIST
				}
				return test.finalErr
			})
			if !errors.Is(err, ErrInternalFailure) {
				t.Fatalf("unexpected rename error = %v", err)
			}
			assertArtifactPathsAbsent(t, parent, destination)
		})
	}
}

func TestAtomicNoReplaceProbeClassification(t *testing.T) {
	wantNil := atomicNoReplaceProbeFailed
	if runtime.GOOS == "darwin" {
		wantNil = atomicNoReplaceProbeSupported
	}
	if got := classifyAtomicNoReplaceProbe(nil); got != wantNil {
		t.Fatalf("nil probe = %d, want %d", got, wantNil)
	}
	if got := classifyAtomicNoReplaceProbe(syscall.EEXIST); got != atomicNoReplaceProbeSupported {
		t.Fatalf("EEXIST probe = %d", got)
	}
	for _, err := range []error{syscall.EINVAL, syscall.ENOTSUP, syscall.EOPNOTSUPP} {
		if got := classifyAtomicNoReplaceProbe(err); got != atomicNoReplaceProbeUnsupported {
			t.Fatalf("unsupported probe %v = %d", err, got)
		}
	}
	if got := classifyAtomicNoReplaceProbe(syscall.EPERM); got != atomicNoReplaceProbeFailed {
		t.Fatalf("unexpected probe = %d", got)
	}
}

func assertArtifactPathsAbsent(t *testing.T, parent, destination string) {
	t.Helper()
	for _, path := range []string{filepath.Join(parent, stagingLeaf), destination} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("path was not cleaned %q: %v", filepath.Base(path), err)
		}
	}
}
