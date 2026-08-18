//go:build !race

// Excluded from race builds on purpose. The race detector adds a large constant
// cost to every memory access, which inflates the linear part of this
// measurement far more than the quadratic part and squeezes the two outcomes
// together: the regression this guards against measured 8.2x without the
// detector and only 5.8x with it, close enough to the linear 4.0x that no
// threshold separates them safely. It would also be the slowest test in the
// suite there. `make test` runs with -race and skips it; `make audit` runs
// `make cover` without -race, so CI still executes it on every push.

package rules_test

import (
	"math"
	"testing"
	"time"

	"github.com/arhuman/ansible-static-lint/internal/discover"
	"github.com/arhuman/ansible-static-lint/internal/parse"
	"github.com/arhuman/ansible-static-lint/internal/rules"
)

// TestNoqaResolutionStaysLinear is the regression guard for a defect the rest
// of the suite cannot see: resolving `# noqa` suppressions once cost time
// proportional to tasks times suppressions, because every task rescanned the
// whole file's noqa map. Nothing failed. The output was correct, the unit tests
// passed, and the corpus speed guard never noticed because its files are small
// enough that the square of a small number is still small.
//
// It asserts a shape rather than a duration. Quadrupling the task count should
// quadruple the work; the threshold sits between the two measured outcomes,
// 3.9x when the lookup is indexed and 8.0x when it scans, so machine speed
// cancels out and only the exponent decides the result.
func TestNoqaResolutionStaysLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("timing guard: needs several hundred milliseconds to be meaningful")
	}

	const (
		small = 512
		large = 4 * small
		// Linear measured 3.90x and quadratic 8.02x. Half way between, in log
		// terms, leaves room for a loaded runner without letting a real
		// regression through.
		maxRatio = 6.0
		runs     = 3
	)

	fastest := func(n int) time.Duration {
		rel, abs := writeFixture(t, "site-playbook.yml", playbookOfTasks(n, true))
		kind := discover.KindOf(rel)
		// One untimed pass first: the first read of a file pays for page faults
		// and lazily built package state that the measurement is not about.
		rules.File(parse.Load(rel, abs, kind), rules.Options{})

		best := time.Duration(math.MaxInt64)
		for range runs {
			start := time.Now()
			rules.File(parse.Load(rel, abs, kind), rules.Options{})
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}

	// Largest first: if the machine is going to throttle or the scheduler is
	// going to interfere, doing the expensive half first makes it show up as a
	// smaller ratio, which fails safe by not accusing the code.
	tLarge := fastest(large)
	tSmall := fastest(small)

	ratio := float64(tLarge) / float64(tSmall)
	t.Logf("%d tasks %v, %d tasks %v, ratio %.2fx (linear is ~4x)", small, tSmall, large, tLarge, ratio)
	if ratio > maxRatio {
		t.Errorf("noqa resolution grew %.2fx for 4x the tasks, over the %.1fx bound: "+
			"skip resolution looks superlinear in the number of suppressions again",
			ratio, maxRatio)
	}
}
