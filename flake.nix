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
        # pkgsStatic compiles all C dependencies (including portaudio) against musl.
        # This handles transitive dep rebuilds automatically — no manual -extldflags juggling.
        pkgsS = pkgs.pkgsStatic;

        # Shared package definition; withStatic toggles static linker flags.
        yapPkg = { buildGoModule, pkg-config, portaudio, lib, withStatic ? false }:
          buildGoModule {
            pname = "yap";
            version = "0.1.0";
            src = ./.;

            # vendorHash: set to null on first build; replace with sha256 from error output.
            # Example: vendorHash = "sha256-abc123...";
            vendorHash = null;

            # CGO is required for portaudio (sole CGo boundary in yap).
            # Use env attrset to pass environment variables to avoid overlap with derivation args.
            env.CGO_ENABLED = "1";

            # nativeBuildInputs: tools needed at build time (not linked into binary).
            # pkg-config is required for CGo to find portaudio headers in Nix sandbox.
            nativeBuildInputs = [ pkg-config ];

            # buildInputs: C libraries linked into the binary.
            buildInputs = [ portaudio ];

            # -s -w: strip debug symbols and DWARF (reduces binary size).
            # Static flags only added when withStatic = true.
            ldflags = [ "-s" "-w" ]
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
          # Uses pkgsStatic which compiles portaudio against musl automatically.
          static = pkgsS.callPackage yapPkg { withStatic = true; };
        };

        # Development shell: provides all tools needed for local development.
        # Usage: nix develop   (or direnv with use flake)
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            pkg-config
            portaudio
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
            echo "  make build         — dynamic build"
            echo "  make build-static  — static musl build"
            echo "  make build-check   — static build + ldd verify"
          '';
        };
      }) // {
        # NixOS module: closes over self to reference flake packages directly.
        # No overlay needed — the module resolves the package from self.packages.
        nixosModules.default = import ./nixosModules.nix self;
      };
}
