.PHONY: build test run build-macos-app build-windows winres

build:
	mkdir -p dist
	CGO_ENABLED=1 go build -o dist/cfssh ./cmd/cfssh

build-macos-app: build
	rm -rf "dist/CF SSH.app"
	mkdir -p "dist/CF SSH.app/Contents/MacOS"
	cp dist/cfssh "dist/CF SSH.app/Contents/MacOS/cfssh"
	cp packaging/Info.plist "dist/CF SSH.app/Contents/Info.plist"
	codesign --force --sign - --identifier dev.cfssh.app "dist/CF SSH.app"

test:
	go test ./...

run: build
	./dist/cfssh

# Cross-compile the Windows build from macOS. Needs mingw-w64 (brew install mingw-w64).
build-windows:
	mkdir -p dist
	CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
		go build -ldflags "-H windowsgui" -o dist/cfssh.exe ./cmd/cfssh

# Regenerate the Windows resource (manifest: common controls v6, per-monitor DPI).
winres:
	cd cmd/cfssh && x86_64-w64-mingw32-windres cfssh.rc -O coff -o rsrc_windows_amd64.syso
