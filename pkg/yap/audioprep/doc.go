// Package audioprep provides safe audio preprocessing for voice-to-text
// pipelines. It sits between the recorder's WAV output and the Whisper
// transcriber, applying two scientifically validated preprocessing steps:
//
//   - High-pass biquad filter (~80Hz) to remove sub-speech rumble (HVAC,
//     desk vibration, handling noise). Speech fundamentals start at ~85Hz
//     so this filter cannot degrade speech content.
//
//   - Leading/trailing silence trimming to prevent Whisper hallucinations
//     on non-speech segments. Uses windowed RMS amplitude detection.
//
// Research (arXiv:2512.17562) shows that noise reduction preprocessing
// degrades Whisper transcription accuracy by up to 46.6%. These two
// operations are the only preprocessing steps that are empirically safe:
// the high-pass filter removes frequencies below the speech range, and
// silence trimming removes non-speech segments entirely.
//
// The [Processor] type satisfies the engine.AudioProcessor interface
// implicitly — this package does not import the engine package.
package audioprep
