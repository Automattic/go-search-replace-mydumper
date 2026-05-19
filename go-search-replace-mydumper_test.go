package main

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/Automattic/go-search-replace/searchreplace"
)

func TestReadFullLineJoinsFragmentsAndAddsNewline(t *testing.T) {
	oldMax := maxLineSize
	maxLineSize = 1024
	t.Cleanup(func() {
		maxLineSize = oldMax
	})

	// A small reader buffer forces ReadLine to return multiple fragments.
	r := bufio.NewReaderSize(strings.NewReader("abcdefghijklmnop\nnext\n"), 4)

	line, err := readFullLine(r)
	if err != nil {
		t.Fatalf("readFullLine() unexpected error: %v", err)
	}

	if got, want := string(line), "abcdefghijklmnop\n"; got != want {
		t.Fatalf("readFullLine() mismatch, got %q want %q", got, want)
	}

	line, err = readFullLine(r)
	if err != nil {
		t.Fatalf("readFullLine() unexpected error on second line: %v", err)
	}
	if got, want := string(line), "next\n"; got != want {
		t.Fatalf("second line mismatch, got %q want %q", got, want)
	}
}

func TestReadFullLineEnforcesMaxLineSize(t *testing.T) {
	oldMax := maxLineSize
	maxLineSize = 8
	t.Cleanup(func() {
		maxLineSize = oldMax
	})

	r := bufio.NewReaderSize(strings.NewReader("123456789\n"), 4)

	line, err := readFullLine(r)
	if err == nil {
		t.Fatalf("readFullLine() expected size error, got line %q", string(line))
	}
	if !strings.Contains(err.Error(), "line exceeds maximum size") {
		t.Fatalf("readFullLine() wrong error: %v", err)
	}
}

func TestReadFullLineAllowsExactlyMaxLineSize(t *testing.T) {
	oldMax := maxLineSize
	maxLineSize = 8
	t.Cleanup(func() {
		maxLineSize = oldMax
	})

	r := bufio.NewReaderSize(strings.NewReader("12345678\n"), 4)

	line, err := readFullLine(r)
	if err != nil {
		t.Fatalf("readFullLine() unexpected error: %v", err)
	}
	if got, want := string(line), "12345678\n"; got != want {
		t.Fatalf("readFullLine() mismatch, got %q want %q", got, want)
	}
}

func TestReadFullLineEOFWithoutTrailingNewlineReturnsDataThenEOF(t *testing.T) {
	oldMax := maxLineSize
	maxLineSize = 1024
	t.Cleanup(func() {
		maxLineSize = oldMax
	})

	r := bufio.NewReaderSize(strings.NewReader("tail-without-newline"), 4)

	line, err := readFullLine(r)
	if err != nil {
		t.Fatalf("readFullLine() unexpected error: %v", err)
	}
	if got, want := string(line), "tail-without-newline\n"; got != want {
		t.Fatalf("readFullLine() mismatch, got %q want %q", got, want)
	}

	line, err = readFullLine(r)
	if err != io.EOF {
		t.Fatalf("second readFullLine() error = %v, want %v", err, io.EOF)
	}
	if len(line) != 0 {
		t.Fatalf("second readFullLine() should return no bytes on EOF, got %q", string(line))
	}
}

func TestValidInput(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		length int
		want   bool
	}{
		{name: "valid URL-like input", in: "https://a-b.c/d_1", length: minInLength, want: true},
		{name: "valid short output", in: "ab", length: minOutLength, want: true},
		{name: "too short", in: "abc", length: minInLength, want: false},
		{name: "invalid character", in: "https://example.com?q=1", length: minInLength, want: false},
		{name: "bad pattern with word colon digits colon", in: "abcx:12:", length: minInLength, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := validInput(tc.in, tc.length)
			if got != tc.want {
				t.Fatalf("validInput(%q, %d) = %v, want %v", tc.in, tc.length, got, tc.want)
			}
		})
	}
}

