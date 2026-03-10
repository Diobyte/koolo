package run

import (
	"errors"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/object"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/utils"

	"github.com/hectorgimenez/d2go/pkg/data/area"
)

type MaggotLair struct {
	ctx *context.Status
}

func NewMaggotLair() *MaggotLair {
	return &MaggotLair{
		ctx: context.Get(),
	}
}

func (m MaggotLair) Name() string {
	return string(config.MaggotLairRun)
}

func (m MaggotLair) CheckConditions(parameters *RunParameters) SequencerResult {
	return SequencerOk
}

func (m MaggotLair) Run(parameters *RunParameters) error {
	m.ctx.Logger.Info("Starting Maggot Lair run for the Staff of Kings")

	err := action.WayPoint(area.FarOasis)
	if err != nil {
		return err
	}

	err = action.MoveToArea(area.MaggotLairLevel1)
	if err != nil {
		return err
	}

	err = action.MoveToArea(area.MaggotLairLevel2)
	if err != nil {
		return err
	}

	err = action.MoveToArea(area.MaggotLairLevel3)
	if err != nil {
		return err
	}

	err = action.MoveTo(func() (data.Position, bool) {
		chest, found := m.ctx.Data.Objects.FindOne(object.StaffOfKingsChest)
		if found {
			m.ctx.Logger.Info("Staff of Kings chest found, moving to that room")
			return chest.Position, true
		}
		return data.Position{}, false
	})
	if err != nil {
		return err
	}

	// Disable item pickup to avoid distractions while fighting Coldworm the Burrower
	m.ctx.DisableItemPickup()
	defer m.ctx.EnableItemPickup()

	action.ClearAreaAroundPlayer(30, data.MonsterAnyFilter())

	obj, found := m.ctx.Data.Objects.FindOne(object.StaffOfKingsChest)
	if !found {
		return errors.New("Staff of Kings chest not found")
	}

	m.ctx.Logger.Info("Opening the Staff of Kings chest")
	err = action.InteractObject(obj, func() bool {
		updatedObj, found := m.ctx.Data.Objects.FindOne(object.StaffOfKingsChest)
		if found {
			return !updatedObj.Selectable
		}
		return false
	})
	if err != nil {
		return err
	}

	m.ctx.EnableItemPickup()
	utils.Sleep(200)
	action.ItemPickup(-1)

	return nil
}
