BINARY := yap
CMD    := ./cmd/yap
LDFLAGS := -s -w -linkmode external -extldflags '-static'
TAGS    := netgo,osusergo

.PHONY: build build-static verify-static build-check test

build:
	go build -o $(BINARY) $(CMD)

build-static:
	@which musl-gcc > /dev/null 2>&1 || (echo "ERROR: musl-gcc not found. Install: apt-get install musl-tools (Debian/Ubuntu), pacman -S musl (Arch), or use Nix devShell." && exit 1)
	CGO_ENABLED=1 CC=musl-gcc \
	go build \
	  -tags $(TAGS) \
	  -ldflags="$(LDFLAGS)" \
	  -o $(BINARY) $(CMD)

verify-static:
	@ldd ./$(BINARY) 2>&1 | grep -q "not a dynamic executable" && \
	  echo "OK: binary is static" || \
	  (echo "FAIL: binary has dynamic deps:" && ldd ./$(BINARY) && exit 1)

build-check: build-static verify-static

test:
	go test ./...
