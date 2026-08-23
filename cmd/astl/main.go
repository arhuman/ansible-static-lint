// Command astl is a fast static linter for Ansible content.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/arhuman/ansible-static-lint/internal/config"
	"github.com/arhuman/ansible-static-lint/internal/discover"
	"github.com/arhuman/ansible-static-lint/internal/format"
	"github.com/arhuman/ansible-static-lint/internal/parse"
	"github.com/arhuman/ansible-static-lint/internal/rules"
	"github.com/arhuman/ansible-static-lint/internal/yamllint"
)

// Exit codes: clean run, usage or runtime failure, violations found, and a run
// that could not check everything it was given. The last is separate from
// exitViolations because the two say different things: violations are a result,
// an unchecked file means there is no result for that file and the silence over
// it must not read as success.
const (
	exitClean      = 0
	exitError      = 1
	exitViolations = 2
	exitIncomplete = 3
)

// bytesInFlight caps the source bytes the workers may hold at once. A parsed
// document and its scanner buffers cost tens of times the bytes they came from
// (measured at roughly 78x), so this is the term that decides peak memory, and
// it is expressed in input bytes because that is the quantity astl can see
// before paying for it. Small files never reach the cap and NumCPU stays the
// binding limit; only large ones throttle, which is exactly where the memory
// went. Raising it trades peak memory for parallelism on big files.
const bytesInFlight = 4 << 20

func main() {
	tuneGC()
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// tuneGC trades heap headroom for wall time, the right exchange for a process
// that lints and exits: the default collector spends a measurable share of a
// run returning memory the process will never need again. The ceiling that
// bounds how far that headroom may grow is set later, once the input is known:
// see setMemoryLimit.
//
// The operator's own GOGC or GOMEMLIMIT always wins.
func tuneGC() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(800)
	}
}

const (
	// minMemoryLimit is the ceiling for any ordinary repository, where files
	// are small enough that concurrency never comes near it. It exists to cap
	// how far GOGC's headroom may run, not to constrain the work.
	minMemoryLimit = 128 << 20

	// memoryAmplification is what a parsed document and its scanner buffers
	// cost relative to the source bytes they came from, measured at roughly
	// 78x. It sizes a ceiling rather than an allocation, so being the right
	// order of magnitude is enough; re-measure by linting one large file with
	// GOMEMLIMIT unset and dividing peak RSS by the file's size.
	memoryAmplification = 80
)

// setMemoryLimit caps heap growth at a value the run can actually stay under.
//
// A soft limit below the live heap cannot shrink anything; it only makes the
// collector run continuously. A fixed 128 MiB did that on repositories of large
// files, costing four times the CPU while still peaking five times over its own
// ceiling, and no single constant avoids it: the live heap depends on how big
// the files being read are, which is knowable only after discovery. So the
// ceiling is derived from the input, and floored so that ordinary repositories
// keep exactly the behaviour they had.
func setMemoryLimit(items []discover.Item) {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}
	debug.SetMemoryLimit(memoryLimitFor(items, runtime.NumCPU()))
}

// memoryLimitFor computes that ceiling. Split from the call that applies it so
// the arithmetic can be tested without reconfiguring the collector under the
// test binary.
func memoryLimitFor(items []discover.Item, workers int) int64 {
	if len(items) == 0 {
		return minMemoryLimit
	}
	var total int64
	for _, it := range items {
		total += it.Size
	}
	// Average, not maximum: it estimates what is genuinely resident at once.
	// One outsized file among thousands of small ones is throttled by
	// bytesInFlight and never sets the steady-state heap.
	avg := total / int64(len(items))
	inFlight := min(int64(bytesInFlight), int64(workers)*avg)
	return max(minMemoryLimit, inFlight*memoryAmplification)
}

