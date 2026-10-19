// Command astl is a fast static linter for Ansible content.
package main

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/arhuman/ansible-static-lint/internal/config"
	"github.com/arhuman/ansible-static-lint/internal/discover"
	"github.com/arhuman/ansible-static-lint/internal/format"
	"github.com/arhuman/ansible-static-lint/internal/ignore"
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
// collector run continuously. No single constant avoids that, because the live
// heap depends on how big the files being read are, which is knowable only after
// discovery. So the ceiling is derived from the input, and floored at
// minMemoryLimit for ordinary repositories.
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
	// Average, not maximum: one outsized file is throttled by bytesInFlight and
	// never sets the steady-state heap.
	avg := total / int64(len(items))
	inFlight := min(int64(bytesInFlight), int64(workers)*avg)
	return max(minMemoryLimit, inFlight*memoryAmplification)
}

// diag writes a diagnostic. Its write error is dropped because a failing
// stderr leaves nowhere to report it, and the exit code still carries the run.
//
// Every argument passes through format.Sanitize, because paths and config
// values are chosen by the linted repository and a newline in one forges a line
// of the CI log. The format string is astl's own and keeps its line breaks.
func diag(w io.Writer, format string, a ...any) {
	for i, v := range a {
		a[i] = sanitized{v}
	}
	_, _ = fmt.Fprintf(w, format, a...)
}

// sanitized renders its value as the verb asks, then drops the control
// characters format.Sanitize removes.
type sanitized struct{ v any }

func (s sanitized) Format(f fmt.State, verb rune) {
	_, _ = io.WriteString(f, format.Sanitize(fmt.Sprintf(fmt.FormatString(f, verb), s.v)))
}

// cliOptions is the parsed command line, with each long flag already folded
// into its short form.
type cliOptions struct {
	format, ids, config, ignore string
	version                     bool
	paths                       []string
}

// errUsage reports a command line that parsed but cannot run; the usage text
// has already been written.
var errUsage = errors.New("usage")

// parseArgs parses the command line, writing flag errors and usage to stderr.
// A request for the version needs no paths, so it is exempt from that check.
func parseArgs(args []string, stderr io.Writer) (cliOptions, error) {
	fs := flag.NewFlagSet("astl", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		formatName  = fs.String("f", "pep8", "output format: pep8 or sarif")
		formatLong  = fs.String("format", "", "output format: pep8 or sarif")
		idsName     = fs.String("ids", "upstream", "rule identifier taxonomy: upstream or native")
		configPath  = fs.String("c", "", "configuration file to use instead of searching for one")
		configLong  = fs.String("config", "", "configuration file to use instead of searching for one")
		ignorePath  = fs.String("i", "", "ignore file to use instead of searching for one")
		ignoreLong  = fs.String("ignore-file", "", "ignore file to use instead of searching for one")
		showVersion = fs.Bool("version", false, "print version and exit")
	)
	fs.Usage = func() {
		diag(stderr, "usage: astl [options] <paths...>\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return cliOptions{}, err
	}
	opts := cliOptions{
		format:  cmp.Or(*formatLong, *formatName),
		ids:     *idsName,
		config:  cmp.Or(*configLong, *configPath),
		ignore:  cmp.Or(*ignoreLong, *ignorePath),
		version: *showVersion,
		paths:   fs.Args(),
	}
	if !opts.version && len(opts.paths) == 0 {
		fs.Usage()
		return cliOptions{}, errUsage
	}
	return opts, nil
}

// run is the whole CLI; main only supplies the process streams and exits.
func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args, stderr)
	if err != nil {
		return exitError
	}
	if opts.version {
		if _, err := fmt.Fprintf(stdout, "astl %s\n", build); err != nil {
			return exitError
		}
		return exitClean
	}

	cfg, err := loadConfig(opts.config)
	if err != nil {
		diag(stderr, "astl: %v\n", err)
		return exitError
	}

	ignorePath := cmp.Or(opts.ignore, cfg.IgnoreFile)
	ignores, err := loadIgnores(ignorePath, stderr)
	if err != nil {
		diag(stderr, "astl: %v\n", err)
		return exitError
	}

	// Built here, not inside lint, so the SARIF report declares exactly the
	// selection the run applied rather than a second derivation that could drift.
	sel := rules.Selection{
		Profile:    cfg.Profile,
		EnableList: cfg.EnableList,
		SkipList:   cfg.SkipList,
		WarnList:   cfg.WarnList,
	}
	findings, unchecked, err := lint(opts.paths, projectRoot(opts.paths, opts.config), cfg, sel, stderr)
	if err != nil {
		diag(stderr, "astl: %v\n", err)
		return exitError
	}
	// After the run: the ignore file names findings, not files. Sorting and dedup
	// read neither flag it sets, so this equals upstream's mid-pipeline order.
	findings = ignores.Apply(findings)

	if err := emit(stdout, findings, opts.format, opts.ids, discover.WorkingDir(), sel); err != nil {
		diag(stderr, "astl: %v\n", err)
		return exitError
	}
	return exitStatus(findings, unchecked, stderr)
}

