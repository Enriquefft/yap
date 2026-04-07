package inject

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
type Injector struct {
	opts       platform.InjectionOptions
	deps       Deps
	logger     *slog.Logger
	strategies []Strategy
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
			newTmuxStrategy(deps, opts),
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
// strategy; returns an aggregate error after every applicable
// strategy has failed.
func (i *Injector) Inject(ctx context.Context, text string) error {
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

	order := selectStrategies(i.strategies, i.opts, target)
	if len(order) == 0 {
		i.logger.ErrorContext(ctx, "inject",
			"target.display_server", target.DisplayServer,
			"target.app_class", target.AppClass,
			"target.app_type", target.AppType.String(),
			"target.tmux", target.Tmux,
			"target.ssh_remote", target.SSHRemote,
			"outcome", "failed",
			"reason", "no applicable strategies",
			"bytes", len(text),
			"duration_ms", i.elapsedMillis(start))
		return fmt.Errorf("inject: no applicable strategies for target %q on %s", target.AppClass, target.DisplayServer)
	}

	var attempts int
	for _, strat := range order {
		attempts++
		err := strat.Deliver(ctx, target, text)
		if err == nil {
			i.logger.InfoContext(ctx, "inject",
				"target.display_server", target.DisplayServer,
				"target.app_class", target.AppClass,
				"target.app_type", target.AppType.String(),
				"target.tmux", target.Tmux,
				"target.ssh_remote", target.SSHRemote,
				"strategy", strat.Name(),
				"outcome", "success",
				"attempts", attempts,
				"bytes", len(text),
				"duration_ms", i.elapsedMillis(start))
			return nil
		}
		level := slog.LevelWarn
		if errors.Is(err, yinject.ErrStrategyUnsupported) {
			level = slog.LevelDebug
		}
		i.logger.Log(ctx, level, "inject attempt failed",
			"target.app_class", target.AppClass,
			"strategy", strat.Name(),
			"error", err.Error())
	}

	i.logger.ErrorContext(ctx, "inject",
		"target.display_server", target.DisplayServer,
		"target.app_class", target.AppClass,
		"target.app_type", target.AppType.String(),
		"target.tmux", target.Tmux,
		"target.ssh_remote", target.SSHRemote,
		"outcome", "failed",
		"attempts", attempts,
		"bytes", len(text),
		"duration_ms", i.elapsedMillis(start))
	return fmt.Errorf("inject: all %d strategies failed for %q on %s", attempts, target.AppClass, target.DisplayServer)
}

// InjectStream consumes chunks until the channel closes, accumulates
// the full text, and delivers it via Inject. Phase 4 implements the
// "buffer then deliver" rule for every target type — Phase 5 will
// refine GUI targets to receive partial chunks. On context
// cancellation we still deliver whatever we have buffered so the user
// is not left with a half-typed sentence.
func (i *Injector) InjectStream(ctx context.Context, in <-chan transcribe.TranscriptChunk) error {
	var buf strings.Builder
	for {
		select {
		case chunk, ok := <-in:
			if !ok {
				if buf.Len() == 0 {
					return nil
				}
				return i.Inject(ctx, buf.String())
			}
			if chunk.Err != nil {
				return chunk.Err
			}
			buf.WriteString(chunk.Text)
		case <-ctx.Done():
			if buf.Len() > 0 {
				return i.Inject(context.Background(), buf.String())
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

// discardHandler is a slog.Handler that drops every record. It is the
// production fallback when the caller did not provide a logger.
// slog.DiscardHandler exists in newer Go releases; we ship our own
// here for forward compatibility with the bundled stdlib.
type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (discardHandler) WithAttrs([]slog.Attr) slog.Handler        { return discardHandler{} }
func (discardHandler) WithGroup(string) slog.Handler             { return discardHandler{} }
