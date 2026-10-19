//go:build unix

package yamllint

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// mkfifo returns the path of a new FIFO nobody will ever write to, so opening
// it for reading blocks forever.
func mkfifo(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// within fails the test when f does not return in time: before the fix, a
// config naming a FIFO hung the run.
func within(t *testing.T, f func() string) string {
	t.Helper()
	done := make(chan string, 1)
	go func() { done <- f() }()
	select {
	case got := <-done:
		return got
	case <-time.After(10 * time.Second):
		t.Fatal("Load did not return: a FIFO blocked the open")
		return ""
	}
}

func TestExtendsFIFOIsRefusedWithoutBlocking(t *testing.T) {
	want := extendsFailure(t, filepath.Join(t.TempDir(), "absent.yml"))
	fifo := mkfifo(t)
	if got := within(t, func() string { return extendsFailure(t, fifo) }); got != want {
		t.Errorf("error %q, want the same wording as a missing file: %q", got, want)
	}
}

func TestIgnoreFromFileFIFOIsRefusedWithoutBlocking(t *testing.T) {
	want := ignoreFromFileWarning(t, filepath.Join(t.TempDir(), "absent"))
	fifo := mkfifo(t)
	if got := within(t, func() string { return ignoreFromFileWarning(t, fifo) }); got != want {
		t.Errorf("warning %q, want the same wording as a missing file: %q", got, want)
	}
}
