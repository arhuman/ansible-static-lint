package safeio

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestReadFileSizes(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		size int
		max  int64
		ok   bool
	}{
		{"empty", 0, 8, true},
		{"under", 7, 8, true},
		// The boundary is the reason ReadFile reads max+1 bytes: a file of
		// exactly max must not be mistaken for one that overflows.
		{"exact", 8, 8, true},
		{"over-by-one", 9, 8, false},
		{"zero-limit", 1, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := bytes.Repeat([]byte("a"), tc.size)
			p := write(t, dir, tc.name, want)
			got, err := ReadFile(p, tc.max)
			if !tc.ok {
				if err == nil {
					t.Fatalf("ReadFile(%d bytes, max %d) = nil error, want a refusal", tc.size, tc.max)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadFile(%d bytes, max %d): %v", tc.size, tc.max, err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("read %d bytes, want %d", len(got), len(want))
			}
		})
	}
}

// TestReadFileMissingStaysErrNotExist pins the one error identity callers
// depend on: config.Load treats a missing `.ansible-lint` as an empty config
// rather than a failure, so wrapping the open must not break errors.Is.
func TestReadFileMissingStaysErrNotExist(t *testing.T) {
	_, err := ReadFile(filepath.Join(t.TempDir(), "absent"), MaxConfigBytes)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want one satisfying fs.ErrNotExist", err)
	}
}

// TestReadFileRefusalsArePathErrors pins the contract cmd/astl reads: a refusal
// here means the file was never read, which is a different report from a file
// that was read and did not parse. Returning a plain error would send both down
// the branch that tells the operator their file is not valid YAML, which for a
// character device or an oversized file is not true.
func TestReadFileRefusalsArePathErrors(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]func() error{
		"too large": func() error {
			_, err := ReadFile(write(t, dir, "big.yml", make([]byte, 32)), 8)
			return err
		},
		"not regular": func() error {
			_, err := ReadFile(dir, MaxConfigBytes)
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			var pathErr *fs.PathError
			if !errors.As(err, &pathErr) {
				t.Fatalf("err = %v (%T), want an *fs.PathError", err, err)
			}
		})
	}
}

func TestReadFileRejectsDirectory(t *testing.T) {
	if _, err := ReadFile(t.TempDir(), MaxConfigBytes); err == nil {
		t.Fatal("reading a directory succeeded, want a refusal")
	}
}

// TestReadFileRejectsCharacterDevice is the regression test for the defect this
// package exists to close.
//
// git stores symlinks natively, so a repository can ship `playbook.yml ->
// /dev/zero`. The directory entry lstats as a handful of bytes, which is what
// sized the read budget, while the read itself follows the link and never ends.
// A size ceiling alone does not fix it: the read would still have to produce
// the whole ceiling before refusing. The mode check is what makes it cheap.
//
// The deadline is the assertion. Before the fix this call did not return.
func TestReadFileRejectsCharacterDevice(t *testing.T) {
	const dev = "/dev/zero"
	if fi, err := os.Stat(dev); err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		t.Skipf("%s is not a character device on this platform", dev)
	}

	link := filepath.Join(t.TempDir(), "playbook.yml")
	if err := os.Symlink(dev, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := ReadFile(link, MaxLintableBytes)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("reading a symlink to a character device succeeded, want a refusal")
		}
		if !strings.Contains(err.Error(), "not a regular file") {
			t.Errorf("err = %v, want it to name the mode refusal", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ReadFile did not return: the read is still unbounded")
	}
}

// TestReadFileRefusesWithoutAllocating pins what the LimitReader is for, which
// the size tests above cannot see: on a file of finite size the length check
// alone produces the same refusal, so both bound the outcome and only one bounds
// the cost of reaching it. Reading the file whole and then declaring it too big
// is exactly the shape of the defect this package closes.
//
// The margin is three orders of magnitude, so this measures the difference
// between reading a kilobyte and reading the file, not allocator noise.
func TestReadFileRefusesWithoutAllocating(t *testing.T) {
	const (
		fileSize = 16 << 20
		limit    = 1 << 10
		headroom = 1 << 20
	)
	p := write(t, t.TempDir(), "big.yml", make([]byte, fileSize))

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, err := ReadFile(p, limit); err == nil {
		t.Fatal("oversized read succeeded, want a refusal")
	}
	runtime.ReadMemStats(&after)

	if used := after.TotalAlloc - before.TotalAlloc; used > headroom {
		t.Errorf("refusing a %d byte file allocated %d bytes, want under %d: "+
			"the read is not bounded, only its verdict is", fileSize, used, headroom)
	}
}

// TestReadFileErrorsNameThePathOnly guards the property SECURITY.md relies on:
// these errors reach the operator's log, and everything about the file was
// chosen by the repository being linted, so the path may appear and the content
// may not.
func TestReadFileErrorsNameThePathOnly(t *testing.T) {
	const secret = "ZZSHOULDNOTAPPEARZZ"
	p := write(t, t.TempDir(), "big.yml", []byte(strings.Repeat(secret, 4)))

	_, err := ReadFile(p, 8)
	if err == nil {
		t.Fatal("oversized read succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), p) {
		t.Errorf("err = %v, want it to name %s", err, p)
	}
	if strings.Contains(err.Error(), secret) {
		t.Error("error quoted the file's content")
	}
}