// exitStatus is the exit code for a run whose findings are already printed.
func exitStatus(findings []rules.Finding, unchecked int, stderr io.Writer) int {
	// Before exitViolations: only the exit code can say the run was incomplete,
	// and a caller gating on it must not miss that.
	if unchecked > 0 {
		diag(stderr, "astl: %d file(s) could not be checked\n", unchecked)
		return exitIncomplete
	}
	// Warnings print but do not fail, as upstream's `Passed: 0 failure(s), N
	// warning(s)`; counting them would defeat the warn_list that set them.
	if failures(findings) > 0 {
		return exitViolations
	}
	return exitClean
}

// projectRoot is the directory the run is confined to. Upstream searches for it
// after loading its configuration, with the file it loaded, so the same file
// marks the root here: the one -c names, or else the one the search found.
// Only when there is none do the config file names mark a root wherever they
// occur.
func projectRoot(paths []string, configFlag string) string {
	inUse := cmp.Or(configFlag, config.Find("."))
	if inUse == "" {
		return discover.ProjectRoot(paths, config.Filenames())
	}
	abs, err := filepath.Abs(inUse)
	if err != nil {
		return ""
	}
	return discover.ProjectRoot(paths, []string{abs})
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

// loadIgnores reads the `.ansible-lint-ignore` that applies, from the given
// path or by searching the working directory for one.
//
// A named file that is not there is reported and the run continues, which is
// the opposite of what `-c` does with a missing config. The asymmetry is
// upstream's, and it is defensible: a config that failed to load would lint the
// repository under the wrong policy without saying so, while a missing ignore
// file can only make the run report findings the repository already knew about,
// and those are on stdout for anyone to see.
func loadIgnores(path string, warn io.Writer) (ignore.Rules, error) {
	r, err := ignore.Load(".", path)
	if errors.Is(err, fs.ErrNotExist) {
		diag(warn, "astl: ignore file not found %q\n", path)
		return ignore.Rules{}, nil
	}
	return r, err
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
// workDir and sel describe the run rather than its findings, and only the SARIF
// document has anywhere to put them; pep8 is a line format and ignores both.
func emit(w io.Writer, findings []rules.Finding, formatName, idsName, workDir string, sel rules.Selection) error {
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
		err = format.SARIF(bw, findings, build.version, style, workDir, sel)
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
//
// root is the run's project root: a walked file or an include child resolving
// outside it is not linted, while a file named in paths always is.
//
// sel comes from the caller rather than from cfg here because the report needs
// it too, and deriving it twice would let the rules a run applies and the rules
// it declares drift apart.
func lint(paths []string, root string, cfg config.Config, sel rules.Selection, warn io.Writer) (found []rules.Finding, unchecked int, err error) {
	items, soft, err := discover.WalkWithin(root, paths, cfg.ExcludePaths)
	for _, e := range soft {
		diag(warn, "astl: %v\n", e)
	}
	unchecked += len(soft)
	if err != nil {
		return nil, 0, err
	}
	opt, err := ruleOptions(cfg, warn)
	if err != nil {
		return nil, 0, err
	}
	// Each round lints the files one more include away from the walk; the
	// workers parse each file once and report what it includes as they go.
	includes := discover.NewIncludes(root, items, cfg.ExcludePaths)
	var all []rules.Finding
	var sized []discover.Item
	for round := items; len(round) > 0; round = includes.Next() {
		// Sized from the input discovered so far, so it follows each round.
		sized = append(sized, round...)
		setMemoryLimit(sized)
		found, failed, err := lintRound(round, opt, includes, warn)
		if err != nil {
			return nil, 0, err
		}
		all = append(all, found...)
		unchecked += failed
	}

	all = rules.Select(all, sel)
	rules.Sort(all)
	return rules.Dedupe(all), unchecked, nil
}

// lintRound lints items in parallel, handing every parsed file to includes so
// the next round needs no second read. failed counts the files not checked.
func lintRound(items []discover.Item, opt rules.Options, includes *discover.Includes, warn io.Writer) (all []rules.Finding, failed int, err error) {
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(context.Background())
	g.SetLimit(runtime.NumCPU())
	// NumCPU bounds the work in flight, bytesInFlight the memory it holds;
	// without the second, peak memory is NumCPU times the largest file.
	sem := semaphore.NewWeighted(bytesInFlight)
	for _, item := range items {
		g.Go(func() error {
			// A file over the whole budget must still run: its weight is clamped
			// and it holds the budget alone, which is intended, not a deadlock.
			weight := max(min(item.Size, bytesInFlight), 1)
			if err := sem.Acquire(ctx, weight); err != nil {
				return err
			}
			defer sem.Release(weight)

			found, failure := lintItem(item, opt, includes)
			if failure != nil {
				mu.Lock()
				diag(warn, "astl: %v\n", failure)
				failed++
				mu.Unlock()
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
	return all, failed, nil
}

// ruleOptions loads the yamllint configuration and assembles the options every
// file is linted under, reporting configuration warnings on warn.
func ruleOptions(cfg config.Config, warn io.Writer) (rules.Options, error) {
	ylcfg, warnings, err := yamllint.Load(".")
	if err != nil {
		return rules.Options{}, err
	}
	for _, w := range warnings {
		diag(warn, "astl: %v\n", w)
	}
	// An unknown profile runs every rule, not none: a profile newer than astl's
	// table must not turn the linter into a silent pass.
	if cfg.Profile != "" && !rules.KnownProfile(cfg.Profile) {
		diag(warn, "astl: unknown profile %q, running every rule (known: %s)\n",
			cfg.Profile, strings.Join(rules.ProfileNames(), ", "))
	}

	return rules.Options{
		Yamllint:         ylcfg,
		EnableList:       cfg.EnableList,
		LoopVarPrefix:    cfg.LoopVarPrefix,
		MaxTasks:         cfg.MaxTasks,
		MaxBlockDepth:    cfg.MaxBlockDepth,
		VarNamingPattern: cfg.VarNamingPattern,
	}, nil
}

// lintItem checks one lintable. failure is non-nil when the file could not be
// checked, and is reported by the caller rather than here so it stays counted.
func lintItem(item discover.Item, opt rules.Options, includes *discover.Includes) (found []rules.Finding, failure error) {
	if item.Kind == parse.KindRole {
		return rules.RoleDir(item.Path, item.Abs), nil
	}
	f := parse.Load(item.Path, item.Abs, item.Kind)
	includes.Found(item, f)
	return rules.File(f, opt), loadFailure(f)
}

// loadFailure returns the reason f could not be checked, or nil when it could.
//
// ansible-lint reports unreadable and unparsable files through rules astl does
// not implement, so the report must stay off stdout to keep pep8 parity. It must
// still exist: a file astl did not check may not exit 0 as if found clean.
//
// Two parse failures are not load failures. A multi-document file is well-formed
// YAML that the yaml[*] pass lints as upstream's embedded yamllint does, and a
// file of a kind that was never YAML (templates, Jinja2, Python plugins, sanity
// ignore lists) is discovered on purpose and read as text where a rule needs it.
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
