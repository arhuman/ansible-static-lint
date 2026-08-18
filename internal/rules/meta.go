package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arhuman/ansible-static-lint/internal/parse"
	"github.com/arhuman/ansible-static-lint/internal/safeio"
)

var (
	reMetaTag     = regexp.MustCompile(`^[a-z0-9]+$`)
	reGalaxyTag   = regexp.MustCompile(`^(?:[a-z][0-9a-z_]*)$`)
	reDoubleUnder = regexp.MustCompile(`__`)
	reSpecifier   = regexp.MustCompile(`^\s*(===|==|!=|<=|>=|~=|<|>)\s*[A-Za-z0-9_.*+!-]+\s*$`)
	reRoleName    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	reVideoURL    = []*regexp.Regexp{
		regexp.MustCompile(`^https://drive\.google\.com.*file/d/([0-9A-Za-z-_]+)/.*`),
		regexp.MustCompile(`^https://vimeo\.com/([0-9]+)`),
		regexp.MustCompile(`^https://youtu\.be/([0-9A-Za-z-_]+)`),
	}
)

// supportedAnsible mirrors ansible-lint 26.8's default supported ansible-core
// series. Matching is a substring test against `requires_ansible`.
var supportedAnsible = []string{"2.15.", "2.16.", "2.17.", "2.18.", "2.19."}

// requiredGalaxyTags is the certification tag list defined by the Automation
// Hub team.
var requiredGalaxyTags = []string{
	"application", "cloud", "database", "eda", "infrastructure", "linux",
	"monitoring", "networking", "security", "storage", "tools", "windows",
}

const (
	maxTagsCount = 20
	maxTagLength = 64
)

// metaDefaultValues are the placeholder values that ansible-galaxy scaffolds.
var metaDefaultValues = [][2]string{
	{"author", "your name"},
	{"description", "your description"},
	{"company", "your company (optional)"},
	{"license", "license (GPLv2, CC-BY, etc)"},
	{"license", "license (GPL-2.0-or-later, MIT, etc)"},
}

func metaRules(f *parse.File) []Finding {
	if f.Kind != "meta" || f.Root == nil {
		return nil
	}
	galaxyInfo := parse.MapGet(f.Root, "galaxy_info")
	if galaxyInfo == nil {
		return nil
	}
	var out []Finding
	out = append(out, metaNoTags(f, galaxyInfo)...)
	out = append(out, metaIncorrect(f, galaxyInfo)...)
	out = append(out, metaVideoLinks(f, galaxyInfo)...)
	return out
}

func metaNoTags(f *parse.File, galaxyInfo *yaml.Node) []Finding {
	var out []Finding
	var tags []*yaml.Node

	if v := parse.MapGet(galaxyInfo, "galaxy_tags"); v != nil {
		if parse.IsSeq(v) {
			tags = append(tags, v.Content...)
		} else {
			out = append(out, whole(f, "meta-no-tags", "Expected 'galaxy_tags' to be a list",
				"galaxy_tags is not a list. Write it as a YAML list of tag strings."))
		}
	}
	if v := parse.MapGet(galaxyInfo, "categories"); v != nil {
		out = append(out, whole(f, "meta-no-tags", "Use 'galaxy_tags' rather than 'categories'",
			"This role uses the obsolete 'categories' key. Rename it to galaxy_tags."))
		if parse.IsSeq(v) {
			tags = append(tags, v.Content...)
		} else {
			out = append(out, whole(f, "meta-no-tags", "Expected 'categories' to be a list",
				"'categories' is not a list. Write it as a YAML list of tag strings."))
		}
	}

	for _, tag := range tags {
		// A plain `no` carries the !!str tag here but is a bool to ansible, so
		// the tag alone does not decide whether the value is a string.
		_, isPyBool := parse.PyBool(tag)
		if !parse.IsScalar(tag) || tag.Tag != "!!str" || isPyBool {
			out = append(out, whole(f, "meta-no-tags",
				fmt.Sprintf("Tags must be strings: '%s'", pyStr(tag)),
				"This role tag is not a string. Quote it so it reads as a tag."))
			continue
		}
		if !reMetaTag.MatchString(tag.Value) {
			out = append(out, whole(f, "meta-no-tags",
				fmt.Sprintf("Tags must contain lowercase letters and digits only., invalid: '%s'", tag.Value),
				"This role tag has characters outside a-z and 0-9. Rename it."))
		}
	}
	return out
}

