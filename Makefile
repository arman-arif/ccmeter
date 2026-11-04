BINARY  := ccmeter
VERSION := $(shell sed -n 's/^var version = "\(.*\)"$$/\1/p' src/version.go)
LDFLAGS := -s -w
GOFMT   ?= $(shell go env GOROOT)/bin/gofmt
PREFIX  ?= /usr/local
BIN     := build/bin

# os/arch pairs for `make bundle`. Darwin binaries are named "macos".
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.PHONY: all bundle build web web-check test check fmt vet validate install uninstall serve clean help

all: bundle

help:
	@echo "make            same as make bundle"
	@echo "make bundle     cross-compile $(VERSION) for every platform into $(BIN)/, with SHA256SUMS"
	@echo "make build      build $(BIN)/$(BINARY) for this machine"
	@echo "make web        rebuild the dashboard from frontend/ into src/web/ (needs Node; commit the result)"
	@echo "make test       go test ./..."
	@echo "make check      gofmt, vet (linux, darwin, windows), tests, plugin validation, web build up to date"
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

# Go embeds src/web and doesn't run npm, so src/web is committed: `go build`
# and `go install` need no Node. Rebuild it after changing frontend/.
web: frontend/node_modules
	cd frontend && npm run build

frontend/node_modules: frontend/package-lock.json
	cd frontend && npm ci
	@touch $@

# Fails if src/web isn't what frontend/ builds to. Skipped when frontend's
# packages aren't installed, so Go-only work doesn't need Node.
web-check:
	@if [ -d frontend/node_modules ]; then \
		rm -rf build/web-check; \
		(cd frontend && npx tsc --noEmit && npx vite build --logLevel warn --outDir ../build/web-check --emptyOutDir) && \
		diff -r src/web build/web-check >/dev/null || { echo "src/web is out of date: run make web and commit it"; exit 1; }; \
	else \
		echo "frontend/node_modules not found, skipping the web build check"; \
	fi

test:
	go test ./...

fmt:
	$(GOFMT) -w .

vet:
	go vet ./...
	GOOS=darwin go vet ./...
	GOOS=windows go vet ./...

# The checks to run before every commit.
check: vet test validate web-check
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
	@if [ -n "$$(find . \( -path ./build -o -path ./frontend \) -prune -o \( -name '*.go' -o -path './src/web/*' \) -newer $(BIN)/$(BINARY) -print | head -1)" ]; then \
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
	rm -rf frontend/node_modules
