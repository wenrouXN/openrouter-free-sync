PLUGIN_ID = openrouter-free-sync
VERSION = 0.3.0
GOFLAGS = -trimpath -buildvcs=false

.PHONY: build clean test

build:
	go build $(GOFLAGS) -buildmode=c-shared -o $(PLUGIN_ID).so .

clean:
	rm -f $(PLUGIN_ID).so $(PLUGIN_ID).h

test:
	go test ./... -v

# Docker-based build (host has no Go installed)
build-docker:
	docker run --rm -v "$$(pwd):/src" -w /src golang:1.24-bookworm bash -c \
		"apt-get update -qq && apt-get install -y -qq gcc >/dev/null && CGO_ENABLED=1 go build -trimpath -buildvcs=false -buildmode=c-shared -o $(PLUGIN_ID).so ."