func metaIncorrect(f *parse.File, galaxyInfo *yaml.Node) []Finding {
	var out []Finding
	for _, fd := range metaDefaultValues {
		v := parse.MapGet(galaxyInfo, fd[0])
		if v != nil && parse.Str(v) == fd[1] {
			out = append(out, at(f, f.Root, "meta-incorrect",
				"Should change default metadata: "+fd[0],
				"The '"+fd[0]+"' field still holds its scaffolded placeholder. Replace it."))
		}
	}
	return out
}

func metaVideoLinks(f *parse.File, galaxyInfo *yaml.Node) []Finding {
	links := parse.MapGet(galaxyInfo, "video_links")
	if !parse.IsSeq(links) {
		return nil
	}
	var out []Finding
	for _, video := range links.Content {
		if !parse.IsMap(video) {
			out = append(out, at(f, video, "meta-video-links",
				"Expected item in 'video_links' to be a dictionary",
				"This video_links entry is not a mapping. Give it url and title keys."))
			continue
		}
		unexpected := false
		for _, k := range parse.MapKeys(video) {
			if k != "url" && k != "title" {
				unexpected = true
			}
		}
		if unexpected {
			out = append(out, at(f, video, "meta-video-links",
				"Expected item in 'video_links' to contain only keys 'url' and 'title'",
				"This video_links entry has keys beyond url and title. Remove the extras."))
			continue
		}
		urlNode := parse.MapGet(video, "url")
		url := parse.Str(urlNode)
		matched := false
		for _, re := range reVideoURL {
			if re.MatchString(url) {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, at(f, urlNode, "meta-video-links",
				fmt.Sprintf("URL format '%s' is not recognized. Expected it be a shared link from Vimeo, YouTube, or Google Drive.", url),
				"This video link is not a Vimeo, YouTube or Google Drive share URL. Replace it."))
		}
	}
	return out
}

func metaRuntimeRules(f *parse.File) []Finding {
	if f.Kind != "meta-runtime" || f.Root == nil {
		return nil
	}
	requires := parse.Str(parse.MapGet(f.Root, "requires_ansible"))
	if requires == "" {
		return nil
	}
	var out []Finding
	supported := false
	for _, v := range supportedAnsible {
		if strings.Contains(requires, v) {
			supported = true
			break
		}
	}
	if !supported {
		hints := make([]string, 0, len(supportedAnsible))
		for _, v := range supportedAnsible {
			hints = append(hints, ">="+v+"0")
		}
		out = append(out, whole(f, "meta-runtime[unsupported-version]",
			"'requires_ansible' key must refer to a currently supported version such as: "+strings.Join(hints, ", "),
			"requires_ansible names no supported ansible-core. Require 2.15 or newer."))
	}
	if !validSpecifierSet(requires) {
		out = append(out, whole(f, "meta-runtime[invalid-version]",
			"'requires_ansible' is not a valid requirement specification",
			"requires_ansible is not a valid version specifier. Fix the constraint."))
	}
	return out
}

func validSpecifierSet(s string) bool {
	for _, part := range strings.Split(s, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if !reSpecifier.MatchString(part) {
			return false
		}
	}
	return true
}

