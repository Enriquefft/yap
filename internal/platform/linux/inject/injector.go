package inject

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hybridz/yap/internal/platform"
	yinject "github.com/hybridz/yap/pkg/yap/inject"
	"github.com/hybridz/yap/pkg/yap/transcribe"
)

// Injector is the Linux implementation of pkg/yap/inject.Injector. It
// owns the strategy registry, the per-call Target detection cache,
// and the slog audit trail emitted on every Inject call.
//
// Construction goes through New, which accepts InjectionOptions, a
// Deps bag, and a logger. The strategy list is built once at
// construction time and walked in priority order on each Inject call:
// tmux → osc52 → electron → wayland → x11.
//
// Concurrency: Inject and InjectStream are serialised through mu so
// concurrent callers cannot race on clipboard state inside the
// electron strategy, nor interleave audit log entries on a single
// session. The daemon serialises by hotkey anyway; the mutex is a
// library-level belt to the daemon's braces.
type Injector struct {
	opts       platform.InjectionOptions
	deps       Deps
	logger     *slog.Logger
	strategies []Strategy

	mu sync.Mutex
}

// finalDeliveryBudget bounds the flush-on-cancel path in
// InjectStream. When the caller cancels their context mid-stream,
// the accumulated text still needs to reach the target — we use a
// fresh context with this budget so a stuck strategy cannot wedge
// the daemon indefinitely.
const finalDeliveryBudget = 3 * time.Second

// ttyReporter is satisfied by strategies (currently only osc52) that
// want to include extra diagnostic context in the audit log after a
// successful delivery. The injector type-asserts the winning strategy
// against this interface in its success path.
type ttyReporter interface {
	LastChosenTTY() string
}

// New constructs a Linux Injector with strategies wired in the
// canonical order. A nil logger is replaced with a discard handler so
// audit calls never panic and tests opt-in to capture by passing a
// real handler.
func New(opts platform.InjectionOptions, deps Deps, logger *slog.Logger) (*Injector, error) {
	if logger == nil {
		logger = slog.New(discardHandler{})
	}
	return &Injector{
		opts:   opts,
		deps:   deps,
		logger: logger,
		strategies: []Strategy{
			newTmuxStrategy(deps),
			newOSC52Strategy(deps, opts),
			newElectronStrategy(deps, opts),
			newWaylandStrategy(deps),
			newX11Strategy(deps),
		},
	}, nil
}

// Inject runs the full pipeline for one delivery: detect the active
// target, walk the prioritized strategy list, and emit a structured
// audit log entry on completion. Returns nil on the first successful
// strategy; returns an aggregate error (via errors.Join) after every
// applicable strategy has failed.
//
// Concurrency: Inject is serialised through i.mu so concurrent
// callers cannot interleave clipboard state or audit lines.
func (i *Injector) Inject(ctx context.Context, text string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.inject(ctx, text)
}

