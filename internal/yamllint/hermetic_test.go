package yamllint

import (
	"os"
	"testing"
)

// TestMain cuts the tests off from the machine's own yamllint configuration.
//
// findConfig falls back to $YAMLLINT_CONFIG_FILE and then to
// ${XDG_CONFIG_HOME:-~/.config}/yamllint/config, which is correct in
// production: it is what ansible-lint does, and an operator who sets either
// means it. Under test it means the suite inherits whatever the developer or
// the runner happens to have, and the effect is not a louder failure but a
// quieter one. A file that reports yaml[trailing-spaces] and exits 2 in a clean
// environment reports nothing and exits 0 with an ambient config that disables
// the rule, so the assertion the test wanted to make silently stops being made.
//
// Both fallbacks have to be closed, and closing the second one is why
// XDG_CONFIG_HOME is set to an empty directory rather than unset: unsetting it
// selects the ~/.config branch instead of disabling it.
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
