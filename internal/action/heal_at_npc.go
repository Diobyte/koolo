package action

import (
	"fmt"
	"log/slog"

	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/town"
)

func HealAtNPC() error {
	ctx := context.Get()
	ctx.SetLastAction("HealAtNPC")

	shouldHeal := false
	hpPct := ctx.Data.PlayerUnit.HPPercent()
	if hpPct < 0 || hpPct > 100 {
		// Data not ready (e.g. NaN conversion during area transition), skip healing
		return step.CloseAllMenus()
	}
	if hpPct < 80 {
		ctx.Logger.Info(fmt.Sprintf("Current life is %d, healing on NPC", hpPct))
		shouldHeal = true
	}

	if ctx.Data.PlayerUnit.HasDebuff() {
		ctx.Logger.Info("Debuff detected, healing on NPC")
		shouldHeal = true
	}

	if shouldHeal {
		err := InteractNPC(town.GetTownByArea(ctx.Data.PlayerUnit.Area).HealNPC())
		if err != nil {
			ctx.Logger.Warn("Failed to heal on NPC", slog.Any("error", err))
		}
	}

	return step.CloseAllMenus()
}
