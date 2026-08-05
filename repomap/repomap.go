// Package repomap builds a compact file-tree representation of a repository
// for injection into the system prompt on every Step. The map shows file
// paths, not source code.
package repomap

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	// defaultMaxTokens bounds the map's approximate token cost (bytes/4).
	defaultMaxTokens = 2000
	// defaultMaxEntries bounds the number of rendered tree lines.
	defaultMaxEntries = 1000
)

// Builder walks a repository and produces a compact file-tree map. It is
// cheap to construct and every Build call walks the tree fresh, so no map is
// cached across Steps or Turns.
type Builder struct {
	// Root is the absolute repository root to map.
	Root string
	// Exclude is a configurable exclusion policy: each entry is a gitignore
	// style glob applied from the repository root.
	Exclude []string
	// MaxTokens bounds the map's approximate token size. Zero selects the
	// package default.
	MaxTokens int
	// MaxEntries bounds the number of rendered tree lines. Zero selects the
	// package default.
	MaxEntries int
}

// NewBuilder constructs a Builder rooted at root with the given exclusion
// policy. Root must be a real directory.
func NewBuilder(root string, exclude []string) (*Builder, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("repomap: repository root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("repomap: resolve root: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("repomap: stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repomap: root %q is not a directory", root)
	}
	return &Builder{
		Root:       absolute,
		Exclude:    exclude,
		MaxTokens:  defaultMaxTokens,
		MaxEntries: defaultMaxEntries,
	}, nil
}

// Build walks the repository fresh and returns the compact file-tree map.
func (b *Builder) Build() (string, error) {
	if b == nil {
		return "", errors.New("repomap: builder is nil")
	}
	rootSet := &ignoreSet{}
	for _, ex := range b.Exclude {
		if p, ok := parseLine(ex); ok {
			rootSet.matchers = append(rootSet.matchers, matcher{base: "", p: p})
		}
	}
	tree := &node{name: ".", isDir: true}
	if err := b.walk("", b.Root, rootSet, tree); err != nil {
		return "", err
	}

	var lines []string
	collectLines(tree.children, "", &lines)

	maxEntries := b.MaxEntries
	if maxEntries <= 0 {
		maxEntries = defaultMaxEntries
	}
	maxTokens := b.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	if len(lines) > maxEntries {
		lines = lines[:maxEntries]
		lines = append(lines, fmt.Sprintf("... (map truncated: exceeds %d entries)", maxEntries))
	}

	maxBytes := maxTokens * 4
	var out bytes.Buffer
	for _, line := range lines {
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		if out.Len()+len(line) > maxBytes {
			fmt.Fprintf(&out, "... (map truncated to stay within %d tokens)", maxTokens)
			break
		}
		out.WriteString(line)
	}
	return out.String(), nil
}

