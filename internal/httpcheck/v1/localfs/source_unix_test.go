//go:build darwin || linux

package localfs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestNonblockingSourceOpenDoesNotFollowSymlink(t *testing.T) {
	root := realTempDir(t)
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.WriteFile(target, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	targetBefore, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	file, err := openNonblocking(link)
	if err == nil {
		_ = file.Close()
		t.Fatal("nonblocking source open followed a symlink")
	}
	targetAfter, statErr := os.Lstat(target)
	contents, readErr := os.ReadFile(target)
	if statErr != nil || readErr != nil || !os.SameFile(targetBefore, targetAfter) || string(contents) != "preserve" {
		t.Fatalf("symlink target changed: identity=%v contents=%q stat=%v read=%v",
			statErr == nil && os.SameFile(targetBefore, targetAfter), contents, statErr, readErr)
	}
}

func TestEvidenceAdmissionRejectsFIFOWithoutBlocking(t *testing.T) {
	fifo := filepath.Join(realTempDir(t), "source.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvidence(fifo, bytes.NewReader(nil)); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("FIFO source = %v", err)
	}
}
