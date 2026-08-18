// Package config reads the subset of ansible-lint settings astl honours, from
// the same set of file names upstream looks for.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/safeio"
)

// Config holds the supported `.ansible-lint` settings. Keys are spelled as
// ansible-lint spells them, so one file drives both linters.
type Config struct {
	SkipList     []string `yaml:"skip_list"`
	EnableList   []string `yaml:"enable_list"`
	ExcludePaths []string `yaml:"exclude_paths"`
	// WarnList demotes rules to warning level: they still print, with pep8's
	// trailing ` (warning)`, but they do not make the run fail.
	WarnList []string `yaml:"warn_list"`
	// Profile selects ansible-lint's named rule set. It is not a severity
	// knob: rules outside the profile do not run at all.
	Profile string `yaml:"profile"`
	// LoopVarPrefix is the regexp a role's loop_var must match. Unset leaves
	// the loop-var-prefix rule inert, as upstream does.
	LoopVarPrefix string `yaml:"loop_var_prefix"`
	// MaxTasks and MaxBlockDepth bound the complexity rule. Zero means the
	// upstream default.
	MaxTasks      int `yaml:"max_tasks"`
	MaxBlockDepth int `yaml:"max_block_depth"`
	// VarNamingPattern replaces var-naming's default `^[a-z_][a-z0-9_]*$`,
	// like upstream's option of the same name. It is interpolated into the
	// var-naming[pattern] message.
	VarNamingPattern string `yaml:"var_naming_pattern"`
}

// Filenames are the config files ansible-lint looks for, in its own order
// (`CONFIG_FILENAMES` in upstream's constants.py). The first one that exists
// wins, and the rest are not merged into it.
//
// Reading only `.ansible-lint` was worth 607 false positives on dell/omnia,
// which keeps its policy at `.config/ansible-lint.yml`: none of its skip_list,
// profile or exclude_paths applied.
var Filenames = []string{
	".ansible-lint",
	".ansible-lint.yml",
	".ansible-lint.yaml",
	".config/ansible-lint.yml",
	".config/ansible-lint.yaml",
}

// Load reads the first of Filenames that exists under dir. No config file at
// all yields an empty config, which is not an error: a repository is free not
// to configure the linter.
func Load(dir string) (Config, error) {
	for _, name := range Filenames {
		c, err := LoadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		return c, err
	}
	return Config{}, nil
}

// LoadFile reads one config file by path, as `-c` does. A missing file is
// reported as fs.ErrNotExist rather than swallowed: an operator who named a
// file wants to hear that it is not there, while Load treats the same error as
// "try the next name".
func LoadFile(path string) (Config, error) {
	var c Config
	data, err := safeio.ReadFile(path, safeio.MaxConfigBytes)
	if err != nil {
		return c, err
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		return c, err
	}
	if c.VarNamingPattern != "" {
		if _, err := regexp.Compile(c.VarNamingPattern); err != nil {
			return c, fmt.Errorf("config: var_naming_pattern: %w", err)
		}
	}
	return c, nil
}
