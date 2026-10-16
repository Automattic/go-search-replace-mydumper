package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Automattic/go-search-replace/searchreplace"
	"github.com/klauspost/pgzip"
)

const (
	badInputRe   = `\w:\d+:`
	inputRe      = `^[A-Za-z0-9_\-\.:/]+$`
	minInLength  = 4
	minOutLength = 2
)

const (
	metadataFilename       = "metadata"
	metadataHeaderFilename = "metadata.header"
)

var (
	dataFileRegex = regexp.MustCompile(`\d+.sql$`)
	filenameRegex = regexp.MustCompile(`^--\s+([\S]+)\s+\d+`)
	input         = regexp.MustCompile(inputRe)
	bad           = regexp.MustCompile(badInputRe)
	bufferSize    int
	maxLineSize   int64
)

func main() {
	// Define flags first
	flag.IntVar(&bufferSize, "buffer-size", 2*1024*1024, "Size of read buffer in bytes")
	flag.Int64Var(&maxLineSize, "max-line-size", 512*1024*1024, "Maximum allowed line size in bytes")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <input file> <output dir> <from> <to> ...\n\nOptions:\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	args := flag.Args()

	if len(args) < 2 {
		flag.Usage()

		os.Exit(1)
		return
	}

	inputFilePath := args[0]

	if _, err := os.Stat(inputFilePath); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, fmt.Sprintf("File %s does not exist", inputFilePath))
		os.Exit(1)
		return
	}

	inputFile, err := os.Open(inputFilePath)
	if err != nil {
		panic(err)
	}
	defer inputFile.Close()

	// Create a reader based on file extension
	var reader io.Reader
	if filepath.Ext(inputFilePath) == ".gz" {
		gzr, err := pgzip.NewReader(inputFile)
		if err != nil {
			panic(err)
		}
		defer gzr.Close()
		reader = gzr
	} else {
		reader = inputFile
	}

	outputDir := args[1]

	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			fmt.Fprintln(os.Stderr, fmt.Sprintf("Error creating output directory: %v", err))
			os.Exit(1)
			return
		}
	}

	// Remove the first two arguments and leave only the replacements
	rawReplacements := args[2:]

	var replacements []*searchreplace.Replacement

	if len(rawReplacements)%2 > 0 {
		fmt.Fprintln(os.Stderr, "All replacements must have a <from> and <to> value")
		os.Exit(1)
		return
	}

	fmt.Println("go-search-replace-mydumper: Processing file:", inputFilePath)
	fmt.Println("go-search-replace-mydumper: Output directory:", outputDir)
	fmt.Println("go-search-replace-mydumper: Replacements:", rawReplacements)

	start := time.Now()

	var from, to string
	for i := 0; i < len(rawReplacements)/2; i++ {
		from = rawReplacements[i*2]
		if !validInput(from, minInLength) {
			fmt.Fprintln(os.Stderr, "Invalid <from> URL, minimum length is 4")
			os.Exit(2)
			return
		}

		to = rawReplacements[(i*2)+1]
		if !validInput(to, minOutLength) {
			fmt.Fprintln(os.Stderr, "Invalid <to>, minimum length is 2")
			os.Exit(3)
			return
		}

		replacements = append(replacements, &searchreplace.Replacement{
			From: []byte(from),
			To:   []byte(to),
		})
	}

	if err := splitStream(reader, outputDir, replacements); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
		return
	}

	fmt.Printf("go-search-replace-mydumper: Finished successfully. took %v\n", time.Since(start))
}