// run is the whole CLI; main only supplies the process streams and exits.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("astl", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		formatName  = fs.String("f", "pep8", "output format: pep8 or sarif")
		formatLong  = fs.String("format", "", "output format: pep8 or sarif")
		idsName     = fs.String("ids", "upstream", "rule identifier taxonomy: upstream or native")
		configPath  = fs.String("c", "", "configuration file to use instead of searching for one")
		configLong  = fs.String("config", "", "configuration file to use instead of searching for one")
		showVersion = fs.Bool("version", false, "print version and exit")
	)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: astl [options] <paths...>\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	if *showVersion {
		fmt.Fprintf(stdout, "astl %s\n", build)
		return exitClean
	}
	if *formatLong != "" {
		*formatName = *formatLong
	}
	if *configLong != "" {
		*configPath = *configLong
	}
	paths := fs.Args()
	if len(paths) == 0 {
		fs.Usage()
		return exitError
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, "astl:", err)
		return exitError
	}

	findings, unchecked, err := lint(paths, cfg, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "astl:", err)
		return exitError
	}

	if err := emit(stdout, findings, *formatName, *idsName); err != nil {
		fmt.Fprintln(stderr, "astl:", err)
		return exitError
	}
	// Ordered before exitViolations deliberately: findings are on stdout either
	// way, but only the exit code can say the run was incomplete, and that is
	// the part a caller gating on it must not miss.
	if unchecked > 0 {
		fmt.Fprintf(stderr, "astl: %d file(s) could not be checked\n", unchecked)
		return exitIncomplete
	}
	// Warning-level findings print but do not fail, matching upstream's
	// `Passed: 0 failure(s), N warning(s)`. A repository puts a rule in
	// warn_list precisely so it keeps seeing it without a red build, so
	// counting warnings here would defeat the key it set.
	if failures(findings) > 0 {
		return exitViolations
	}
	return exitClean
}

// loadConfig reads the named configuration file, or searches for one the way
// ansible-lint does when no name was given. A named file that does not exist is
// an error, an absent search is not: `-c` says which policy to apply, so
// falling back to defaults would silently lint under the wrong one.
func loadConfig(path string) (config.Config, error) {
	if path == "" {
		return config.Load(".")
	}
	return config.LoadFile(path)
}

// failures counts the findings that make a run fail, which is every finding
// that warn_list (or upstream's default `experimental` demotion) did not turn
// into a warning.
func failures(findings []rules.Finding) int {
	n := 0
	for _, f := range findings {
		if !f.Warning {
			n++
		}
	}
	return n
}

// emit renders findings to w in the requested format and rule-id taxonomy.
func emit(w io.Writer, findings []rules.Finding, formatName, idsName string) error {
	var style rules.IDStyle
	switch idsName {
	case string(rules.IDUpstream):
		style = rules.IDUpstream
	case string(rules.IDNative):
		style = rules.IDNative
	default:
		return fmt.Errorf("unknown ids taxonomy %q", idsName)
	}

	bw := bufio.NewWriter(w)
	var err error
	switch formatName {
	case "pep8":
		err = format.PEP8(bw, findings, style)
	case "sarif":
		err = format.SARIF(bw, findings, build.version, style)
	default:
		return fmt.Errorf("unknown format %q", formatName)
	}
	if err != nil {
		return err
	}
	return bw.Flush()
}

