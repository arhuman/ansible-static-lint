package main

import (
	"os"
	"testing"
)

// TestMain cuts the CLI tests off from the machine's own yamllint
// configuration, which run() would otherwise pick up through yamllint.Load and
// apply to every fixture. These tests assert exact pep8 output and exact exit
// codes, so an ambient config that disables a yaml rule turns them green for a
// reason that has nothing to do with the code. See the same function in
// internal/yamllint for why the second variable is set rather than unset.
func TestMain(m *testing.M) {
	os.Unsetenv("YAMLLINT_CONFIG_FILE")
	dir, err := os.MkdirTemp("", "astl-no-yamllint-config")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
