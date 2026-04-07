// Package whisperlocal implements a transcribe.Transcriber backed by a
// long-lived whisper.cpp `whisper-server` subprocess.
//
// The subprocess is started lazily on the first Transcribe call and
// kept alive across subsequent calls so the whisper model is loaded
// exactly once per process. The subprocess is torn down by Close,
// which the daemon calls on shutdown.
//
// Discovery order for the whisper-server binary:
//
//  1. transcribe.Config.WhisperServerPath (set from
//     transcription.whisper_server_path in the user's config)
//  2. $YAP_WHISPER_SERVER
//  3. exec.LookPath("whisper-server")
//  4. /run/current-system/sw/bin/whisper-server (Nix profile fallback)
//
// Discovery order for the model file:
//
//  1. transcribe.Config.ModelPath (transcription.model_path) — must
//     exist on disk; this is the air-gapped escape hatch
//  2. The shared model cache at $XDG_CACHE_HOME/yap/models/, indexed
//     by the model name from transcribe.Config.Model
//
// Importing this package for side effects registers the backend under
// the name "whisperlocal" in the transcribe registry:
//
//	import _ "github.com/hybridz/yap/pkg/yap/transcribe/whisperlocal"
//
// Direct construction is also supported via New for library callers
// that do not want to go through the registry.
package whisperlocal
