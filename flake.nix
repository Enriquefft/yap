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

        # Dynamic build for NixOS consumers. Static curl-install
        # binaries are produced via `nix develop .#static --command
        # make build-static`, which routes through the Makefile — the
        # single source of truth for release build flags.
        yapPkg = { buildGoModule, pkg-config }:
          let
            version = "0.1.0";
          in
          buildGoModule {
            pname = "yap";
            inherit version;
            src = ./.;

            vendorHash = null;

            env.CGO_ENABLED = "1";

            nativeBuildInputs = [ pkg-config ];
            buildInputs = [ ];

            ldflags = [
              "-s" "-w"
              "-X" "github.com/Enriquefft/yap/internal/config.Version=${version}"
            ];
          };
      in {
        packages = {
          default = pkgs.callPackage yapPkg {};
          yap = pkgs.callPackage yapPkg {};
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
            echo "  make build                                        — dynamic build"
            echo "  nix develop .#static --command make build-static  — static musl build"
          '';
        };
      }) // {
        # NixOS module: closes over self to reference flake packages directly.
        # No overlay needed — the module resolves the package from self.packages.
        nixosModules.default = import ./nixosModules.nix self;

        # Home-manager module: provides programs.yap with optional systemd
        # user service. Users who manage keybinds externally can set
        # programs.yap.daemon.enable = false and use yap record/toggle
        # from their own keybind setup.
        homeManagerModules.default = import ./homeManagerModules.nix self;
      };
}
