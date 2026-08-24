package parse

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/safeio"
)

// File is a loaded lintable YAML file.
type File struct {
	// Path is the path used in output.
	Path string
	// Abs is the absolute path on disk.
	Abs string
	// Kind is the ansible-lint kind (playbook, tasks, meta, ...).
	Kind string
	// Role is the name of the role directory the file belongs to, or "".
	Role string
	// Text is the raw file content. Rules that read a non-YAML format, such as
	// a sanity ignore list, work from it directly.
	Text string
	// Root is the first document's content node, or nil when the file is empty
	// or could not be parsed.
	Root *yaml.Node
	// Err records a load or parse failure. Rules skip such files.
	Err error
	// Noqa maps a 1-based line to the rule ids or tags skipped there.
	Noqa map[int]map[string]bool

	// tasks and plays memoize Tasks() and Plays(): half a dozen rule passes
	// walk the same task list, and rebuilding it dominates the allocation
	// profile without this.
	tasks     []*Task
	tasksDone bool
	plays     []*yaml.Node
	playsDone bool

	// noqaLines memoizes the sorted keys of Noqa. SkipsInRange is asked one
	// range per task, so scanning the whole map each time made skip resolution
	// cost O(tasks x noqa lines); sorted keys make it a binary search.
	noqaLines     []int
	noqaLinesDone bool
}

// Load reads and parses a YAML file. Parse errors are recorded on the returned
// File rather than returned, because ansible-lint reports unparsable files
// through a separate rule that is out of scope here.
func Load(path, abs, kind string) *File {
	f := &File{Path: path, Abs: abs, Kind: kind}
	data, err := safeio.ReadFile(abs, safeio.MaxLintableBytes)
	if err != nil {
		f.Err = err
		return f
	}
	f.Text = string(data)
	f.Role = roleName(abs)
	f.Noqa = parseNoqa(f.Text)
	docs, err := decodeDocuments(data)
	if err != nil {
		f.Err = err
		return f
	}
	// Ansible cannot load multi-document YAML; such files fail before any rule
	// gets to look at them.
	if len(docs) > 1 {
		f.Err = errMultiDocument
		return f
	}
	if len(docs) == 1 && len(docs[0].Content) > 0 {
		f.Root = docs[0].Content[0]
	}
	// A yaml kind that parses as a list of plays is really a playbook.
	if f.Kind == "yaml" && looksLikePlaybook(f.Root) {
		f.Kind = "playbook"
	}
	return f
}

// errMultiDocument marks files holding more than one YAML document.
var errMultiDocument = errors.New("parse: multi-document YAML is not supported by ansible")

// IsMultiDocument reports whether a File's Err is the multi-document marker.
// Ansible cannot load such files, but they are still well-formed YAML, so the
// yaml[*] pass lints them like upstream's embedded yamllint does.
func IsMultiDocument(err error) bool {
	return errors.Is(err, errMultiDocument)
}

func decodeDocuments(data []byte) ([]yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []yaml.Node
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
}

// roleSubdirs are the subdirectories that make a directory a role root.
var roleSubdirs = []string{"tasks", "meta", "vars", "defaults", "handlers"}

// roleName returns the name of the role a file belongs to, mirroring
// ansible-lint's find_role_dir: the outermost ancestor that sits under a
// `roles/` directory, is not the `roles/` directory itself, and carries at
// least one role subdirectory.
func roleName(abs string) string {
	dir := filepath.Dir(abs)
	found := ""
	for {
		base := filepath.Base(dir)
		if base != "roles" && underRoles(dir) && hasRoleSubdir(dir) {
			found = base
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return found
		}
		dir = parent
	}
}

func underRoles(dir string) bool {
	for _, part := range strings.Split(filepath.ToSlash(dir), "/") {
		if part == "roles" {
			return true
		}
	}
	return false
}

func hasRoleSubdir(dir string) bool {
	for _, sub := range roleSubdirs {
		if fi, err := os.Stat(filepath.Join(dir, sub)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

func looksLikePlaybook(root *yaml.Node) bool {
	if !IsSeq(root) || len(root.Content) == 0 {
		return false
	}
	first := root.Content[0]
	if !IsMap(first) || MapHas(first, "rules") {
		return false
	}
	// Mirrors upstream's _guess_kind: a first play holding only an
	// import_playbook is still a playbook (astl issue 0010).
	return MapHas(first, "hosts") || MapHas(first, "import_playbook") ||
		MapHas(first, "ansible.builtin.import_playbook")
}
