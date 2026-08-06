package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// seedFixture writes a committed Go fixture repository with roughly 30 source
// files and exactly one seeded failing test, then returns its root. The fixture
// is the v0 benchmark repository: a clean, compiling Go module whose `go test
// ./...` fails only because of a bug in stats.Mean. It is committed so the
// Agent's real Git safety flow can be exercised end to end.
func seedFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for path, content := range fixtureFiles {
		target := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustFixtureGit(t, dir, "init", "-q")
	mustFixtureGit(t, dir, "config", "user.email", "benchmark@example.com")
	mustFixtureGit(t, dir, "config", "user.name", "Benchmark")
	mustFixtureGit(t, dir, "add", "--", ".")
	mustFixtureGit(t, dir, "commit", "-q", "-m", "seed fixture")
	return dir
}

// mustFixtureGit runs git in the fixture working tree, failing the test on error.
func mustFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out.String())
	}
	return out.String()
}

// fixtureBugFix is an exact-match replacement that repairs the seeded bug in
// stats/mean.go. It is what a correct Agent Turn applies before Validation and
// the Agent Commit.
const fixtureBugFix = `return sum / float64(len(values))`

// fixtureBuggyLine is the exact buggy line in stats/mean.go that the fixture
// seeds and the TestMean requirement targets.
const fixtureBuggyLine = `return sum / float64(len(values)-1)`

