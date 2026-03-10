package run

import (
	"errors"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/object"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/utils"
)

type ViperTemple struct {
	ctx *context.Status
}

func NewViperTemple() *ViperTemple {
	return &ViperTemple{
		ctx: context.Get(),
	}
}

func (v ViperTemple) Name() string {
	return string(config.ViperTempleRun)
}

func (v ViperTemple) CheckConditions(parameters *RunParameters) SequencerResult {
	return SequencerOk
}

func (v ViperTemple) Run(parameters *RunParameters) error {
	v.ctx.Logger.Info("Starting Claw Viper Temple run")

	err := action.WayPoint(area.LostCity)
	if err != nil {
		return err
	}

	err = action.MoveToArea(area.ValleyOfSnakes)
	if err != nil {
		return err
	}

	err = action.MoveToArea(area.ClawViperTempleLevel1)
	if err != nil {
		return err
	}

	err = action.MoveToArea(area.ClawViperTempleLevel2)
	if err != nil {
		return err
	}

	err = action.MoveTo(func() (data.Position, bool) {
		altar, found := v.ctx.Data.Objects.FindOne(object.TaintedSunAltar)
		if found {
			v.ctx.Logger.Info("Tainted Sun Altar found, moving to that room")
			return altar.Position, true
		}
		return data.Position{}, false
	})
	if err != nil {
		return err
	}

	// Disable item pickup to avoid distractions while fighting Fangskin
	v.ctx.DisableItemPickup()
	defer v.ctx.EnableItemPickup()

	action.ClearAreaAroundPlayer(30, data.MonsterAnyFilter())

	obj, found := v.ctx.Data.Objects.FindOne(object.TaintedSunAltar)
	if !found {
		return errors.New("Tainted Sun Altar not found")
	}

	v.ctx.Logger.Info("Breaking the Tainted Sun Altar")
	err = action.InteractObject(obj, func() bool {
		updatedObj, found := v.ctx.Data.Objects.FindOne(object.TaintedSunAltar)
		if found {
			return !updatedObj.Selectable
		}
		return false
	})
	if err != nil {
		return err
	}

	v.ctx.EnableItemPickup()
	utils.Sleep(200)
	action.ItemPickup(-1)

	return nil
}
