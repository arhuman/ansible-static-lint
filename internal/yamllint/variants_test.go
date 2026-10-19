package yamllint

import (
	"fmt"
	"strings"
	"testing"
)

// The cases below cover the rule variants ansible-lint's own policy never
// reaches (document-start, comments-indentation, forbidden flow collections,
// strict anchors, YAML 1.2 truthy); each expectation was cross-checked
// against the reference yamllint during a differential matrix run.
func TestConfiguredRuleVariants(t *testing.T) {
	base := func(mutate func(c *Config)) *Config {
		c := AnsibleLintDefaults()
		mutate(c)
		return c.fillDefaults()
	}
	cases := []struct {
		name  string
		cfg   *Config
		input string
		want  []string // "line:col rule desc"
	}{
		{
			name:  "document-start missing",
			cfg:   base(func(c *Config) { c.rules["document-start"].enabled = true }),
			input: "key: value\n",
			want:  []string{`1:1 document-start missing document start "---"`},
		},
		{
			name: "document-start forbidden",
			cfg: base(func(c *Config) {
				c.rules["document-start"].enabled = true
				c.rules["document-start"].opts["present"] = false
			}),
			input: "---\nkey: value\n",
			want:  []string{`1:1 document-start found forbidden document start "---"`},
		},
		{
			name:  "comments-indentation",
			cfg:   base(func(c *Config) { c.rules["comments-indentation"].enabled = true }),
			input: "key:\n  a: 1\n    # over-indented comment\n  b: 2\n",
			want:  []string{"3:5 comments-indentation comment not indented like content"},
		},
		{
			name:  "braces forbidden outright",
			cfg:   base(func(c *Config) { c.rules["braces"].opts["forbid"] = true }),
			input: "---\nkey: {}\n",
			want:  []string{"2:7 braces forbidden flow mapping"},
		},
		{
			name:  "brackets forbidden when non-empty",
			cfg:   base(func(c *Config) { c.rules["brackets"].opts["forbid"] = "non-empty" }),
			input: "---\nempty: []\nfull: [1]\n",
			want:  []string{"3:8 brackets forbidden flow sequence"},
		},
		{
			name: "anchors strict",
			cfg: base(func(c *Config) {
				c.rules["anchors"].opts["forbid-duplicated-anchors"] = true
				c.rules["anchors"].opts["forbid-unused-anchors"] = true
			}),
			input: "---\na: &x 1\nb: &x 2\nc: &y 3\nd: *x\n",
			want: []string{
				`3:4 anchors found duplicated anchor "x"`,
				`4:4 anchors found unused anchor "y"`,
			},
		},
		{
			name:  "truthy under YAML 1.2",
			cfg:   base(func(*Config) {}),
			input: "%YAML 1.2\n---\na: yes\nb: True\n",
			want:  []string{"4:4 truthy truthy value should be one of [false, true]"},
		},
		{
			name: "line-length non-breakable inline mappings",
			cfg: base(func(c *Config) {
				c.rules["line-length"].opts["max"] = 20
				c.rules["line-length"].opts["allow-non-breakable-inline-mappings"] = true
			}),
			input: "---\nlong_line: http://localhost/very/very/long/url\nlong line: http://localhost/very/very/long/url\nshort_url: http://localhost/short/url + word\nlong: [http://localhost/very/very/long/url]\n",
			want: []string{
				"4:21 line-length line too long (44 > 20 characters)",
				"5:21 line-length line too long (43 > 20 characters)",
			},
		},
		{
			name: "line-length non-breakable inline mapping in a sequence",
			cfg: base(func(c *Config) {
				c.rules["line-length"].opts["max"] = 20
				c.rules["line-length"].opts["allow-non-breakable-inline-mappings"] = true
			}),
			input: "---\n- long line: http://localhost/very/very/long/url\n",
		},
		{
			name: "line-length inline mappings not allowed",
			cfg: base(func(c *Config) {
				c.rules["line-length"].opts["max"] = 20
				c.rules["line-length"].opts["allow-non-breakable-inline-mappings"] = false
			}),
			input: "---\nlong_line: http://localhost/very/very/long/url\n",
			want:  []string{"2:21 line-length line too long (46 > 20 characters)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, p := range Lint(tc.input, tc.cfg) {
				got = append(got, fmt.Sprintf("%d:%d %s %s", p.Line, p.Column, p.Rule, p.Desc))
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("got %v\nwant %v", got, tc.want)
			}
		})
	}
}

// Two files linted back to back through the pooled linter must behave as two
// fresh runs: per-file rule state (anchors, indent style, truthy directives)
// resets in between.
func TestPooledLinterResets(t *testing.T) {
	cfg := AnsibleLintDefaults()
	first := "%YAML 1.2\n---\na: &x 1\nb: *x\nc: yes\n"
	second := "---\na: *x\nb: yes\nind:\n    deep: 1\n"

	fresh := func() []Problem { return Lint(second, cfg) }
	want := fmt.Sprint(fresh())
	_ = Lint(first, cfg) // pollute: 1.2 truthy set, anchor x declared
	if got := fmt.Sprint(Lint(second, cfg)); got != want {
		t.Errorf("pooled rerun diverges from a fresh run\ngot:  %s\nwant: %s", got, want)
	}
}