// lint discovers and checks every lintable under paths. Files that cannot be
// discovered, read or parsed are reported on warn and skipped, and counted into
// the returned unchecked total so the caller can refuse to call the run clean;
// only a failure that invalidates the whole run comes back as an error.
func lint(paths []string, cfg config.Config, warn io.Writer) (found []rules.Finding, unchecked int, err error) {
	items, soft, err := discover.Walk(paths, cfg.ExcludePaths)
	for _, e := range soft {
		fmt.Fprintln(warn, "astl:", e)
	}
	unchecked += len(soft)
	if err != nil {
		return nil, 0, err
	}
	// Files a playbook pulls in are lintables too, under the kind the including
	// section gives them, so this has to happen before anything is sized or
	// linted (issue 0008).
	items = append(items, discover.ExpandIncludes(items, cfg.ExcludePaths)...)

	// Sized from the discovered input, so it must come after the walk.
	setMemoryLimit(items)

	ylcfg, warnings, err := yamllint.Load(".")
	if err != nil {
		return nil, 0, err
	}
	for _, w := range warnings {
		fmt.Fprintln(warn, "astl:", w)
	}
	// An unknown profile runs every rule rather than none. Upstream may add a
	// profile astl's table predates, and muting the linter on a name it merely
	// does not recognise would turn a stale table into a silent pass.
	if cfg.Profile != "" && !rules.KnownProfile(cfg.Profile) {
		fmt.Fprintf(warn, "astl: unknown profile %q, running every rule (known: %s)\n",
			cfg.Profile, strings.Join(rules.ProfileNames(), ", "))
	}

	opt := rules.Options{
		Yamllint:         ylcfg,
		EnableList:       cfg.EnableList,
		LoopVarPrefix:    cfg.LoopVarPrefix,
		MaxTasks:         cfg.MaxTasks,
		MaxBlockDepth:    cfg.MaxBlockDepth,
		VarNamingPattern: cfg.VarNamingPattern,
	}
	var mu sync.Mutex
	var all []rules.Finding

	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(runtime.NumCPU())
	// Two limits, because they bound different things. NumCPU bounds how much
	// work runs at once; bytesInFlight bounds how much memory that work holds.
	// Without the second, peak memory is NumCPU times the largest file, which
	// is why a repository of a few dozen megabytes could cost gigabytes.
	sem := semaphore.NewWeighted(bytesInFlight)
	for _, item := range items {
		g.Go(func() error {
			// A file larger than the whole budget still has to run, so its
			// weight is clamped; it then holds the budget alone, which is the
			// intended outcome rather than a deadlock.
			weight := max(min(item.Size, bytesInFlight), 1)
			if err := sem.Acquire(ctx, weight); err != nil {
				return err
			}
			defer sem.Release(weight)

			var found []rules.Finding
			if item.Kind == discover.KindRole {
				found = rules.RoleDir(item.Path, item.Abs)
			} else {
				f := parse.Load(item.Path, item.Abs, item.Kind)
				found = rules.File(f, opt)
				if err := loadFailure(f); err != nil {
					mu.Lock()
					fmt.Fprintln(warn, "astl:", err)
					unchecked++
					mu.Unlock()
				}
			}
			if len(found) == 0 {
				return nil
			}
			mu.Lock()
			all = append(all, found...)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, 0, err
	}

	all = rules.Select(all, rules.Selection{
		Profile:    cfg.Profile,
		EnableList: cfg.EnableList,
		SkipList:   cfg.SkipList,
		WarnList:   cfg.WarnList,
	})
	rules.Sort(all)
	return rules.Dedupe(all), unchecked, nil
}

// loadFailure returns the reason f could not be checked, or nil when it could.
//
// ansible-lint reports both unreadable and unparsable files through rules astl
// does not implement, so neither may reach stdout without breaking pep8 output
// parity. That constrains where the report goes, not whether there is one: a
// file astl could not parse is a file astl did not check, and exiting 0 with
// nothing printed claims it was checked and found clean.
//
// A multi-document file is not a failure. Ansible cannot load one, but it is
// well-formed YAML and the yaml[*] pass lints it exactly as upstream's embedded
// yamllint does, so it is skipped by the ansible-shaped rules on purpose rather
// than left unexamined.
//
// Neither is a file of a kind that was never YAML. Templates, Jinja2 files,
// Python plugins and sanity ignore lists are all discovered on purpose, and
// astl reads the ones it has rules for as text; that they do not parse as YAML
// is the expected outcome and says nothing about whether they were checked.
func loadFailure(f *parse.File) error {
	if f.Err == nil || parse.IsMultiDocument(f.Err) {
		return nil
	}
	var pathErr *fs.PathError
	if errors.As(f.Err, &pathErr) {
		return fmt.Errorf("%s: not checked: %w", f.Path, f.Err)
	}
	if !discover.IsYAMLKind(f.Kind) {
		return nil
	}
	return fmt.Errorf("%s: not checked, it is not valid YAML: %w", f.Path, f.Err)
}
