BINARY  := ccmeter
VERSION := $(shell sed -n 's/^var version = "\(.*\)"$$/\1/p' main.go)
LDFLAGS := -s -w
GOFMT   ?= $(shell go env GOROOT)/bin/gofmt
PREFIX  ?= /usr/local
BIN     := build/bin

# os/arch pairs for `make bundle`. Darwin binaries are named "macos".
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: all bundle build test check fmt vet validate install uninstall serve clean help

all: bundle

help:
	@echo "make            same as make bundle"
	@echo "make bundle     cross-compile $(VERSION) for every platform into $(BIN)/, with SHA256SUMS"
	@echo "make build      build $(BIN)/$(BINARY) for this machine"
	@echo "make test       go test ./..."
	@echo "make check      gofmt, vet (linux, darwin, windows), tests, plugin validation"
	@echo "make fmt        gofmt -w ."
	@echo "make install    copy $(BIN)/$(BINARY) to $(PREFIX)/bin; run make build first (PREFIX=~/.local for no sudo)"
	@echo "make uninstall  remove $(PREFIX)/bin/$(BINARY)"
	@echo "make serve      build and run the dashboard on http://127.0.0.1:7878"
	@echo "make clean      remove build/"

bundle:
	@mkdir -p $(BIN)
	@set -e; for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		name=$$os; [ $$os = darwin ] && name=macos; \
		ext=; [ $$os = windows ] && ext=.exe; \
		out=$(BIN)/$(BINARY)-$$name-$$arch$$ext; \
		echo "$$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags="$(LDFLAGS)" -o $$out .; \
	done
	@cd $(BIN) && if command -v sha256sum >/dev/null 2>&1; then \
		sha256sum $(BINARY)-* > SHA256SUMS; \
	else \
		shasum -a 256 $(BINARY)-* > SHA256SUMS; \
	fi
	@echo "$(BIN)/SHA256SUMS"

build:
	@mkdir -p $(BIN)
	go build -trimpath -o $(BIN)/$(BINARY) .

test:
	go test ./...

fmt:
	$(GOFMT) -w .

vet:
	go vet ./...
	GOOS=darwin go vet ./...
	GOOS=windows go vet ./...

# The checks to run before every commit.
check: vet test validate
	@out=$$($(GOFMT) -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# Skipped when the claude CLI isn't installed.
validate:
	@if command -v claude >/dev/null 2>&1; then \
		claude plugin validate . && claude plugin validate ./plugin; \
	else \
		echo "claude not found, skipping plugin validation"; \
	fi

# Doesn't build: sudo resets PATH, so go usually isn't found under it.
# Run `make build` first, then `sudo make install`.
install:
	@if [ ! -f $(BIN)/$(BINARY) ]; then \
		echo "$(BIN)/$(BINARY) not found. Run 'make build' first (without sudo), then 'sudo make install'."; \
		exit 1; \
	fi
	@if [ -n "$$(find . -path ./build -prune -o \( -name '*.go' -o -path './web/*' \) -newer $(BIN)/$(BINARY) -print | head -1)" ]; then \
		echo "warning: $(BIN)/$(BINARY) is older than the source; run 'make build' to update it"; \
	fi
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 755 $(BIN)/$(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)

uninstall:
	rm -f $(DESTDIR)$(PREFIX)/bin/$(BINARY)

serve: build
	./$(BIN)/$(BINARY) serve

clean:
	rm -rf build
