VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build build-all install uninstall test clean checksums release

build:
	go build -ldflags "$(LDFLAGS)" -o bin/trae-proxy ./cmd/trae-proxy

build-all:
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o bin/trae-proxy-darwin-arm64       ./cmd/trae-proxy
	GOOS=darwin  GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/trae-proxy-darwin-amd64       ./cmd/trae-proxy
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/trae-proxy-linux-amd64        ./cmd/trae-proxy
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/trae-proxy-windows-amd64.exe  ./cmd/trae-proxy

checksums: build-all
	cd bin && shasum -a 256 trae-proxy-* > checksums.txt

release: checksums
	@if [ "$(VERSION)" = "dev" ]; then echo "Error: 请先打 tag (git tag vX.Y.Z)"; exit 1; fi
	@echo "Creating GitHub release $(VERSION)..."
	gh release create $(VERSION) \
		bin/trae-proxy-darwin-arm64 \
		bin/trae-proxy-darwin-amd64 \
		bin/trae-proxy-linux-amd64 \
		bin/trae-proxy-windows-amd64.exe \
		bin/checksums.txt \
		--title "$(VERSION)" \
		--generate-notes

install: build
	sudo cp bin/trae-proxy /usr/local/bin/trae-proxy
	@echo "installed to /usr/local/bin/trae-proxy"

uninstall:
	sudo rm -f /usr/local/bin/trae-proxy
	@echo "removed /usr/local/bin/trae-proxy"

test:
	go test -v -race ./...

clean:
	rm -rf bin/