// fixtureFiles maps repository-relative paths to their committed content. The
// module compiles with the standard library only and every test passes except
// TestMean, which fails because of the line in stats/mean.go referenced by
// fixtureBuggyLine.
var fixtureFiles = map[string]string{
	"go.mod": "module example.com/textstats\n\ngo 1.21\n",

	".gitignore": "# Build and test artifacts from running go build / go test in the repo.\ntextstats\n*.test\n*.out\ncoverage.*\n",

	"counter/counter.go": `// Package counter counts words, lines, and characters in text.
package counter

import "strings"

// CountWords returns the number of whitespace-separated words in text.
func CountWords(text string) int {
	return len(strings.Fields(text))
}

// CountLines returns the number of lines in text. An empty text has zero lines.
func CountLines(text string) int {
	if text == "" {
		return 0
	}
	return 1 + strings.Count(text, "\n")
}
`,
	"counter/chars.go": `package counter

// CountChars returns the number of runes in text, ignoring newline characters.
func CountChars(text string) int {
	count := 0
	for _, r := range text {
		if r != '\n' && r != '\r' {
			count++
		}
	}
	return count
}
`,
	"counter/words.go": `package counter

import "strings"

// Words returns the individual whitespace-separated words in text.
func Words(text string) []string {
	return strings.Fields(text)
}
`,
	"counter/counter_test.go": `package counter

import "testing"

func TestCountWords(t *testing.T) {
	if got := CountWords(""); got != 0 {
		t.Errorf("CountWords empty = %d, want 0", got)
	}
	if got := CountWords("hello world"); got != 2 {
		t.Errorf("CountWords = %d, want 2", got)
	}
}

func TestCountLines(t *testing.T) {
	if got := CountLines(""); got != 0 {
		t.Errorf("CountLines empty = %d, want 0", got)
	}
	if got := CountLines("a\nb\nc"); got != 3 {
		t.Errorf("CountLines = %d, want 3", got)
	}
}

func TestCountChars(t *testing.T) {
	if got := CountChars("ab\ncd"); got != 4 {
		t.Errorf("CountChars = %d, want 4", got)
	}
}
`,

	"freq/freq.go": `// Package freq computes word frequency distributions.
package freq

import "strings"

// WordFrequency returns a map from each normalized word in text to its count.
func WordFrequency(text string) map[string]int {
	counts := make(map[string]int)
	for _, word := range strings.Fields(text) {
		counts[Normalize(word)]++
	}
	return counts
}
`,
	"freq/normalize.go": `package freq

import "strings"

// Normalize lowercases word and strips surrounding punctuation.
func Normalize(word string) string {
	word = strings.ToLower(word)
	return strings.Trim(word, ".,;:!?\"'()[]{}")
}
`,
	"freq/top.go": `package freq

import "sort"

type entry struct {
	word  string
	count int
}

// Top returns the top n words by descending frequency, breaking ties
// alphabetically. It returns fewer than n entries when there are not enough.
func Top(counts map[string]int, n int) []string {
	if n < 0 {
		n = 0
	}
	entries := make([]entry, 0, len(counts))
	for word, count := range counts {
		entries = append(entries, entry{word: word, count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].word < entries[j].word
	})
	if len(entries) > n {
		entries = entries[:n]
	}
	words := make([]string, 0, len(entries))
	for _, e := range entries {
		words = append(words, e.word)
	}
	return words
}
`,
	"freq/total.go": `package freq

// Total returns the total number of occurrences across all words.
func Total(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}
`,
	"freq/stopwords.go": `package freq

// stopwords are common words excluded from frequency counts when requested.
var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true,
}

// IsStopword reports whether word (already normalized) is a common stopword.
func IsStopword(word string) bool {
	return stopwords[word]
}
`,
	"freq/freq_test.go": `package freq

import (
	"reflect"
	"testing"
)

func TestWordFrequency(t *testing.T) {
	counts := WordFrequency("The cat, the dog.")
	if counts["the"] != 2 {
		t.Errorf("the = %d, want 2", counts["the"])
	}
	if counts["cat"] != 1 || counts["dog"] != 1 {
		t.Errorf("cat/dog = %d/%d, want 1/1", counts["cat"], counts["dog"])
	}
}

func TestTop(t *testing.T) {
	counts := map[string]int{"a": 1, "b": 3, "c": 2}
	got := Top(counts, 2)
	if !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Errorf("Top = %v, want [b c]", got)
	}
}

func TestNormalize(t *testing.T) {
	if got := Normalize("Hello,"); got != "hello" {
		t.Errorf("Normalize = %q, want hello", got)
	}
}

func TestIsStopword(t *testing.T) {
	if !IsStopword("the") {
		t.Error("the should be a stopword")
	}
	if IsStopword("cat") {
		t.Error("cat should not be a stopword")
	}
}

func TestTotal(t *testing.T) {
	if got := Total(map[string]int{"a": 1, "b": 2}); got != 3 {
		t.Errorf("Total = %d, want 3", got)
	}
}
`,

	"text/palindrome.go": `// Package text provides string utilities.
package text

// IsPalindrome reports whether s reads the same forward and backward,
// ignoring case and non-alphanumeric characters.
func IsPalindrome(s string) bool {
	runes := []rune{}
	for _, r := range s {
		if isAlphanumeric(r) {
			runes = append(runes, lower(r))
		}
	}
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		if runes[i] != runes[j] {
			return false
		}
	}
	return true
}
`,
	"text/anagram.go": `package text

import "sort"

// IsAnagram reports whether a and b contain the same alphanumeric characters
// ignoring case and order.
func IsAnagram(a, b string) bool {
	return sortedLetters(a) == sortedLetters(b)
}

func sortedLetters(s string) string {
	letters := []rune{}
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			letters = append(letters, lower(r))
		}
	}
	sort.Slice(letters, func(i, j int) bool { return letters[i] < letters[j] })
	return string(letters)
}
`,
	"text/sentences.go": `package text

import "strings"

// SplitSentences splits s into sentences on sentence-ending punctuation.
func SplitSentences(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '.' || r == '!' || r == '?'
	})
	sentences := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			sentences = append(sentences, trimmed)
		}
	}
	return sentences
}
`,
	"text/substring.go": `package text

// LongestCommonPrefix returns the longest prefix shared by a and b.
func LongestCommonPrefix(a, b string) string {
	ra := []rune(a)
	rb := []rune(b)
	n := len(ra)
	if len(rb) < n {
		n = len(rb)
	}
	i := 0
	for i < n && ra[i] == rb[i] {
		i++
	}
	return string(ra[:i])
}
`,
	"text/length.go": `package text

// RuneLength returns the number of runes in s.
func RuneLength(s string) int {
	return len([]rune(s))
}

// isAlphanumeric reports whether r is a letter or digit.
func isAlphanumeric(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
}

// lower converts an uppercase letter to lowercase.
func lower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}
`,
	"text/text_test.go": `package text

import (
	"reflect"
	"testing"
)

func TestIsPalindrome(t *testing.T) {
	if !IsPalindrome("racecar") {
		t.Error("racecar should be a palindrome")
	}
	if !IsPalindrome("A man, a plan, a canal: Panama") {
		t.Error("mixed-case palindrome should be recognized")
	}
	if IsPalindrome("hello") {
		t.Error("hello should not be a palindrome")
	}
}

func TestIsAnagram(t *testing.T) {
	if !IsAnagram("listen", "silent") {
		t.Error("listen and silent should be anagrams")
	}
	if IsAnagram("abc", "abd") {
		t.Error("abc and abd should not be anagrams")
	}
}

func TestSplitSentences(t *testing.T) {
	got := SplitSentences("Hello. How are you? Great!")
	want := []string{"Hello", "How are you", "Great"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitSentences = %v, want %v", got, want)
	}
}

func TestLongestCommonPrefix(t *testing.T) {
	if got := LongestCommonPrefix("foo-bar", "foo-baz"); got != "foo-ba" {
		t.Errorf("LongestCommonPrefix = %q, want foo-ba", got)
	}
}

func TestRuneLength(t *testing.T) {
	if got := RuneLength("héllo"); got != 5 {
		t.Errorf("RuneLength = %d, want 5", got)
	}
}
`,

	"format/format.go": `// Package format renders strings with alignment and truncation.
package format

import "strings"

// PadRight pads s on the right with spaces to width runes.
func PadRight(s string, width int) string {
	runes := []rune(s)
	if len(runes) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(runes))
}

// PadLeft pads s on the left with spaces to width runes.
func PadLeft(s string, width int) string {
	runes := []rune(s)
	if len(runes) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(runes)) + s
}

// Center centers s within width runes, padding equally on both sides.
func Center(s string, width int) string {
	runes := []rune(s)
	if len(runes) >= width {
		return s
	}
	total := width - len(runes)
	left := total / 2
	right := total - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}
`,
	"format/truncate.go": `package format

// Truncate shortens s to at most max runes, appending an ellipsis when it is
// truncated. A max of 0 returns an empty string.
func Truncate(s string, max int) string {
	if max < 0 {
		max = 0
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max == 0 {
		return ""
	}
	return string(runes[:max-1]) + "…"
}
`,
	"format/case.go": `package format

import "strings"

// TitleCase capitalizes the first letter of each word in s.
func TitleCase(s string) string {
	words := strings.Fields(s)
	for i, word := range words {
		if word == "" {
			continue
		}
		runes := []rune(word)
		runes[0] = upper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

func upper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - ('a' - 'A')
	}
	return r
}
`,
	"format/format_test.go": `package format

import "testing"

func TestPadRight(t *testing.T) {
	if got := PadRight("ab", 4); got != "ab  " {
		t.Errorf("PadRight = %q, want %q", got, "ab  ")
	}
	if got := PadRight("abcd", 4); got != "abcd" {
		t.Errorf("PadRight = %q, want abcd", got)
	}
}

func TestPadLeft(t *testing.T) {
	if got := PadLeft("ab", 4); got != "  ab" {
		t.Errorf("PadLeft = %q, want %q", got, "  ab")
	}
}

func TestCenter(t *testing.T) {
	if got := Center("ab", 6); got != "  ab  " {
		t.Errorf("Center = %q, want %q", got, "  ab  ")
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("hello", 3); got != "he…" {
		t.Errorf("Truncate = %q, want %q", got, "he…")
	}
	if got := Truncate("hi", 3); got != "hi" {
		t.Errorf("Truncate = %q, want hi", got)
	}
}

func TestTitleCase(t *testing.T) {
	if got := TitleCase("hello world"); got != "Hello World" {
		t.Errorf("TitleCase = %q, want %q", got, "Hello World")
	}
}
`,

	"scan/scan.go": `// Package scan tokenizes text and lines.
package scan

import "strings"

// Tokenize splits text into words, preserving order.
func Tokenize(text string) []string {
	return strings.Fields(text)
}
`,
	"scan/lines.go": `package scan

import "strings"

// Lines splits text into lines without trailing newlines.
func Lines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}
`,
	"scan/tokens.go": `package scan

// CountTokens returns the number of tokens in text.
func CountTokens(text string) int {
	return len(Tokenize(text))
}
`,
	"scan/scan_test.go": `package scan

import (
	"reflect"
	"testing"
)

func TestTokenize(t *testing.T) {
	got := Tokenize("one two\tthree")
	want := []string{"one", "two", "three"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tokenize = %v, want %v", got, want)
	}
}

func TestLines(t *testing.T) {
	got := Lines("a\nb\nc")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Lines = %v, want %v", got, want)
	}
}

func TestCountTokens(t *testing.T) {
	if got := CountTokens("a b c"); got != 3 {
		t.Errorf("CountTokens = %d, want 3", got)
	}
}
`,

	"stats/mean.go": `package stats

// Mean returns the arithmetic mean of values. An empty slice yields 0.
func Mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values)-1)
}
`,
	"stats/median.go": `package stats

import "sort"

// Median returns the middle value of a sorted values slice. An empty slice
// yields 0.
func Median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
`,
	"stats/variance.go": `package stats

import "math"

// Variance returns the population variance of values. Fewer than two values
// yields 0.
func Variance(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	sum := 0.0
	for _, v := range values {
		d := v - mean
		sum += d * d
	}
	return sum / float64(len(values))
}

// StdDev returns the population standard deviation of values.
func StdDev(values []float64) float64 {
	return math.Sqrt(Variance(values))
}
`,
	"stats/round.go": `package stats

import "math"

// Round rounds v to the nearest integer, rounding halves away from zero.
func Round(v float64) float64 {
	return math.Round(v)
}
`,
	"stats/sum.go": `package stats

// Sum returns the arithmetic sum of values.
func Sum(values []float64) float64 {
	total := 0.0
	for _, v := range values {
		total += v
	}
	return total
}
`,
	"stats/stats_test.go": `package stats

import "testing"

func TestMean(t *testing.T) {
	cases := []struct {
		name   string
		values []float64
		want   float64
	}{
		{"empty", nil, 0},
		{"single", []float64{5}, 5},
		{"two", []float64{2, 4}, 3},
		{"three", []float64{1, 2, 3}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Mean(tc.values); got != tc.want {
				t.Errorf("Mean(%v) = %v, want %v", tc.values, got, tc.want)
			}
		})
	}
}

func TestMedian(t *testing.T) {
	if got := Median([]float64{3, 1, 2}); got != 2 {
		t.Errorf("Median = %v, want 2", got)
	}
	if got := Median([]float64{1, 2, 3, 4}); got != 2.5 {
		t.Errorf("Median = %v, want 2.5", got)
	}
}

func TestVariance(t *testing.T) {
	if got := Variance([]float64{2, 4, 4, 4, 5, 5, 7, 9}); got != 4 {
		t.Errorf("Variance = %v, want 4", got)
	}
}

func TestStdDev(t *testing.T) {
	if got := StdDev([]float64{2, 4}); got != 1 {
		t.Errorf("StdDev = %v, want 1", got)
	}
}

func TestSum(t *testing.T) {
	if got := Sum([]float64{1, 2, 3}); got != 6 {
		t.Errorf("Sum = %v, want 6", got)
	}
}

func TestRound(t *testing.T) {
	if got := Round(3.4); got != 3 {
		t.Errorf("Round(3.4) = %v, want 3", got)
	}
	if got := Round(3.6); got != 4 {
		t.Errorf("Round(3.6) = %v, want 4", got)
	}
}
`,

	"cmd/textstats/main.go": `// Command textstats is a small demonstration CLI for the textstats library.
package main

import (
	"fmt"
	"os"

	"example.com/textstats/counter"
	"example.com/textstats/stats"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: textstats <word> <word> ...")
		os.Exit(1)
	}
	words := os.Args[1:]
	values := make([]float64, 0, len(words))
	joined := ""
	for i, w := range words {
		if i > 0 {
			joined += " "
		}
		joined += w
		values = append(values, float64(len(w)))
	}
	fmt.Printf("words: %d\n", counter.CountWords(joined))
	fmt.Printf("mean length: %.2f\n", stats.Mean(values))
}
`,
}
