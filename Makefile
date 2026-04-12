BINARY := yap
CMD    := ./cmd/yap

# VERSION is computed from `git describe` so release builds report a
# meaningful version string in `yap status`. Falls back to "dev" when
# git metadata is unavailable (e.g. tarball builds, sandboxed CI).
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

# VERSION_LDFLAG injects the value into internal/config.Version via the
# Go linker. internal/config/version.go is the single source of truth
# for the default ("dev"); release builds override it here.
VERSION_LDFLAG := -X github.com/Enriquefft/yap/internal/config.Version=$(VERSION)

# -s -w: strip debug info. Required for NFR-05 (< 20MB).
# -linkmode external -extldflags '-static': produce static binary (NFR-01, NFR-02).
LDFLAGS_COMMON := -s -w $(VERSION_LDFLAG)
LDFLAGS_STATIC := $(LDFLAGS_COMMON) -linkmode external -extldflags '-static'
TAGS    := netgo,osusergo
MAX_SIZE_MB := 20

.PHONY: build build-static verify-static size-check build-check test clean

build:
	go build -ldflags "$(LDFLAGS_COMMON)" -o $(BINARY) $(CMD)

build-static:
	@which musl-gcc > /dev/null 2>&1 || \
	  (echo "ERROR: musl-gcc not found." && \
	   echo "  Debian/Ubuntu: sudo apt-get install musl-tools" && \
	   echo "  Arch: sudo pacman -S musl" && \
	   echo "  NixOS: nix develop (devShell includes musl)" && \
	   exit 1)
	CGO_ENABLED=1 CC=musl-gcc \
	go build \
	  -tags $(TAGS) \
	  -ldflags="$(LDFLAGS_STATIC)" \
	  -o $(BINARY) $(CMD)

verify-static:
	@ldd ./$(BINARY) 2>&1 | grep -q "not a dynamic executable" && \
	  echo "OK: $(BINARY) is statically linked" || \
	  (echo "FAIL: $(BINARY) has dynamic dependencies:" && ldd ./$(BINARY) && exit 1)

size-check:
	@SIZE=$$(du -b ./$(BINARY) | cut -f1); \
	 MAX_BYTES=$$(($(MAX_SIZE_MB) * 1024 * 1024)); \
	 SIZE_MB=$$((SIZE / 1048576)); \
	 echo "Binary size: $$SIZE bytes (~$$SIZE_MB MB)"; \
	 if [ "$$SIZE" -lt "$$MAX_BYTES" ]; then \
	   echo "OK: binary under $(MAX_SIZE_MB)MB limit"; \
	 else \
	   echo "FAIL: binary $$SIZE bytes exceeds $(MAX_SIZE_MB)MB limit ($$MAX_BYTES bytes)"; \
	   exit 1; \
	 fi

# Full verification gate: static build + ldd check + size check.
# All must pass before Phase 2 begins.
build-check: build-static verify-static size-check

test:
	go test ./...

clean:
	rm -f $(BINARY)
