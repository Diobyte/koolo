package run

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/object"
	"github.com/hectorgimenez/d2go/pkg/data/quest"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/utils"
)

const ScrollInifussUnitID = 539
const ScrollInifussAfterAkara = 540
const ScrollInifussName = "Scroll of Inifuss"

// isInifussScroll returns true if the item is any variant of the Scroll of Inifuss,
// including the DLC-renamed "KeyToTheCairnStones".
func isInifussScroll(itm data.Item) bool {
	if itm.ID == ScrollInifussUnitID || itm.ID == ScrollInifussAfterAkara {
		return true
	}
	switch itm.Name {
	case ScrollInifussName, "ScrollOfInifuss", "KeyToTheCairnStones":
		return true
	}
	return false
}

type RescueCain struct {
	ctx *context.Status
}

func NewRescueCain() *RescueCain {
	return &RescueCain{
		ctx: context.Get(),
	}
}

func (rc RescueCain) Name() string {
	return string(config.RescueCainRun)
}

func (rc RescueCain) CheckConditions(parameters *RunParameters) SequencerResult {
	if IsFarmingRun(parameters) {
		return SequencerError
	}
	if rc.ctx.Data.Quests[quest.Act1TheSearchForCain].Completed() {
		return SequencerSkip
	}

	return SequencerOk
}

func (rc RescueCain) Run(parameters *RunParameters) error {
	rc.ctx.Logger.Info("Starting Rescue Cain Quest...")

	// --- Navigation to the Dark Wood and a safe zone near the Inifuss Tree ---
	err := action.WayPoint(area.RogueEncampment)
	if err != nil {
		return err
	}

	needToGoToTristram := (rc.ctx.Data.Quests[quest.Act1TheSearchForCain].HasStatus(quest.StatusInProgress2) ||
		rc.ctx.Data.Quests[quest.Act1TheSearchForCain].HasStatus(quest.StatusInProgress3) ||
		rc.ctx.Data.Quests[quest.Act1TheSearchForCain].HasStatus(quest.StatusEnterArea))

	infusInInventory := false
	for _, itm := range rc.ctx.Data.Inventory.ByLocation(item.LocationInventory) {
		if isInifussScroll(itm) {
			infusInInventory = true
			break
		}
	}

	rc.ctx.Logger.Info("Quest state evaluated",
		slog.Bool("needToGoToTristram", needToGoToTristram),
		slog.Bool("infusInInventory", infusInInventory),
	)

	if !infusInInventory && !needToGoToTristram {
		err = rc.gatherInfussScroll()
		if err != nil {
			return err
		}
		infusInInventory = true
	}

	if infusInInventory {
		rc.ctx.Logger.Info("Scroll in inventory, giving it to Akara for decryption")
		err = action.InteractNPC(npc.Akara)
		if err != nil {
			return err
		}

		step.CloseAllMenus()
	}

	// Use waypoint to StonyField
	err = action.WayPoint(area.StonyField)
	if err != nil {
		return err
	}

	// Find the Cairn Stone Alpha
	cairnStone := data.Object{}
	foundCairn := false
	for _, o := range rc.ctx.Data.Objects {
		if o.Name == object.CairnStoneAlpha {
			cairnStone = o
			foundCairn = true
		}
	}
	if !foundCairn {
		rc.ctx.Logger.Error("Cairn Stone Alpha not found in Stony Field")
		return errors.New("cairn stone alpha not found")
	}

	rc.ctx.Logger.Debug("Found Cairn Stone Alpha", slog.Any("position", cairnStone.Position))

	// Move to the cairnStone
	if err = action.MoveToCoords(cairnStone.Position); err != nil {
		rc.ctx.Logger.Error("Failed to move to Cairn Stone Alpha", slog.String("error", err.Error()))
		return fmt.Errorf("moving to cairn stone: %w", err)
	}
	if err = action.ClearAreaAroundPlayer(10, data.MonsterAnyFilter()); err != nil {
		rc.ctx.Logger.Warn("Failed to clear area around Cairn Stones", slog.String("error", err.Error()))
	}

	// Handle opening Tristram Portal, will be skipped if its already opened
	if err = rc.openPortalIfNotOpened(); err != nil {
		return err
	}

	// Enter Tristram portal
	tristPortal, portalFound := rc.ctx.Data.Objects.FindOne(object.PermanentTownPortal)
	if !portalFound {
		rc.ctx.Logger.Error("Tristram portal not found after opening attempt")
		return errors.New("tristram portal not found")
	}

	rc.ctx.Logger.Info("Entering Tristram portal", slog.Any("position", tristPortal.Position))
	if err = action.InteractObject(tristPortal, func() bool {
		return rc.ctx.Data.PlayerUnit.Area == area.Tristram && rc.ctx.Data.AreaData.IsInside(rc.ctx.Data.PlayerUnit.Position)
	}); err != nil {
		return fmt.Errorf("entering tristram portal: %w", err)
	}

	// Check if Cain is rescued
	if o, found := rc.ctx.Data.Objects.FindOne(object.CainGibbet); found && o.Selectable {
		rc.ctx.Logger.Info("Found Cain in gibbet, attempting rescue", slog.Any("position", o.Position))

		if err = action.MoveToCoords(o.Position); err != nil {
			rc.ctx.Logger.Warn("Failed to move to Cain gibbet", slog.String("error", err.Error()))
		}

		if err = action.InteractObject(o, func() bool {
			obj, _ := rc.ctx.Data.Objects.FindOne(object.CainGibbet)
			return !obj.Selectable
		}); err != nil {
			rc.ctx.Logger.Warn("Failed to interact with Cain gibbet", slog.String("error", err.Error()))
		}
	} else if found && !o.Selectable {
		rc.ctx.Logger.Info("Cain already rescued (gibbet not selectable)")
	} else {
		rc.ctx.Logger.Warn("Cain gibbet not found in Tristram")
	}

	if err = action.ReturnTown(); err != nil {
		rc.ctx.Logger.Warn("Failed to return to town from Tristram", slog.String("error", err.Error()))
	}

	utils.Sleep(10000)

	rc.ctx.Logger.Info("Talking to Deckard Cain to complete quest")
	err = action.InteractNPC(npc.DeckardCain5)
	if err != nil {
		rc.ctx.Logger.Warn("Failed to talk to Deckard Cain", slog.String("error", err.Error()))
	}

	step.CloseAllMenus()

	rc.ctx.Logger.Info("Talking to Akara for quest reward")
	err = action.InteractNPC(npc.Akara)
	if err != nil {
		rc.ctx.Logger.Warn("Failed to talk to Akara for reward", slog.String("error", err.Error()))
	}

	step.CloseAllMenus()

	rc.ctx.Logger.Info("Rescue Cain quest run completed")
	return nil
}

