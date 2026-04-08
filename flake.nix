{
  description = "yap — hold-to-talk voice dictation daemon for Linux";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        # pkgsStatic exposes a musl-libc package set used for the
        # fully-static build. malgo (the only CGo audio dep) bundles
        # miniaudio as a single C header — no system C audio library
        # is linked into the binary, so the static build only needs a
        # musl libc and -ldl.
        pkgsS = pkgs.pkgsStatic;

        # Shared package definition; withStatic toggles static linker flags.
        yapPkg = { buildGoModule, pkg-config, lib, withStatic ? false }:
          let
            # Single source of truth for the Nix-built version string.
            # Threaded into the Go binary via -ldflags so `yap status`
            # reports the same value the flake declares.
            version = "0.1.0";
          in
          buildGoModule {
            pname = "yap";
            inherit version;
            src = ./.;

            # vendorHash: set to null on first build; replace with sha256 from error output.
            # Example: vendorHash = "sha256-abc123...";
            vendorHash = null;

            # CGO is required for malgo (audio) and whisper.cpp
            # bindings — the only CGo boundaries in yap.
            # Use env attrset to pass environment variables to avoid overlap with derivation args.
            env.CGO_ENABLED = "1";

            # nativeBuildInputs: tools needed at build time (not linked into binary).
            nativeBuildInputs = [ pkg-config ];

            # buildInputs: C libraries linked into the binary. malgo
            # vendors miniaudio.h directly, so no system audio library
            # is required at link time.
            buildInputs = [ ];

            # -s -w: strip debug symbols and DWARF (reduces binary size).
            # -X ...Version=${version}: thread the flake-declared version
            #   into internal/config.Version so `yap status` reports the
            #   same value the package metadata advertises.
            # Static flags only added when withStatic = true.
            ldflags = [
              "-s" "-w"
              "-X" "github.com/hybridz/yap/internal/config.Version=${version}"
            ]
              ++ lib.optionals withStatic [
                "-linkmode external"
                "-extldflags \"-static\""
              ];

            # netgo: pure-Go DNS resolver (avoids glibc dynamic dep via CGo DNS).
            # osusergo: pure-Go user lookup (avoids glibc dynamic dep via CGo os/user).
            # Both required for fully static binary even with musl-gcc.
            tags = lib.optionals withStatic [ "netgo" "osusergo" ];
          };
      in {
        packages = {
          # Dynamic build: for NixOS users who install via nix profile or home-manager.
          default = pkgs.callPackage yapPkg {};

          # Alias for NixOS module reference
          yap = pkgs.callPackage yapPkg {};

          # Static build: for curl install on any Linux distro.
          # Uses pkgsStatic so the toolchain links against musl libc.
          # malgo provides miniaudio inline, so no audio C library needs
          # a static rebuild.
          static = pkgsS.callPackage yapPkg { withStatic = true; };
        };

        # Development shell: provides all tools needed for local development.
        # Usage: nix develop   (or direnv with use flake)
        # Static-only dev shell: provides a musl-gcc wrapper so
        # `make build-static` works from this shell.  Separated from
        # the default dev shell because adding musl to the default
        # shell's NIX_LDFLAGS causes musl+glibc mixing in test binaries
        # which segfaults `go test`.
        # Usage: nix develop .#static
        devShells.static = let
          muslCC = pkgs.pkgsStatic.stdenv.cc;
        in pkgs.mkShell {
          nativeBuildInputs = with pkgs; [
            go
            pkg-config
            (writeShellScriptBin "musl-gcc" ''
              exec ${muslCC}/bin/${muslCC.targetPrefix}gcc "$@"
            '')
          ];
          shellHook = ''
            echo "yap static dev shell — musl toolchain active"
            echo "  make build-static  — static musl build"
          '';
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            pkg-config
            # malgo bundles miniaudio.h directly — no system audio
            # development library is needed in the dev shell.
            # musl intentionally omitted: only used by pkgsStatic for static builds.
            # Including musl here adds -L/musl/lib to NIX_LDFLAGS, causing musl+glibc
            # mixing in test binaries which crashes at startup (segfault on go test).
            ffmpeg
            # whisper-cpp ships the whisper-server subprocess yap launches as
            # the local transcription backend (Phase 6). The yap binary itself
            # does not link against whisper.cpp — discovery is via PATH at
            # runtime, so this is purely a developer convenience.
            whisper-cpp
          ];

          shellHook = ''
            echo "yap dev shell — go $(go version | awk '{print $3}')"
            echo "  make build              — dynamic build"
            echo "  nix build .#static      — static musl build"
            echo "  nix develop .#static    — shell with musl-gcc for make build-static"
          '';
        };
      }) // {
        # NixOS module: closes over self to reference flake packages directly.
        # No overlay needed — the module resolves the package from self.packages.
        nixosModules.default = import ./nixosModules.nix self;
      };
}
