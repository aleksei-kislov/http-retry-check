//go:build darwin || linux

package localfs

import (
	"bytes"
	"errors"
	"path/filepath"
	"syscall"
	"testing"
)

func TestEvidenceAdmissionRejectsFIFOWithoutBlocking(t *testing.T) {
	fifo := filepath.Join(realTempDir(t), "source.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadEvidence(fifo, bytes.NewReader(nil)); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("FIFO source = %v", err)
	}
}