func TestFromEntriesContainsRegexMatchesAndOptimizesDoubleSlashPrefix(t *testing.T) {
	replacements := []*searchreplace.Replacement{
		{From: []byte("//old.example/path"), To: []byte("new-a")},
		{From: []byte("//cdn.example/static"), To: []byte("new-b")},
	}

	re := fromEntriesContainsRegex(replacements)

	if got, wantPrefix := re.String(), `//(?:`; !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("expected optimized regex to start with %q, got %q", wantPrefix, got)
	}

	if !re.MatchString("prefix //old.example/path suffix") {
		t.Fatalf("optimized regex should match first from entry")
	}
	if !re.MatchString("prefix //cdn.example/static suffix") {
		t.Fatalf("optimized regex should match second from entry")
	}
	if re.MatchString("prefix old.example/path suffix") {
		t.Fatalf("optimized regex should not match without the // prefix")
	}
}

func TestFromEntriesContainsRegexMatchesWithoutOptimization(t *testing.T) {
	replacements := []*searchreplace.Replacement{
		{From: []byte("abc.example"), To: []byte("x")},
		{From: []byte("//cdn.example/static"), To: []byte("y")},
	}

	re := fromEntriesContainsRegex(replacements)

	if strings.HasPrefix(re.String(), `//(?:`) {
		t.Fatalf("did not expect optimization when not all from entries start with //: %q", re.String())
	}

	if !re.MatchString("start abc.example end") {
		t.Fatalf("regex should match non-slash from entry")
	}
	if !re.MatchString("start //cdn.example/static end") {
		t.Fatalf("regex should match slash-prefixed from entry")
	}
}

func TestPrefilterAndReplaceInteraction(t *testing.T) {
	replacements := []*searchreplace.Replacement{
		{From: []byte("//cdn.example/static"), To: []byte("//assets.example/static")},
	}

	hasReplacements := len(replacements) > 0
	re := fromEntriesContainsRegex(replacements)

	apply := func(in string) string {
		line := []byte(in)
		if hasReplacements && re.Match(line) {
			replaced := searchreplace.FixLine(&line, replacements)
			return string(*replaced)
		}
		return string(line)
	}

	if got, want := apply("INSERT INTO t VALUES ('//cdn.example/static/file.js');\n"), "INSERT INTO t VALUES ('//assets.example/static/file.js');\n"; got != want {
		t.Fatalf("replacement mismatch, got %q want %q", got, want)
	}

	nearMiss := "INSERT INTO t VALUES ('//cdn.example/statik/file.js');\n"
	if got := apply(nearMiss); got != nearMiss {
		t.Fatalf("near-miss line should not be changed, got %q want %q", got, nearMiss)
	}

	nonMatch := "INSERT INTO t VALUES ('//api.example/static/file.js');\n"
	if got := apply(nonMatch); got != nonMatch {
		t.Fatalf("non-match line should not be changed, got %q want %q", got, nonMatch)
	}
}

func TestFromEntriesContainsRegexGuardsAgainstMetacharacterFalsePositives(t *testing.T) {
	replacements := []*searchreplace.Replacement{
		{From: []byte("a+b(c).js?x=1"), To: []byte("ignored")},
		{From: []byte("foo.bar/baz"), To: []byte("ignored")},
	}

	re := fromEntriesContainsRegex(replacements)

	if !re.MatchString("prefix a+b(c).js?x=1 suffix") {
		t.Fatalf("regex should match literal metacharacter-containing from entry")
	}
	if re.MatchString("prefix aaabcc.jsax=1 suffix") {
		t.Fatalf("regex should not treat metacharacters as regex operators")
	}

	if !re.MatchString("prefix foo.bar/baz suffix") {
		t.Fatalf("regex should match literal dotted path")
	}
	if re.MatchString("prefix fooXbar/baz suffix") {
		t.Fatalf("regex should not match near-miss dotted path")
	}
}
