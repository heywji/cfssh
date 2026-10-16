.PHONY: build test run

build:
	mkdir -p dist
	CGO_ENABLED=1 go build -o dist/cfssh ./cmd/cfssh

test:
	go test ./...

run: build
	./dist/cfssh

# Cross-compile from macOS or Linux. Needs mingw-w64 (brew install mingw-w64).
build-windows:
	mkdir -p dist
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
		go build -ldflags "-H windowsgui" -o dist/cfssh.exe ./cmd/cfssh