func galaxyRules(f *parse.File) []Finding {
	if f.Kind != "galaxy" || !parse.IsMap(f.Root) {
		return nil
	}
	base := filepath.Dir(f.Abs)
	var out []Finding

	if deps := parse.MapGet(f.Root, "dependencies"); parse.IsMap(deps) {
		for i := 0; i+1 < len(deps.Content); i += 2 {
			if strings.TrimSpace(parse.Str(deps.Content[i+1])) == "" {
				out = append(out, whole(f, "galaxy[invalid-dependency-version]",
					fmt.Sprintf("Invalid collection metadata. Dependency version spec range is invalid for '%s'.", deps.Content[i].Value),
					"This dependency has an invalid version specifier. Fix the version range."))
			}
		}
	}

	if !anyFileExists(base, "changelogs/changelog.yaml", "changelogs/changelog.yml", "CHANGELOG.rst", "CHANGELOG.md") {
		out = append(out, whole(f, "galaxy[no-changelog]",
			"No changelog found. Please add a changelog file. Refer to the galaxy.md file for more info.",
			"This collection has no changelog. Add a CHANGELOG.rst or similar."))
	}

	tags := parse.StrList(parse.MapGet(f.Root, "tags"))
	if !containsAny(tags, requiredGalaxyTags) {
		out = append(out, whole(f, "galaxy[tags]",
			fmt.Sprintf("galaxy.yaml must have one of the required tags: ['%s']", strings.Join(requiredGalaxyTags, "', '")),
			"This collection has no required certification tag. Add one, such as linux."))
	}
	var badFormat, badLength []string
	for _, t := range tags {
		if !reGalaxyTag.MatchString(t) || reDoubleUnder.MatchString(t) {
			badFormat = append(badFormat, t)
		}
		if len(t) > maxTagLength {
			badLength = append(badLength, t)
		}
	}
	if len(badFormat) > 0 {
		out = append(out, whole(f, "galaxy[tags-format]",
			"galaxy.yaml must have properly formatted tags. Invalid tags: "+strings.Join(badFormat, ","),
			"A collection tag is not lowercase alphanumeric. Rename it to Galaxy's format."))
	}
	if len(badLength) > 0 {
		out = append(out, whole(f, "galaxy[tags-length]",
			fmt.Sprintf("galaxy.yaml tags must not exceed %d characters. Invalid tags: %s", maxTagLength, strings.Join(badLength, ",")),
			fmt.Sprintf("A collection tag is longer than %d characters. Shorten it.", maxTagLength)))
	}
	if len(tags) > maxTagsCount {
		out = append(out, whole(f, "galaxy[tags-count]",
			fmt.Sprintf("galaxy.yaml exceeds %d tags. Current count: %d", maxTagsCount, len(tags)),
			fmt.Sprintf("This collection declares more than %d tags. Remove some.", maxTagsCount)))
	}

	if !parse.MapHas(f.Root, "version") {
		return append(out, at(f, f.Root, "galaxy[version-missing]", "galaxy.yaml should have version tag.",
			"This collection's galaxy.yml has no version field. Add one."))
	}
	if !anyFileExists(base, "meta/runtime.yml") {
		out = append(out, whole(f, "galaxy[no-runtime]", "meta/runtime.yml file not found.",
			"This collection has no meta/runtime.yml. Add one to declare ansible-core support."))
	}
	if !parse.MapHas(f.Root, "repository") {
		out = append(out, whole(f, "galaxy[no-repository]",
			"galaxy.yaml should have a repository key for publication to Galaxy. See https://docs.ansible.com/projects/ansible/latest/dev_guide/collections_galaxy_meta.html",
			"This collection's galaxy.yml has no repository field. Add one."))
	}
	if !parse.MapHas(f.Root, "license") && !parse.MapHas(f.Root, "license_file") {
		out = append(out, whole(f, "galaxy[no-license]",
			"galaxy.yaml should have a license or license_file key for publication to Galaxy. See https://docs.ansible.com/projects/ansible/latest/dev_guide/collections_galaxy_meta.html",
			"This collection's galaxy.yml has no license. Add license or license_file."))
	}
	return out
}

// roleNameMetaDeps flags path-style role dependencies declared in meta/main.yml.
func roleNameMetaDeps(f *parse.File) []Finding {
	if f.Kind != "meta" || f.Root == nil {
		return nil
	}
	deps := parse.MapGet(f.Root, "dependencies")
	if !parse.IsSeq(deps) {
		return nil
	}
	var out []Finding
	for _, role := range deps.Content {
		node := role
		if parse.IsMap(role) {
			node = parse.MapGet(role, "role")
		}
		name := parse.Str(node)
		if !strings.Contains(name, "/") {
			continue
		}
		out = append(out, at(f, node, "role-name[path]",
			fmt.Sprintf("Avoid using paths when importing roles. (%s)", name),
			rolePathNativeMsg))
	}
	return out
}

// RoleDir checks the name of a role directory.
func RoleDir(displayPath, abs string) []Finding {
	name := inferRoleName(filepath.Join(abs, "meta", "main.yml"), filepath.Base(abs))
	name = strings.TrimPrefix(name, "ansible-role-")
	if name == "" || reRoleName.MatchString(name) {
		return nil
	}
	return []Finding{{
		Path: displayPath, Line: 1, Tag: "role-name",
		Message:       fmt.Sprintf("Role name %s does not match ``^[a-z][a-z0-9_]*$`` pattern.", name),
		NativeMessage: "This role name has invalid characters. Use only a-z, 0-9 and underscore.",
	}}
}

func inferRoleName(metaPath, fallback string) string {
	data, err := safeio.ReadFile(metaPath, safeio.MaxLintableBytes)
	if err != nil {
		return fallback
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return fallback
	}
	if name := parse.Str(parse.MapGet(parse.MapGet(doc.Content[0], "galaxy_info"), "role_name")); name != "" {
		return name
	}
	return fallback
}

func anyFileExists(base string, names ...string) bool {
	for _, n := range names {
		if fi, err := os.Stat(filepath.Join(base, filepath.FromSlash(n))); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

func containsAny(have, want []string) bool {
	set := make(map[string]bool, len(want))
	for _, w := range want {
		set[w] = true
	}
	for _, h := range have {
		if set[h] {
			return true
		}
	}
	return false
}