// inject is the lock-free body of Inject. InjectStream calls it
// directly while holding the same mutex.
//
// Transcription backends (whisper.cpp, groq, openai) emit the final
// text with a trailing newline the model uses to mark the end of the
// segment. For clipboard strategies (osc52, tmux, electron) that is
// an ugly-but-harmless extra blank at the end of a paste. For
// keystroke strategies (wayland, x11) it is a correctness bug: wtype
// and ydotool faithfully type the '\n' as an Enter press, which in a
// terminal executes the transcribed line and in a web form submits
// it. Trim trailing whitespace once at the shared entry point so
// every strategy sees the same normalized payload — the rule is
// load-bearing for keystroke targets and cosmetic for everything
// else, and belongs at the dispatch boundary rather than inside each
// strategy so no future strategy can forget it. Leading whitespace is
// preserved because whisper's leading-space convention carries
// context (e.g. continuation after existing cursor text); users who
// want it stripped can post-process via the transform layer.
//
// When the selected app_override carries AppendEnter = true, a single
// trailing "\n" is re-appended after the trim and after strategy
// selection runs. This is the opt-in auto-submit channel: users
// mark terminal or form-submission apps in their config and get
// Enter at the end of every dictation for those apps only. The order
// matters — we trim unconditionally (artifact) and then re-append
// only when the user explicitly asked for it (intent), never because
// whisper happened to include one.
func (i *Injector) inject(ctx context.Context, text string) error {
	text = strings.TrimRight(text, " \t\n\r")
	start := i.now()
	target, detectErr := Detect(ctx, i.deps)
	if detectErr != nil {
		i.logger.WarnContext(ctx, "inject target detection failed",
			"error", detectErr.Error())
		// Fall back to a generic GUI target on whatever display
		// server we can guess so the wayland/x11 strategies still
		// have a chance to deliver.
		target = yinject.Target{
			DisplayServer: detectDisplayServer(i.deps),
			AppType:       yinject.AppGeneric,
		}
		target = annotate(target, i.deps)
	}

	order := buildStrategyOrder(ctx, i.logger, i.strategies, i.opts, target)
	if order.appendEnter {
		text += "\n"
	}
	if len(order.strategies) == 0 {
		i.logOutcome(ctx, slog.LevelError, injectOutcomeFields{
			target:     target,
			outcome:    "failed",
			reason:     order.reason,
			bytes:      len(text),
			durationMS: i.elapsedMillis(start),
		})
		return fmt.Errorf("inject: %s for target %q on %s", order.reason, target.AppClass, target.DisplayServer)
	}

	var (
		attempts    int
		attemptErrs []error
	)
	for _, strat := range order.strategies {
		attempts++
		err := strat.Deliver(ctx, target, text)
		if err == nil {
			fields := injectOutcomeFields{
				target:     target,
				strategy:   strat.Name(),
				outcome:    "success",
				attempts:   attempts,
				bytes:      len(text),
				durationMS: i.elapsedMillis(start),
			}
			if r, ok := strat.(ttyReporter); ok {
				fields.osc52TTY = r.LastChosenTTY()
			}
			i.logOutcome(ctx, slog.LevelInfo, fields)
			return nil
		}
		attemptErrs = append(attemptErrs, fmt.Errorf("%s: %w", strat.Name(), err))
		level := slog.LevelWarn
		if errors.Is(err, yinject.ErrStrategyUnsupported) {
			level = slog.LevelDebug
		}
		i.logger.Log(ctx, level, "inject attempt failed",
			"target.app_class", target.AppClass,
			"strategy", strat.Name(),
			"error", err.Error())
	}

	i.logOutcome(ctx, slog.LevelError, injectOutcomeFields{
		target:     target,
		outcome:    "failed",
		attempts:   attempts,
		bytes:      len(text),
		durationMS: i.elapsedMillis(start),
	})
	return fmt.Errorf("inject: all %d strategies failed for %q on %s: %w",
		attempts, target.AppClass, target.DisplayServer, errors.Join(attemptErrs...))
}

// Resolve runs target detection + classification + strategy selection
// as a pure query and returns the StrategyDecision that the next
// Inject would act on. It does NOT call Deliver on any strategy —
// clipboard, keystroke, and OSC52 tty writes are all suppressed.
//
// Resolve satisfies pkg/yap/inject.StrategyResolver, the optional
// interface debug tooling (`yap paste --dry-run`,
// `yap record --resolve`) uses to surface the routing decision
// without mutating external state. The method is safe to call
// concurrently with Inject — both paths acquire i.mu — and emits no
// audit log entry itself, since the result is already consumed by
// the caller.
//
// Target detection failure is surfaced as an error because the
// caller cannot render a meaningful decision without a classified
// target. Selection failure ("no strategy applies") is reported via
// an empty Strategy in the returned decision, never as an error, so
// the caller can still display the classified Target alongside the
// "no applicable strategies" reason in a single render.
func (i *Injector) Resolve(ctx context.Context) (yinject.StrategyDecision, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	target, err := Detect(ctx, i.deps)
	if err != nil {
		// Surface detection failure directly: the caller needs to
		// distinguish "could not detect target" from "detected but
		// no strategy applies". Fall-through to a generic target
		// (as Inject does) would hide genuine detection bugs in the
		// debug surface.
		return yinject.StrategyDecision{}, fmt.Errorf("resolve: detect target: %w", err)
	}

	order := buildStrategyOrder(ctx, i.logger, i.strategies, i.opts, target)
	decision := yinject.StrategyDecision{
		Target:    target,
		Fallbacks: strategyNames(order.strategies),
		Reason:    order.reason,
	}
	if len(order.strategies) > 0 {
		first := order.strategies[0]
		decision.Strategy = first.Name()
		decision.Tool = toolForStrategy(first.Name(), target)
	}
	return decision, nil
}

