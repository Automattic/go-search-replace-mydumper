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

var (
	input       = regexp.MustCompile(inputRe)
	bad         = regexp.MustCompile(badInputRe)
	bufferSize  int
	maxLineSize int64
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

	dataFileRegex := regexp.MustCompile(`\d+.sql$`)

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

	hasReplacements := len(replacements) > 0

	fromEntries := make([]string, len(replacements))
	for i, replacement := range replacements {
		fromEntries[i] = string(replacement.From)
	}

	fromEntriesContainsPattern := "(?:"
	for i, from := range fromEntries {
		if i > 0 {
			fromEntriesContainsPattern += "|"
		}
		fromEntriesContainsPattern += regexp.QuoteMeta(from)
	}
	fromEntriesContainsPattern += ")"
	fromEntriesContainsRegex := regexp.MustCompile(fromEntriesContainsPattern)

	pattern := `^--\s+([\S]+)\s+\d+`
	filenameRegex := regexp.MustCompile(pattern)

	keep := true
	var newFilename string

	var output *os.File
	var writer *bufio.Writer

	fileLinePrefix := []byte("-- ")

	isDataFile := false

	r := bufio.NewReaderSize(reader, bufferSize)

	fileInfo, err := inputFile.Stat()
	if err != nil {
		panic(err)
	}

	totalFileSize := fileInfo.Size()
	bytesProcessed := int64(0)
	lastPrintedPercentage := 0

	for {
		line, err := readFullLine(r)
		bytesProcessed += int64(len(line))

		if err != nil {
			if err == io.EOF {
				if 0 == len(line) {
					break
				}
			} else {
				fmt.Fprintln(os.Stderr, err.Error())

				os.Exit(1)
			}
		}

		progress := float64(bytesProcessed) / float64(totalFileSize) * 100
		currentPercentage := int(progress/10) * 10

		if currentPercentage > lastPrintedPercentage {
			fmt.Printf("go-search-replace-mydumper: Processing: %d%% complete\n", currentPercentage)
			lastPrintedPercentage = currentPercentage
		}

		if bytes.HasPrefix(line, fileLinePrefix) {
			if matches := filenameRegex.FindSubmatch(line); matches != nil {
				if output != nil {
					if err = writer.Flush(); err != nil {
						fmt.Printf("Error flushing buffer: %v\n", err)
						os.Exit(1)
						return
					}
					if err = output.Close(); err != nil {
						fmt.Printf("Error closing file: %v\n", err)
						os.Exit(1)
						return
					}
				}

				newFilename = string(matches[1])
				isDataFile = dataFileRegex.MatchString(newFilename)

				outputFile := filepath.Join(outputDir, newFilename)

				output, err = os.OpenFile(outputFile, os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					fmt.Printf("Error opening file: %v\n", err)
					return
				}
				writer = bufio.NewWriterSize(output, bufferSize)

				keep = true
			}
		} else {
			if keep && writer != nil {
				if isDataFile {
					if hasReplacements && fromEntriesContainsRegex.Match(line) {
						replaced := searchreplace.FixLine(&line, replacements)
						_, err = writer.Write(*replaced)
					} else {
						_, err = writer.Write(line)
					}
					if err != nil {
						fmt.Printf("Error writing to buffer: %v\n", err)
						return
					}
				} else {
					_, err = writer.Write(line)
					if err != nil {
						fmt.Printf("Error writing to buffer: %v\n", err)
						return
					}
				}
			}
		}
	}

	if writer != nil {
		if err = writer.Flush(); err != nil {
			fmt.Printf("Error flushing buffer: %v\n", err)
			os.Exit(1)
			return
		}
	}

	if output != nil {
		if err = output.Close(); err != nil {
			fmt.Printf("Error closing file: %v\n", err)
			os.Exit(1)
			return
		}
	}

	fmt.Printf("go-search-replace-mydumper: Finished successfully. took %v\n", time.Since(start))
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
