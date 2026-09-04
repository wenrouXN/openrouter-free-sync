PLUGIN_ID = openrouter-free-sync
VERSION = 0.1.0
GOFLAGS = -trimpath -buildmode=c-shared

.PHONY: build clean test all

all: build

build:
	go build $(GOFLAGS) -o $(PLUGIN_ID).so .

clean:
	rm -f $(PLUGIN_ID).so $(PLUGIN_ID).h

test:
	go test ./... -v

# Cross-compile helpers (requires GOOS/GOARCH env)
build-all: clean
	GOOS=linux   GOARCH=amd64 go build $(GOFLAGS) -o dist/$(PLUGIN_ID)_$(VERSION)_linux_amd64.so .
	GOOS=linux   GOARCH=arm64 go build $(GOFLAGS) -o dist/$(PLUGIN_ID)_$(VERSION)_linux_arm64.so .
	GOOS=darwin  GOARCH=amd64 go build $(GOFLAGS) -o dist/$(PLUGIN_ID)_$(VERSION)_darwin_amd64.dylib .
	GOOS=darwin  GOARCH=arm64 go build $(GOFLAGS) -o dist/$(PLUGIN_ID)_$(VERSION)_darwin_arm64.dylib .
	GOOS=windows GOARCH=amd64 go build $(GOFLAGS) -o dist/$(PLUGIN_ID)_$(VERSION)_windows_amd64.dll .
	GOOS=windows GOARCH=arm64 go build $(GOFLAGS) -o dist/$(PLUGIN_ID)_$(VERSION)_windows_arm64.dll .
	GOOS=freebsd GOARCH=amd64 go build $(GOFLAGS) -o dist/$(PLUGIN_ID)_$(VERSION)_freebsd_amd64.so .
	cd dist && sha256sum * > checksums.txt
