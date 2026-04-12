package termscroll

import (
	"context"
	"log/slog"

	"github.com/Enriquefft/yap/pkg/yap/hint"
	"github.com/Enriquefft/yap/pkg/yap/inject"
)

const providerName = "termscroll"

// provider walks terminal scrollback strategies in priority order.
type provider struct {
	strategies []Strategy
	stripper   *ansiStripper
}

// NewFactory returns a hint.Factory that constructs termscroll providers.
func NewFactory(_ hint.Config) (hint.Provider, error) {
	return &provider{
		strategies: []Strategy{
			newKittyStrategy(),
			// Phase 12.5: tmux, wezterm, ghostty strategies.
		},
		stripper: newANSIStripper(),
	}, nil
}

// newProvider constructs a provider with explicit strategies (for testing).
func newProvider(strategies []Strategy) *provider {
	return &provider{
		strategies: strategies,
		stripper:   newANSIStripper(),
	}
}

func (p *provider) Name() string { return providerName }

func (p *provider) Supports(target inject.Target) bool {
	return target.AppType == inject.AppTerminal
}

func (p *provider) Fetch(ctx context.Context, target inject.Target) (hint.Bundle, error) {
	for _, s := range p.strategies {
		if !s.Supports(target) {
			continue
		}
		text, err := s.Read(ctx)
		if err != nil {
			slog.Debug("termscroll: strategy error",
				"strategy", s.Name(), "error", err)
			continue
		}
		if text == "" {
			continue
		}
		stripped := p.stripper.Strip(text)
		if stripped == "" {
			continue
		}
		return hint.Bundle{
			Conversation: stripped,
			Source:       providerName,
		}, nil
	}
	return hint.Bundle{}, nil
}
