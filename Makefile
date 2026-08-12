TARGETS := darwin-arm64 linux-amd64 windows-amd64
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build clean

build:
	@for t in $(TARGETS); do \
		os=$$(echo $$t | cut -d- -f1); \
		arch=$$(echo $$t | cut -d- -f2); \
		bin=dist/larkctl-$$t; \
		if [ "$$os" = "windows" ]; then bin=$$bin.exe; fi; \
		echo "==> build $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags='-s -w -X main.version=$(VERSION)' -o $$bin .; \
	done

clean:
	rm -rf dist/