// splitStream splits a mydumper stream into one file per section in outputDir, applying the
// replacements to data files.
//
// mydumper streams start with a metadata.header section and end with a metadata section. myloader
// handles both files as global metadata and, when it finds both in a directory, can release its
// metadata lock twice and hang or crash. The header is therefore written at the top of the
// metadata file instead of to a file of its own (or on its own, if the stream has no metadata).
func splitStream(reader io.Reader, outputDir string, replacements []*searchreplace.Replacement) error {
	hasReplacements := len(replacements) > 0
	fromEntriesContainsRegex := fromEntriesContainsRegex(replacements)

	var header bytes.Buffer
	headerMerged := false

	var output *os.File
	var writer *bufio.Writer
	isDataFile := false

	closeCurrent := func() error {
		if writer != nil {
			if err := writer.Flush(); err != nil {
				return fmt.Errorf("error flushing buffer: %w", err)
			}
		}
		if output != nil {
			if err := output.Close(); err != nil {
				return fmt.Errorf("error closing file: %w", err)
			}
		}
		output, writer = nil, nil
		return nil
	}

	openFile := func(filename string) error {
		var err error
		output, err = os.OpenFile(filepath.Join(outputDir, filename), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return fmt.Errorf("error opening file: %w", err)
		}
		writer = bufio.NewWriterSize(output, bufferSize)
		return nil
	}

	fileLinePrefix := []byte("-- ")

	r := bufio.NewReaderSize(reader, bufferSize)

	for {
		line, err := readFullLine(r)

		if err != nil {
			if err != io.EOF {
				return err
			}
			if len(line) == 0 {
				break
			}
		}

		if bytes.HasPrefix(line, fileLinePrefix) {
			if matches := filenameRegex.FindSubmatch(line); matches != nil {
				if err := closeCurrent(); err != nil {
					return err
				}

				newFilename := string(matches[1])
				isDataFile = dataFileRegex.MatchString(newFilename)

				switch newFilename {
				case metadataHeaderFilename:
					writer = bufio.NewWriter(&header)
				case metadataFilename:
					if err := openFile(newFilename); err != nil {
						return err
					}
					if _, err := writer.Write(header.Bytes()); err != nil {
						return fmt.Errorf("error writing to buffer: %w", err)
					}
					headerMerged = true
				default:
					if err := openFile(newFilename); err != nil {
						return err
					}
				}
			}

			continue
		}

		if writer == nil {
			continue
		}

		if isDataFile && hasReplacements && fromEntriesContainsRegex.Match(line) {
			line = *searchreplace.FixLine(&line, replacements)
		}
		if _, err := writer.Write(line); err != nil {
			return fmt.Errorf("error writing to buffer: %w", err)
		}
	}

	if err := closeCurrent(); err != nil {
		return err
	}

	if header.Len() > 0 && !headerMerged {
		if err := os.WriteFile(filepath.Join(outputDir, metadataHeaderFilename), header.Bytes(), 0644); err != nil {
			return fmt.Errorf("error writing %s: %w", metadataHeaderFilename, err)
		}
	}

	return nil
}

// readFullLine reads a complete line from the reader, handling lines larger than the buffer size
// by joining fragments until the complete line is read
func readFullLine(r *bufio.Reader) ([]byte, error) {
	var lineBuffer bytes.Buffer
	var currentSize int64

	for {
		fragment, isPrefix, err := r.ReadLine()

		if err != nil {
			return lineBuffer.Bytes(), err
		}

		currentSize += int64(len(fragment))
		if currentSize > maxLineSize {
			return nil, fmt.Errorf("line exceeds maximum size of %d MB", maxLineSize/(1024*1024))
		}

		lineBuffer.Write(fragment)

		if !isPrefix {
			lineBuffer.Write([]byte{'\n'})

			return lineBuffer.Bytes(), nil
		}
	}
}

func fromEntriesContainsRegex(replacements []*searchreplace.Replacement) *regexp.Regexp {
	allFromStartWithSlash := true

	fromEntries := make([]string, len(replacements))
	for i, replacement := range replacements {
		fromEntries[i] = string(replacement.From)
		if !bytes.HasPrefix(replacement.From, []byte("//")) {
			allFromStartWithSlash = false
		}
	}

	fromEntriesContainsPattern := ""

	// If all replacements start with a double-slash (which is a common scenario), we
	// can optimize the regex by adding the double-slash at the beginning of the regex.
	// Sort of Aho–Corasick algorithm.
	if allFromStartWithSlash {
		fromEntriesContainsPattern += regexp.QuoteMeta("//")
	}

	fromEntriesContainsPattern += "(?:"
	for i, from := range fromEntries {
		if i > 0 {
			fromEntriesContainsPattern += "|"
		}

		if allFromStartWithSlash {
			from = from[2:]
		}

		fromEntriesContainsPattern += regexp.QuoteMeta(from)
	}
	fromEntriesContainsPattern += ")"

	return regexp.MustCompile(fromEntriesContainsPattern)
}

func validInput(in string, length int) bool {
	if len(in) < length {
		return false
	}

	if !input.MatchString(in) {
		return false
	}

	if bad.MatchString(in) {
		return false
	}

	return true
}
