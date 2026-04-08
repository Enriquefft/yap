// Package fallback provides a transform.Transformer decorator that
// runs a primary transformer and, on any primary failure, replays the
// original input through a secondary transformer. Graceful
// degradation for the Phase 8 LLM transform pipeline is wired through
// this decorator: the daemon supplies the configured backend as the
// primary and the passthrough transformer as the fallback so a
// backend outage never costs the user their dictation.
//
// # Buffered, atomic delivery
//
// fallback.Transformer buffers the primary's output and delivers it
// atomically — no chunk reaches the consumer until the primary stream
// has terminated cleanly. This is what makes "replay through the
// fallback on failure" sound: a half-emitted primary cannot leak
// partial transformed output before the decorator has decided whether
// to commit or roll back.
//
// The trade-off is real: the decorator turns a streaming primary into
// a batch primary. Callers that want both streaming partials AND
// graceful fallback have to pick one. The recommended escape hatch
// is daemon-side: when the user has stream_partials = true the
// daemon should NOT wrap the primary in this decorator — it should
// use the primary directly and accept that a mid-stream primary
// failure surfaces as a partial-output-plus-error. When
// stream_partials = false (the atomic-delivery case), the fallback
// decorator is the correct wrapper.
//
// # Semantics
//
//   - The full input channel is drained into an in-memory slice
//     before either transformer runs. This lets us replay the same
//     chunks through the fallback if the primary fails. For the
//     dictation use case the transcript is small (a few hundred
//     bytes to a few kB) so the buffer cost is negligible.
//
//   - If an upstream chunk carries an error (chunk.Err != nil) the
//     error is propagated directly, without running either
//     transformer. Transcription failures are not something the
//     transform fallback should paper over.
//
//   - If the primary's Transform call returns an error (factory-time
//     failure, e.g. invalid config), OnError is called once and the
//     fallback takes over immediately.
//
//   - If the primary emits any chunk with Err != nil mid-stream,
//     OnError is called once and the fallback replays the buffered
//     input. Partial success is treated as failure — we never mix
//     transformed and raw output.
//
//   - If ctx is cancelled while the primary is running, ctx.Err is
//     returned and the fallback is not invoked. Cancellation is the
//     user's explicit stop, not a backend failure.
//
// OnError is called at most once per decorator invocation.
package fallback
