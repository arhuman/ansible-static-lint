// Package safeio reads files under an explicit ceiling, for the paths where
// what is being read was chosen by the linted repository rather than by the
// operator.
//
// astl runs in CI over repositories it does not trust (see SECURITY.md), and a
// repository can point astl at a file whose size is not what its directory
// entry says. git stores symlinks natively, so a checkout can contain
// `playbook.yml -> /dev/zero`: the entry lstats as a few bytes and the read
// never ends. Every read of a repository-chosen path goes through this package
// so that neither outcome is reachable.
package safeio

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
)

// errNotRegular is the refusal for anything that is not a regular file: a
// device, a socket, a directory, or a symlink resolving to one of those.
var errNotRegular = errors.New("not a regular file")

const (
	// MaxLintableBytes bounds one linted source file.
	//
	// It is far above any real Ansible file and is not a tuning knob: it exists
	// so the read terminates, not to express a policy about file sizes. A file
	// over the limit is reported as unreadable, which makes the run exit 3
	// rather than pass, so the divergence from ansible-lint (which has no limit)
	// is on the side that stays visible.
	MaxLintableBytes = 64 << 20

	// MaxConfigBytes bounds one configuration file: `.ansible-lint`, and each
	// link of a `.yamllint` extends chain. Tighter than a lintable because a
	// configuration file is hand-written and small, and because the extends
	// chain reads a path the configuration names, which is the widest choice a
	// repository gets over what astl opens.
	MaxConfigBytes = 4 << 20
)

// ReadFile reads path, refusing anything that is not a regular file and
// anything longer than limit bytes.
//
// The two refusals are one requirement: a bounded read. A character device
// reports no size and yields bytes forever, so the size check alone would not
// stop it, and a regular file large enough to exhaust memory is not a device,
// so the mode check alone would not stop that.
//
// Every failure is an *fs.PathError, matching the os.ReadFile this replaced.
// The CLI relies on it: a file that could not be read is reported differently
// from one that was read and did not parse, and only the second is a statement
// about the file's YAML.
//
// Errors name the path and never the content, because they reach the operator's
// log while the content is attacker-chosen.
//
// One case is out of reach here: opening a FIFO blocks until a writer appears,
// which happens before there is a descriptor to inspect. git cannot store a
// FIFO, so reaching one needs a symlink to a FIFO that already exists on the
// host at a path the attacker can predict.
func ReadFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &fs.PathError{Op: "read", Path: path, Err: errNotRegular}
	}

	// limit+1 so that a file of exactly limit bytes reads whole and one byte
	// over is distinguishable from one that just fits. Bounding the read as well
	// as the verdict is the point: reading the file and then declaring it too
	// big would allocate exactly what the limit exists to refuse.
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, &fs.PathError{
			Op:   "read",
			Path: path,
			Err:  fmt.Errorf("larger than the %d byte limit astl reads", limit),
		}
	}
	return data, nil
}
