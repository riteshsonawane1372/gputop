# gputop developer tasks. CI runs the same commands.
GO       ?= go
PKG      := github.com/gputop/gputop
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(PKG)/internal/buildinfo.Version=$(VERSION)
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: all build test race lint fmt vet bench cross demo test-nvidia clean

all: fmt vet lint test build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/gputop ./cmd/gputop

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

# Requires a Linux host with an NVIDIA driver; skips otherwise.
test-nvidia:
	$(GO) test -tags integration -run Integration -v ./internal/gpu/nvidia/

# Lint for both target OSes: build-tagged files (e.g. the Linux NVML
# binding) are only analyzed for their own GOOS.
lint:
	GOOS=linux golangci-lint run ./...
	GOOS=darwin golangci-lint run ./...

fmt:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "run: gofmt -w ." && exit 1)

vet:
	$(GO) vet ./...
	GOOS=linux $(GO) vet ./...

bench:
	$(GO) test -run '^$$' -bench . -benchmem ./internal/collector ./internal/history ./internal/tui

cross:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/gputop-$$os-$$arch ./cmd/gputop || exit 1; \
	done

demo: build
	./bin/gputop --demo

clean:
	rm -rf bin dist
