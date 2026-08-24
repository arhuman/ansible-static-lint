package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".ansible-lint"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// load writes content as the config in a fresh directory and reads it back.
func load(t *testing.T, content string) Config {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, content)
	c, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.SkipList) != 0 || len(c.ExcludePaths) != 0 {
		t.Fatalf("expected empty config, got %+v", c)
	}
}

func TestLoadReadsSkipAndExclude(t *testing.T) {
	c := load(t, "skip_list:\n  - name[casing]\n  - no-changed-when\nexclude_paths:\n  - vendor/\n")
	if len(c.SkipList) != 2 || c.SkipList[0] != "name[casing]" {
		t.Fatalf("skip_list = %v", c.SkipList)
	}
	if len(c.ExcludePaths) != 1 || c.ExcludePaths[0] != "vendor/" {
		t.Fatalf("exclude_paths = %v", c.ExcludePaths)
	}
}

// everySetting names every supported key once, with a value distinct from that
// field's zero value. It is the fixture both tests below read, and the
// spellings are ansible-lint's, since the point of matching them is that one
// file drives both linters.
const everySetting = `
skip_list:
  - no-changed-when
enable_list:
  - no-log-password
exclude_paths:
  - vendor/
warn_list:
  - no-handler
profile: production
loop_var_prefix: "^(__|{role}_)"
max_tasks: 42
max_block_depth: 7
var_naming_pattern: "^[a-z][a-z0-9_]*$"
ignore_file: .config/ansible-lint-ignore.txt
`

func TestLoadReadsEverySetting(t *testing.T) {
	c := load(t, everySetting)

	if len(c.EnableList) != 1 || c.EnableList[0] != "no-log-password" {
		t.Errorf("enable_list = %v", c.EnableList)
	}
	if c.LoopVarPrefix != "^(__|{role}_)" {
		t.Errorf("loop_var_prefix = %q", c.LoopVarPrefix)
	}
	if c.MaxTasks != 42 {
		t.Errorf("max_tasks = %d", c.MaxTasks)
	}
	if c.MaxBlockDepth != 7 {
		t.Errorf("max_block_depth = %d", c.MaxBlockDepth)
	}
	if c.VarNamingPattern != "^[a-z][a-z0-9_]*$" {
		t.Errorf("var_naming_pattern = %q", c.VarNamingPattern)
	}
	if len(c.WarnList) != 1 || c.WarnList[0] != "no-handler" {
		t.Errorf("warn_list = %v", c.WarnList)
	}
	if c.Profile != "production" {
		t.Errorf("profile = %q", c.Profile)
	}
	if c.IgnoreFile != ".config/ansible-lint-ignore.txt" {
		t.Errorf("ignore_file = %q", c.IgnoreFile)
	}
}

// TestEverySettingIsWiredToItsField is the one that catches the failure this
// package is most exposed to. A wrong or missing `yaml` tag does not fail: the
// file parses, Load returns no error, and the operator's setting is silently
// dropped, so the only visible symptom is astl quietly not honouring a
// configuration it accepted.
//
// Reading the fields reflectively rather than naming them means a field added
// to Config later is covered the day it is added: it will be zero after loading
// a fixture that does not mention it, and this fails until everySetting does.
func TestEverySettingIsWiredToItsField(t *testing.T) {
	c := load(t, everySetting)

	v := reflect.ValueOf(c)
	for i := range v.NumField() {
		field := v.Type().Field(i)
		tag, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if tag == "" {
			t.Errorf("%s has no yaml tag, so no config file can reach it", field.Name)
			continue
		}
		if !strings.Contains(everySetting, "\n"+tag+":") {
			t.Errorf("%s is tagged %q, which everySetting does not exercise; add it there",
				field.Name, tag)
			continue
		}
		if v.Field(i).IsZero() {
			t.Errorf("%s stayed zero after loading a config that sets %q: "+
				"the tag does not match the key the file uses", field.Name, tag)
		}
	}
}

