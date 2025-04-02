BINARY = go-search-replace-mydumper
BUILDDIR = ./bin

all: vet fmt test build

ci: clean vet test

build: clean
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -o "${BUILDDIR}/${BINARY}_linux_amd64"       go-search-replace-mydumper.go
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -o "${BUILDDIR}/${BINARY}_linux_arm64"       go-search-replace-mydumper.go
	GOOS=linux   GOARCH=386   CGO_ENABLED=0 go build -o "${BUILDDIR}/${BINARY}_linux_386"         go-search-replace-mydumper.go
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o "${BUILDDIR}/${BINARY}_windows_amd64.exe" go-search-replace-mydumper.go
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build -o "${BUILDDIR}/${BINARY}_windows_arm64.exe" go-search-replace-mydumper.go
	GOOS=windows GOARCH=386   CGO_ENABLED=0 go build -o "${BUILDDIR}/${BINARY}_windows_386.exe"   go-search-replace-mydumper.go
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build -o "${BUILDDIR}/${BINARY}_darwin_amd64"      go-search-replace-mydumper.go
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -o "${BUILDDIR}/${BINARY}_darwin_arm64"      go-search-replace-mydumper.go

vet:
	go vet ./...

fmt:
	gofmt -s -l . | grep -v vendor | tee /dev/stderr

test:
	go test -v ./...
	go test -bench .

bench:
	go test -bench .

clean:
	rm -rf ${BUILDDIR}

.PHONY: all ci clean vet fmt test build
