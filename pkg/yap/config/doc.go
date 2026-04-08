// Package config is the single source of truth for yap's configuration
// schema. The TOML file format, the NixOS module, the first-run wizard,
// and the validation logic are all generated from or validated against
// the types in this package.
//
// No other package may declare config types. Field renames and schema
// changes belong here first; every downstream surface is derived.
//
// The `yap:"..."` struct tag carries metadata consumed by the NixOS
// generator (enum values, numeric ranges, documentation strings). Its
// grammar is `key=value;key=value;flag`. Recognized keys:
//
//	enum=v1,v2,v3  — allowed string values (comma-separated)
//	min=N          — minimum numeric value (int or float)
//	max=N          — maximum numeric value
//	gt=N           — strict numeric lower bound (exclusive)
//	doc=TEXT       — documentation string rendered into the NixOS module
//	secret         — flag: mark field as containing a secret (API key etc.)
//
// The validator in validate.go enforces enum/min/max/gt. The NixOS
// generator in internal/cmd/gen-nixos consumes them to emit types
// and descriptions.
//
// Run `go generate ./pkg/yap/config/...` after any schema change to
// regenerate nixosModules.nix and homeManagerModules.nix. CI enforces
// that the committed files match generator output byte-for-byte.
package config

//go:generate go run ../../../internal/cmd/gen-nixos -o-nixos ../../../nixosModules.nix -o-hm ../../../homeManagerModules.nix