// TestUnsupportedSettingsAreIgnored pins a deliberate choice. astl reads a
// subset of ansible-lint's settings out of a file ansible-lint also reads, so
// rejecting keys it does not implement would reject nearly every real
// `.ansible-lint`. The cost is that a misspelled supported key is silent too,
// which is the tradeoff upstream compatibility forces here.
func TestUnsupportedSettingsAreIgnored(t *testing.T) {
	c := load(t, "offline: true\nuse_default_rules: true\nmock_modules:\n  - fake.mod\nskip_list:\n  - no-changed-when\n")
	if len(c.SkipList) != 1 {
		t.Fatalf("skip_list = %v, want the supported key to survive beside unsupported ones", c.SkipList)
	}
}

func TestLoadRejectsMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "skip_list: [unterminated\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("a config that is not YAML must be an error, not an empty config")
	}
}

func TestLoadRejectsAWrongTypedSetting(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "max_tasks: many\n")
	if _, err := Load(dir); err == nil {
		t.Fatal("max_tasks that is not a number must be an error")
	}
}

// TestLoadRejectsAnInvalidVarNamingPattern covers the one setting Load
// validates beyond parsing. The pattern is compiled by the var-naming rule on
// every candidate name, so an invalid one has to fail here, where the operator
// is told which setting is wrong, rather than at the point of use.
func TestLoadRejectsAnInvalidVarNamingPattern(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "var_naming_pattern: \"^[a-z\"\n")
	_, err := Load(dir)
	if err == nil {
		t.Fatal("an invalid regexp must be rejected")
	}
	if !strings.Contains(err.Error(), "var_naming_pattern") {
		t.Errorf("error %q does not name the setting at fault", err)
	}
}

// TestLoadAcceptsAValidVarNamingPattern is the other half: validation must not
// reject a pattern the rule would accept.
func TestLoadAcceptsAValidVarNamingPattern(t *testing.T) {
	c := load(t, "var_naming_pattern: \"^my_[a-z0-9_]+$\"\n")
	if c.VarNamingPattern != "^my_[a-z0-9_]+$" {
		t.Fatalf("var_naming_pattern = %q", c.VarNamingPattern)
	}
}

// TestLoadFindsEveryUpstreamFilename is the regression for issue 0007. astl
// read only `.ansible-lint`, so a repository keeping its policy under any of
// the other four names upstream accepts was linted with defaults: on
// dell/omnia, whose config is `.config/ansible-lint.yml`, that was 607 false
// positives its `skip_list` removes.
func TestLoadFindsEveryUpstreamFilename(t *testing.T) {
	for _, name := range Filenames {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("skip_list:\n  - var-naming\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			c, err := Load(dir)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if len(c.SkipList) != 1 || c.SkipList[0] != "var-naming" {
				t.Fatalf("%s was not read: skip_list = %v", name, c.SkipList)
			}
		})
	}
}

// TestLoadPrefersTheFirstFilename pins the order rather than only the set.
// Upstream takes the first name that exists and does not merge the rest, so a
// repository holding two of them must get exactly one policy, and the same one
// both tools pick.
func TestLoadPrefersTheFirstFilename(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "skip_list:\n  - first\n")
	if err := os.WriteFile(filepath.Join(dir, ".config", "ansible-lint.yml"),
		[]byte("skip_list:\n  - second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.SkipList) != 1 || c.SkipList[0] != "first" {
		t.Fatalf("skip_list = %v, want only the first filename in Filenames to apply", c.SkipList)
	}
}

// TestLoadFileReportsAMissingFile is the difference between the two entry
// points: Load treats "not there" as "try the next name", LoadFile as an error,
// because `-c` names the policy the operator wants applied.
func TestLoadFileReportsAMissingFile(t *testing.T) {
	_, err := LoadFile(filepath.Join(t.TempDir(), "nowhere.yml"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

// TestLoadReportsAnUnreadableFile covers the read error that is not a missing
// file. A directory named `.ansible-lint` is the portable way to produce one:
// unlike a chmod, it behaves the same whether or not the tests run as root.
func TestLoadReportsAnUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".ansible-lint"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("a config that cannot be read must be an error, not an empty config")
	}
}
