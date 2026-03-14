package bot

import (
	"context"
	"log/slog"

	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/event"
)

// StoreLootEventHandler handles events for the StoreLoot cross-account mule system.
// Farmers receive game info from the StoreLoot mule via events.
type StoreLootEventHandler struct {
	supervisor string
	log        *slog.Logger
	cfg        *config.CharacterCfg
}

func NewStoreLootEventHandler(supervisor string, log *slog.Logger, cfg *config.CharacterCfg) *StoreLootEventHandler {
	return &StoreLootEventHandler{
		supervisor: supervisor,
		log:        log,
		cfg:        cfg,
	}
}

func (h *StoreLootEventHandler) Handle(ctx context.Context, e event.Event) error {
	// Only farmers (non-mule) with StoreLoot enabled should process these events.
	if !h.cfg.StoreLoot.Enabled || h.cfg.StoreLoot.IsMule {
		return nil
	}

	switch evt := e.(type) {
	case event.RequestStoreLootJoinGameEvent:
		// If a specific mule name is configured, only accept events from that mule.
		if h.cfg.StoreLoot.MuleName != "" && evt.MuleSupervisor != h.cfg.StoreLoot.MuleName {
			return nil
		}
		h.log.Info("StoreLoot game info received",
			slog.String("supervisor", h.supervisor),
			slog.String("mule", evt.MuleSupervisor),
			slog.String("game", evt.Name))
		h.cfg.StoreLoot.StoreLootGameName = evt.Name
		h.cfg.StoreLoot.StoreLootGamePassword = evt.Password

	case event.ResetStoreLootGameInfoEvent:
		if h.cfg.StoreLoot.MuleName != "" && evt.MuleSupervisor != h.cfg.StoreLoot.MuleName {
			return nil
		}
		h.log.Info("StoreLoot game info cleared",
			slog.String("supervisor", h.supervisor),
			slog.String("mule", evt.MuleSupervisor))
		h.cfg.StoreLoot.StoreLootGameName = ""
		h.cfg.StoreLoot.StoreLootGamePassword = ""
	}

	return nil
}