// walk descends into dirAbs, honoring .gitignore files and the exclusion
// policy, and populates the tree node.
func (b *Builder) walk(dirRel, dirAbs string, set *ignoreSet, tree *node) error {
	child := set.clone()
	if err := readGitignore(dirRel, dirAbs, child); err != nil {
		return err
	}
	entries, err := os.ReadDir(dirAbs)
	if err != nil {
		return fmt.Errorf("repomap: read dir %q: %w", dirAbs, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		rel := path.Join(dirRel, name)
		isDir := entry.IsDir()
		if name == ".git" && isDir {
			continue
		}
		if child.ignored(rel, isDir) {
			continue
		}
		childNode := &node{name: name, isDir: isDir}
		tree.children = append(tree.children, childNode)
		if isDir {
			if err := b.walk(rel, filepath.Join(dirAbs, name), child, childNode); err != nil {
				return err
			}
		}
	}
	return nil
}

// node is one entry in the rendered file tree.
type node struct {
	name     string
	isDir    bool
	children []*node
}

func collectLines(nodes []*node, prefix string, lines *[]string) {
	for i, n := range nodes {
		last := i == len(nodes)-1
		connector := "├── "
		spacer := "│   "
		if last {
			connector = "└── "
			spacer = "    "
		}
		line := prefix + connector + n.name
		if n.isDir {
			line += "/"
		}
		*lines = append(*lines, line)
		if n.isDir {
			collectLines(n.children, prefix+spacer, lines)
		}
	}
}

// matcher couples a parsed gitignore pattern with the directory it was read
// from so paths can be matched relative to that directory.
type matcher struct {
	base string
	p    pattern
}

// ignoreSet is an ordered set of patterns from applicable .gitignore files and
// the root exclusion policy. Later patterns take precedence (last match wins),
// which is how nested .gitignore negation works.
type ignoreSet struct {
	matchers []matcher
}

func (s *ignoreSet) clone() *ignoreSet {
	return &ignoreSet{matchers: append([]matcher(nil), s.matchers...)}
}

// ignored reports whether rel (slash-separated, relative to the repository
// root) is excluded by any applicable pattern.
func (s *ignoreSet) ignored(rel string, isDir bool) bool {
	ignored := false
	for i := range s.matchers {
		m := &s.matchers[i]
		r, ok := relToBase(m.base, rel)
		if !ok {
			continue
		}
		if m.p.matches(r, isDir) {
			ignored = !m.p.negated
		}
	}
	return ignored
}

// relToBase returns the path of rel relative to base, and whether rel is
// contained under base.
func relToBase(base, rel string) (string, bool) {
	if base == "" {
		return rel, true
	}
	if strings.HasPrefix(rel, base+"/") {
		return strings.TrimPrefix(rel, base+"/"), true
	}
	return "", false
}

// readGitignore parses dirAbs/.gitignore and appends its patterns to set,
// anchored at dirRel.
func readGitignore(dirRel, dirAbs string, set *ignoreSet) error {
	data, err := os.ReadFile(filepath.Join(dirAbs, ".gitignore"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("repomap: read .gitignore: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if p, ok := parseLine(line); ok {
			set.matchers = append(set.matchers, matcher{base: dirRel, p: p})
		}
	}
	return nil
}

// pattern is one parsed .gitignore line.
type pattern struct {
	negated  bool
	dirOnly  bool
	anchored bool
	segments []string
}

// parseLine parses a single .gitignore line into a pattern. The second return
// is false for blank lines, comments, and otherwise non-matching lines.
func parseLine(line string) (pattern, bool) {
	line = strings.TrimRight(line, " \t")
	if line == "" || strings.HasPrefix(line, "#") {
		return pattern{}, false
	}
	negated := false
	if strings.HasPrefix(line, "!") {
		negated = true
		line = line[1:]
	}
	if line == "" {
		return pattern{}, false
	}
	dirOnly := strings.HasSuffix(line, "/")
	if dirOnly {
		line = strings.TrimSuffix(line, "/")
	}
	if line == "" {
		return pattern{}, false
	}
	anchored := false
	if strings.HasPrefix(line, "/") {
		anchored = true
		line = strings.TrimPrefix(line, "/")
	}
	if strings.Contains(line, "/") {
		anchored = true
	}
	return pattern{
		negated:  negated,
		dirOnly:  dirOnly,
		anchored: anchored,
		segments: strings.Split(line, "/"),
	}, true
}

// matches reports whether the pattern matches rel (relative to the pattern's
// base directory).
func (p pattern) matches(rel string, isDir bool) bool {
	if p.dirOnly && !isDir {
		return false
	}
	if !p.anchored {
		return matchSegment(p.segments[0], path.Base(rel))
	}
	return matchSegments(p.segments, strings.Split(rel, "/"))
}

// matchSegments matches a slash-anchored pattern's segments against the path
// segments, honoring the ** wildcard.
func matchSegments(pat, path []string) bool {
	if len(pat) == 0 {
		return len(path) == 0
	}
	if pat[0] == "**" {
		skip := 0
		for skip < len(pat) && pat[skip] == "**" {
			skip++
		}
		for offset := 0; offset <= len(path); offset++ {
			if matchSegments(pat[skip:], path[offset:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 {
		return false
	}
	if !matchSegment(pat[0], path[0]) {
		return false
	}
	return matchSegments(pat[1:], path[1:])
}

// matchSegment matches a single path segment against a glob pattern supporting
// *, ?, and [set] wildcards.
func matchSegment(pat, s string) bool {
	return matchSegmentRec(pat, s)
}

func matchSegmentRec(p, s string) bool {
	for len(p) > 0 {
		switch p[0] {
		case '*':
			for len(p) > 0 && p[0] == '*' {
				p = p[1:]
			}
			if len(p) == 0 {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if matchSegmentRec(p, s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if len(s) == 0 {
				return false
			}
			p, s = p[1:], s[1:]
		case '[':
			negated, ranges, rest, ok := parseClass(p)
			if !ok {
				// Unterminated class: treat '[' literally.
				if len(s) == 0 || s[0] != '[' {
					return false
				}
				p, s = p[1:], s[1:]
				continue
			}
			if len(s) == 0 {
				return false
			}
			inClass := false
			for _, r := range ranges {
				if s[0] >= r[0] && s[0] <= r[1] {
					inClass = true
					break
				}
			}
			if inClass == negated {
				return false
			}
			p, s = rest, s[1:]
		default:
			if len(s) == 0 || p[0] != s[0] {
				return false
			}
			p, s = p[1:], s[1:]
		}
	}
	return len(s) == 0
}

// parseClass parses a [...] character class starting at p[0] == '['. It
// returns the negation flag, the set of byte ranges, the pattern remainder
// after the closing ']', and whether the class was well formed.
func parseClass(p string) (negated bool, ranges [][2]byte, rest string, ok bool) {
	i := 1
	if i < len(p) && (p[i] == '!' || p[i] == '^') {
		negated = true
		i++
	}
	for i < len(p) && p[i] != ']' {
		if i+2 < len(p) && p[i+1] == '-' && p[i+2] != ']' {
			ranges = append(ranges, [2]byte{p[i], p[i+2]})
			i += 3
		} else {
			ranges = append(ranges, [2]byte{p[i], p[i]})
			i++
		}
	}
	if i >= len(p) {
		return false, nil, "", false
	}
	return negated, ranges, p[i+1:], true
}
