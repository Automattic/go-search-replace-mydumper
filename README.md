# Go Search Replace for mydumper backups

A specialized tool for performing search and replace operations on MySQL backups created with mydumper.

## Overview

This utility extends [github.com/Automattic/go-search-replace](https://github.com/Automattic/go-search-replace) to work specifically with mydumper (stream) backup files.

## Installation

### Pre-built binaries

Download the appropriate binary for your system from the releases page.

### Building from source

To build from source, you need to have Go installed. Clone the repository and run:

```bash
make build
```

## Usage

```
go-search-replace-mydumper <input file> <output dir> <from> <to> ...
```