func (rc RescueCain) gatherInfussScroll() error {
	rc.ctx.CharacterCfg.Character.ClearPathDist = 20
	if err := config.SaveSupervisorConfig(rc.ctx.CharacterCfg.ConfigFolderName, rc.ctx.CharacterCfg); err != nil {
		rc.ctx.Logger.Error("Failed to save character configuration", slog.String("error", err.Error()))
	}

	err := action.WayPoint(area.DarkWood)
	if err != nil {
		return err
	}

	rc.ctx.CharacterCfg.Character.ClearPathDist = 30
	if err := config.SaveSupervisorConfig(rc.ctx.CharacterCfg.ConfigFolderName, rc.ctx.CharacterCfg); err != nil {
		rc.ctx.Logger.Error("Failed to save character configuration", slog.String("error", err.Error()))
	}

	// Find the Inifuss Tree position.
	var inifussTreePos data.Position
	var foundTree bool
	for _, o := range rc.ctx.Data.Objects {
		if o.Name == object.InifussTree {
			inifussTreePos = o.Position
			foundTree = true
			break
		}
	}
	if !foundTree {
		rc.ctx.Logger.Error("InifussTree not found, aborting quest.")
		return errors.New("InifussTree not found")
	}

	err = action.MoveToCoords(inifussTreePos)
	if err != nil {
		return err
	}

	obj, found := rc.ctx.Data.Objects.FindOne(object.InifussTree)
	if !found {
		rc.ctx.Logger.Error("InifussTree not found, aborting quest.")
		return errors.New("InifussTree not found")
	}

	err = action.InteractObject(obj, func() bool {
		updatedObj, found := rc.ctx.Data.Objects.FindOne(object.InifussTree)
		return found && !updatedObj.Selectable
	})
	if err != nil {
		return fmt.Errorf("error interacting with Inifuss Tree: %w", err)
	}

PickupLoop:
	for i := 0; i < 5; i++ {
		rc.ctx.RefreshGameData()

		foundInInv := false
		for _, itm := range rc.ctx.Data.Inventory.ByLocation(item.LocationInventory) {
			if isInifussScroll(itm) {
				foundInInv = true
				break
			}
		}

		if foundInInv {
			rc.ctx.Logger.Info(fmt.Sprintf("%s found in inventory. Proceeding with quest.", ScrollInifussName))
			break PickupLoop
		}

		// Find the scroll on the ground.
		var scrollObj data.Item
		foundOnGround := false
		for _, itm := range rc.ctx.Data.Inventory.ByLocation(item.LocationGround) {
			if isInifussScroll(itm) {
				scrollObj = itm
				foundOnGround = true
				break
			}
		}

		if foundOnGround {
			rc.ctx.Logger.Info(fmt.Sprintf("%s found on the ground at position %v. Attempting pickup (Attempt %d)...", ScrollInifussName, scrollObj.Position, i+1))

			playerPos := rc.ctx.Data.PlayerUnit.Position
			safeAwayPos := atDistance(scrollObj.Position, playerPos, -5)

			pickupAttempts := 0
			for pickupAttempts < 8 {
				rc.ctx.Logger.Debug("Moving away from scroll for a brief moment...")
				moveAwayErr := action.MoveToCoords(safeAwayPos)
				if moveAwayErr != nil {
					rc.ctx.Logger.Warn(fmt.Sprintf("Failed to move away from scroll: %v", moveAwayErr))
				}
				utils.Sleep(200)

				moveErr := action.MoveToCoords(scrollObj.Position)
				if moveErr != nil {
					rc.ctx.Logger.Error(fmt.Sprintf("Failed to move to scroll position: %v", moveErr))
					utils.Sleep(500)
					pickupAttempts++
					continue
				}

				// --- Refresh game data just before pickup attempt ---
				rc.ctx.RefreshGameData()

				pickupErr := action.ItemPickup(10)
				if pickupErr != nil {
					rc.ctx.Logger.Warn(fmt.Sprintf("Pickup attempt %d failed: %v", pickupAttempts+1, pickupErr))
					utils.Sleep(500)
					pickupAttempts++
					continue
				}

				rc.ctx.RefreshGameData()
				foundInInvAfterPickup := false
				for _, itm := range rc.ctx.Data.Inventory.ByLocation(item.LocationInventory) {
					if isInifussScroll(itm) {
						foundInInvAfterPickup = true
						break
					}
				}
				if foundInInvAfterPickup {
					rc.ctx.Logger.Info(fmt.Sprintf("Pickup confirmed for %s after %d attempts. Proceeding.", ScrollInifussName, pickupAttempts+1))
					break PickupLoop
				}
				pickupAttempts++
			}
		} else {
			rc.ctx.Logger.Debug(fmt.Sprintf("%s not found on the ground on attempt %d. Retrying.", ScrollInifussName, i+1))
			utils.Sleep(1000)
		}
	}

	infusInInventory := false
	for _, itm := range rc.ctx.Data.Inventory.ByLocation(item.LocationInventory) {
		if isInifussScroll(itm) {
			infusInInventory = true
			break
		}
	}
	if !infusInInventory {
		rc.ctx.Logger.Error(fmt.Sprintf("Failed to pick up %s after all attempts. Aborting current run.", ScrollInifussName))
		return errors.New("failed to pick up Scroll of Inifuss")
	}

	err = action.ReturnTown()
	if err != nil {
		return err
	}

	return nil
}
func (rc RescueCain) openPortalIfNotOpened() error {

	// If the portal already exists, skip this
	if _, found := rc.ctx.Data.Objects.FindOne(object.PermanentTownPortal); found {
		rc.ctx.Logger.Debug("Tristram portal already open, skipping stone activation")
		return nil
	}

	rc.ctx.Logger.Info("Tristram portal not detected, activating Cairn Stones")

	for attempt := range 6 {
		activeStones := 0
		for _, cainStone := range []object.Name{
			object.CairnStoneAlpha,
			object.CairnStoneGamma,
			object.CairnStoneBeta,
			object.CairnStoneLambda,
			object.CairnStoneDelta,
		} {
			stone, found := rc.ctx.Data.Objects.FindOne(cainStone)
			if !found {
				rc.ctx.Logger.Warn("Cairn Stone not found", slog.Any("stone", cainStone))
				continue
			}
			if stone.Selectable {
				rc.ctx.PathFinder.RandomMovement()
				utils.Sleep(250)
				if err := action.InteractObject(stone, func() bool {
					st, _ := rc.ctx.Data.Objects.FindOne(cainStone)
					return !st.Selectable
				}); err != nil {
					rc.ctx.Logger.Warn("Failed to activate Cairn Stone",
						slog.Any("stone", cainStone),
						slog.String("error", err.Error()),
					)
				}
			} else {
				utils.Sleep(200)
				activeStones++
			}
			_, tristPortal := rc.ctx.Data.Objects.FindOne(object.PermanentTownPortal)
			if activeStones >= 5 || tristPortal {
				break
			}
		}
		rc.ctx.Logger.Debug("Stone activation round finished",
			slog.Int("attempt", attempt+1),
			slog.Int("activeStones", activeStones),
		)
	}

	// Wait up to 15 seconds for the portal to open, checking every second
	rc.ctx.Logger.Debug("Waiting for Tristram portal to appear")
	for i := range 15 {
		utils.Sleep(1000)

		if _, portalFound := rc.ctx.Data.Objects.FindOne(object.PermanentTownPortal); portalFound {
			rc.ctx.Logger.Info("Tristram portal appeared", slog.Int("waitSeconds", i+1))
			return nil
		}
	}

	rc.ctx.Logger.Error("Tristram portal did not appear after 15 seconds")
	return errors.New("failed to open Tristram portal")
}
