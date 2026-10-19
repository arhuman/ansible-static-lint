//go:build unix

package safeio

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Opening a FIFO for reading blocks until a writer appears, before there is a
// descriptor to inspect, so the refusal has to come before the open. git cannot
// store a FIFO, but a symlink or a configured path can still name one.
func TestReadFileRefusesAFIFOWithoutBlocking(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := ReadFile(fifo, MaxConfigBytes)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Errorf("err = %v, want the mode refusal", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ReadFile did not return: opening the FIFO blocked")
	}
}
