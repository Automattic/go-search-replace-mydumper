package main

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
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

func useTestSizes(t *testing.T) {
	t.Helper()

	oldBufferSize, oldMax := bufferSize, maxLineSize
	bufferSize, maxLineSize = 4096, 1024*1024
	t.Cleanup(func() {
		bufferSize, maxLineSize = oldBufferSize, oldMax
	})
}

func runSplitStream(t *testing.T, stream string, replacements []*searchreplace.Replacement) string {
	t.Helper()

	useTestSizes(t)

	outputDir := t.TempDir()
	if err := splitStream(strings.NewReader(stream), outputDir, replacements); err != nil {
		t.Fatalf("splitStream() unexpected error: %v", err)
	}

	return outputDir
}

func readOutputFile(t *testing.T, outputDir, name string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(outputDir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}

	return string(content)
}

func listOutputFiles(t *testing.T, outputDir string) []string {
	t.Helper()

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		t.Fatalf("reading output dir: %v", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

const testHeader = "# Started dump at: 2026-10-16 15:23:56\n[config]\nquote-character = BACKTICK\n\n[myloader_session_variables]\nSQL_MODE='NO_AUTO_VALUE_ON_ZERO' /*!40101\n"

func TestSplitStreamMergesMetadataHeaderIntoMetadata(t *testing.T) {
	stream := "-- metadata.header 120\n" + testHeader +
		"-- db-schema-create.sql 40\nCREATE DATABASE `db` /* //old.example */;\n" +
		"-- db.wp_posts-schema.sql 40\nCREATE TABLE `wp_posts` (`guid` varchar(255));\n" +
		"-- db.wp_posts.00000.sql 60\nINSERT INTO `wp_posts` VALUES ('https://old.example/1');\n" +
		"-- metadata 60\n[`db`.`wp_posts`]\nreal_table_name=wp_posts\nrows = 1\n\n[config]\nnum-sequences = 0\n"

	replacements := []*searchreplace.Replacement{
		{From: []byte("//old.example"), To: []byte("//new.example")},
	}

	outputDir := runSplitStream(t, stream, replacements)

	wantFiles := []string{"db-schema-create.sql", "db.wp_posts-schema.sql", "db.wp_posts.00000.sql", "metadata"}
	if got := listOutputFiles(t, outputDir); strings.Join(got, ",") != strings.Join(wantFiles, ",") {
		t.Fatalf("output files = %v, want %v", got, wantFiles)
	}

	wantMetadata := testHeader + "[`db`.`wp_posts`]\nreal_table_name=wp_posts\nrows = 1\n\n[config]\nnum-sequences = 0\n"
	if got := readOutputFile(t, outputDir, "metadata"); got != wantMetadata {
		t.Fatalf("metadata mismatch, got %q want %q", got, wantMetadata)
	}

	if got, want := readOutputFile(t, outputDir, "db.wp_posts.00000.sql"), "INSERT INTO `wp_posts` VALUES ('https://new.example/1');\n"; got != want {
		t.Fatalf("data file mismatch, got %q want %q", got, want)
	}

	// Replacements only apply to data files.
	if got, want := readOutputFile(t, outputDir, "db-schema-create.sql"), "CREATE DATABASE `db` /* //old.example */;\n"; got != want {
		t.Fatalf("schema file mismatch, got %q want %q", got, want)
	}
}

func TestSplitStreamKeepsMetadataHeaderWithoutMetadata(t *testing.T) {
	stream := "-- metadata.header 120\n" + testHeader +
		"-- db.wp_posts.00000.sql 30\nINSERT INTO `wp_posts` VALUES (1);\n"

	outputDir := runSplitStream(t, stream, nil)

	if got := readOutputFile(t, outputDir, "metadata.header"); got != testHeader {
		t.Fatalf("metadata.header mismatch, got %q want %q", got, testHeader)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "metadata")); !os.IsNotExist(err) {
		t.Fatalf("metadata should not be created, stat error: %v", err)
	}
}

func TestSplitStreamTruncatesExistingFiles(t *testing.T) {
	useTestSizes(t)

	outputDir := t.TempDir()
	stale := filepath.Join(outputDir, "db.wp_posts.00000.sql")
	if err := os.WriteFile(stale, []byte(strings.Repeat("stale content ", 10)), 0644); err != nil {
		t.Fatalf("writing stale file: %v", err)
	}

	stream := "-- db.wp_posts.00000.sql 30\nINSERT INTO `wp_posts` VALUES (1);\n"
	if err := splitStream(strings.NewReader(stream), outputDir, nil); err != nil {
		t.Fatalf("splitStream() unexpected error: %v", err)
	}

	if got, want := readOutputFile(t, outputDir, "db.wp_posts.00000.sql"), "INSERT INTO `wp_posts` VALUES (1);\n"; got != want {
		t.Fatalf("data file mismatch, got %q want %q", got, want)
	}
}

func TestSplitStreamReturnsErrorWhenOutputDirIsMissing(t *testing.T) {
	useTestSizes(t)

	missingDir := filepath.Join(t.TempDir(), "missing")

	err := splitStream(strings.NewReader("-- db.wp_posts.00000.sql 30\nINSERT INTO `wp_posts` VALUES (1);\n"), missingDir, nil)
	if err == nil || !strings.Contains(err.Error(), "error opening file") {
		t.Fatalf("splitStream() error = %v, want an error opening file", err)
	}
}

func TestSplitStreamKeepsMetadataHeaderThatComesAfterMetadata(t *testing.T) {
	metadata := "[`db`.`wp_posts`]\nreal_table_name=wp_posts\n"
	stream := "-- metadata 40\n" + metadata + "-- metadata.header 120\n" + testHeader

	outputDir := runSplitStream(t, stream, nil)

	if got := readOutputFile(t, outputDir, "metadata"); got != metadata {
		t.Fatalf("metadata mismatch, got %q want %q", got, metadata)
	}
	if got := readOutputFile(t, outputDir, "metadata.header"); got != testHeader {
		t.Fatalf("metadata.header mismatch, got %q want %q", got, testHeader)
	}
}

func TestSplitStreamKeepsEmptyMetadataHeaderWithoutMetadata(t *testing.T) {
	outputDir := runSplitStream(t, "-- metadata.header 0\n", nil)

	if got := readOutputFile(t, outputDir, "metadata.header"); got != "" {
		t.Fatalf("metadata.header should be empty, got %q", got)
	}
}

func TestSplitStreamDropsNonMarkerCommentLines(t *testing.T) {
	stream := "-- db.wp_posts.00000.sql 60\n" +
		"-- not a file marker\n" +
		"INSERT INTO `wp_posts` VALUES (1);\n" +
		"-- metadata.header 120\n" + testHeader +
		"-- another comment\n" +
		"-- metadata 20\n[config]\nnum-sequences = 0\n"

	outputDir := runSplitStream(t, stream, nil)

	if got, want := readOutputFile(t, outputDir, "db.wp_posts.00000.sql"), "INSERT INTO `wp_posts` VALUES (1);\n"; got != want {
		t.Fatalf("data file mismatch, got %q want %q", got, want)
	}
	if got, want := readOutputFile(t, outputDir, "metadata"), testHeader+"[config]\nnum-sequences = 0\n"; got != want {
		t.Fatalf("metadata mismatch, got %q want %q", got, want)
	}
}

func TestSplitStreamPassesPartialMetadataThrough(t *testing.T) {
	partial := "[`db`.`wp_posts`]\nreal_table_name=wp_posts\nrows = 1\n"
	stream := "-- metadata.header 120\n" + testHeader +
		"-- metadata.partial.0 50\n" + partial +
		"-- metadata 20\n[config]\nnum-sequences = 0\n"

	outputDir := runSplitStream(t, stream, nil)

	if got := readOutputFile(t, outputDir, "metadata.partial.0"); got != partial {
		t.Fatalf("metadata.partial.0 mismatch, got %q want %q", got, partial)
	}
	if got, want := readOutputFile(t, outputDir, "metadata"), testHeader+"[config]\nnum-sequences = 0\n"; got != want {
		t.Fatalf("metadata mismatch, got %q want %q", got, want)
	}
}
