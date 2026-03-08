BINARY := yap
CMD    := ./cmd/yap
# -s -w: strip debug info. Required for NFR-05 (< 20MB).
# -linkmode external -extldflags '-static': produce static binary (NFR-01, NFR-02).
LDFLAGS := -s -w -linkmode external -extldflags '-static'
TAGS    := netgo,osusergo
MAX_SIZE_MB := 20

.PHONY: build build-static verify-static size-check build-check test clean

build:
	go build -o $(BINARY) $(CMD)

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
	  -ldflags="$(LDFLAGS)" \
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