// InjectStream consumes chunks until the channel closes, accumulates
// the full text, and delivers it via Inject. Phase 4 implements the
// "buffer then deliver" rule for every target type — Phase 5 will
// refine GUI targets to receive partial chunks. On context
// cancellation we still deliver whatever we have buffered so the user
// is not left with a half-typed sentence, but the flush uses a fresh
// context bounded by finalDeliveryBudget so a wedged strategy cannot
// hold the daemon indefinitely.
func (i *Injector) InjectStream(ctx context.Context, in <-chan transcribe.TranscriptChunk) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	var buf strings.Builder
	for {
		select {
		case chunk, ok := <-in:
			if !ok {
				if buf.Len() == 0 {
					return nil
				}
				return i.inject(ctx, buf.String())
			}
			if chunk.Err != nil {
				return chunk.Err
			}
			buf.WriteString(chunk.Text)
		case <-ctx.Done():
			if buf.Len() > 0 {
				flushCtx, cancel := context.WithTimeout(context.Background(), finalDeliveryBudget)
				defer cancel()
				return i.inject(flushCtx, buf.String())
			}
			return ctx.Err()
		}
	}
}

// now returns the current time via Deps.Now if set, falling back to
// the standard library when the bag is incomplete (which only happens
// in tests that build a partial Deps).
func (i *Injector) now() time.Time {
	if i.deps.Now != nil {
		return i.deps.Now()
	}
	return time.Now()
}

// elapsedMillis returns the elapsed milliseconds since start in a way
// that survives a nil Deps.Now hook.
func (i *Injector) elapsedMillis(start time.Time) int64 {
	return i.now().Sub(start).Milliseconds()
}

// injectOutcomeFields is the canonical field set for every audit log
// line emitted by the Injector. Using a single struct across every
// success/failure code path guarantees consumers (Loki, Grafana, and
// the injector_test capture buffer) can group on the same attribute
// names without missing-key spaghetti.
//
// Zero-valued fields are omitted from the log line through
// logOutcome's conditional append so a log consumer can distinguish
// "not populated" from "populated with empty string".
type injectOutcomeFields struct {
	target     yinject.Target
	strategy   string
	outcome    string
	reason     string
	attempts   int
	bytes      int
	durationMS int64
	osc52TTY   string
}

// logOutcome emits a single "inject" audit line with the canonical
// field names. Every Injector log path routes through here so every
// line has the same attribute shape.
func (i *Injector) logOutcome(ctx context.Context, level slog.Level, f injectOutcomeFields) {
	attrs := []slog.Attr{
		slog.String("target.display_server", f.target.DisplayServer),
		slog.String("target.app_class", f.target.AppClass),
		slog.String("target.app_type", f.target.AppType.String()),
		slog.Bool("target.tmux", f.target.Tmux),
		slog.Bool("target.ssh_remote", f.target.SSHRemote),
		slog.String("outcome", f.outcome),
		slog.Int("bytes", f.bytes),
		slog.Int64("duration_ms", f.durationMS),
	}
	if f.strategy != "" {
		attrs = append(attrs, slog.String("strategy", f.strategy))
	}
	if f.reason != "" {
		attrs = append(attrs, slog.String("reason", f.reason))
	}
	if f.attempts > 0 {
		attrs = append(attrs, slog.Int("attempts", f.attempts))
	}
	if f.osc52TTY != "" {
		attrs = append(attrs, slog.String("target.osc52_pty", f.osc52TTY))
	}
	i.logger.LogAttrs(ctx, level, "inject", attrs...)
}

// discardHandler is a slog.Handler that drops every record. It is the
// production fallback when the caller did not provide a logger.
// slog.DiscardHandler exists in newer Go releases; we ship our own
// here for forward compatibility with the bundled stdlib.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (discardHandler) WithAttrs([]slog.Attr) slog.Handler        { return discardHandler{} }
func (discardHandler) WithGroup(string) slog.Handler             { return discardHandler{} }
