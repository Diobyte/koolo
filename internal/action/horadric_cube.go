package action

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/d2go/pkg/data/object"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"
	"github.com/lxn/win"
)

func CubeAddItems(items ...data.Item) error {
	ctx := context.Get()
	ctx.SetLastAction("CubeAddItems")

	ClearMessages()
	ctx.Logger.Info("Adding items to the Horadric Cube", slog.Any("items", items))

	// Phase 1: Pull recipe items from stash into inventory (if any are stashed).
	needsStash := false
	for _, itm := range items {
		switch itm.Location.LocationType {
		case item.LocationStash, item.LocationSharedStash,
			item.LocationGemsTab, item.LocationMaterialsTab, item.LocationRunesTab:
			needsStash = true
		}
		if needsStash {
			break
		}
	}

	if needsStash {
		if !ctx.Data.OpenMenus.Stash {
			bank, found := ctx.Data.Objects.FindOne(object.Bank)
			if !found {
				return errors.New("stash object not found nearby")
			}
			err := InteractObject(bank, func() bool {
				return ctx.Data.OpenMenus.Stash
			})
			if err != nil {
				return err
			}
			if !ctx.CurrentGame.HasOpenedStash {
				ctx.CurrentGame.CurrentStashTab = 1
				ctx.CurrentGame.HasOpenedStash = true
			}
		}

		for _, itm := range items {
			nwIt := itm
			if nwIt.Location.LocationType != item.LocationStash &&
				nwIt.Location.LocationType != item.LocationSharedStash &&
				nwIt.Location.LocationType != item.LocationGemsTab &&
				nwIt.Location.LocationType != item.LocationMaterialsTab &&
				nwIt.Location.LocationType != item.LocationRunesTab {
				continue
			}

			if requiresPersonalStash(nwIt) {
				if nwIt.Location.LocationType == item.LocationSharedStash {
					return fmt.Errorf("quest item %s must be in personal stash to use the cube", nwIt.Name)
				}
				SwitchStashTab(1)
			} else {
				switch nwIt.Location.LocationType {
				case item.LocationStash:
					SwitchStashTab(1)
				case item.LocationSharedStash:
					SwitchStashTab(nwIt.Location.Page + 1)
				case item.LocationGemsTab:
					SwitchStashTab(StashTabGems)
				case item.LocationMaterialsTab:
					SwitchStashTab(StashTabMaterials)
				case item.LocationRunesTab:
					SwitchStashTab(StashTabRunes)
				}
			}

			screenPos := ui.GetScreenCoordsForItem(nwIt)
			ctx.HID.ClickWithModifier(game.LeftButton, screenPos.X, screenPos.Y, game.CtrlKey)
			utils.PingSleep(utils.Light, 200)
		}
	}

	// Phase 2: Open cube (automatically closes stash if it was open).
	if err := ensureCubeIsOpen(); err != nil {
		return err
	}

	// Phase 3: Quick-empty cube if it has leftover items.
	// Just ctrl-click them to inventory — no close/stash/reopen cycle needed.
	ctx.RefreshGameData()
	cubeItems := ctx.Data.Inventory.ByLocation(item.LocationCube)
	if len(cubeItems) > 0 {
		ctx.Logger.Debug("Quick-emptying cube before adding recipe items")
		for _, itm := range cubeItems {
			screenPos := ui.GetScreenCoordsForItem(itm)
			ctx.HID.ClickWithModifier(game.LeftButton, screenPos.X, screenPos.Y, game.CtrlKey)
			utils.PingSleep(utils.Light, 200)
		}
		ctx.RefreshGameData()
	}

	// Phase 4: Move recipe items from inventory into cube.
	usedUnitIDs := make(map[data.UnitID]struct{})

	for _, itm := range items {
		var found *data.Item

		// DLC tab items (gems, runes, materials) get new UnitIDs when moved to
		// inventory, so we must match by Name in inventory instead of by UnitID.
		isDLC := itm.Location.LocationType == item.LocationGemsTab ||
			itm.Location.LocationType == item.LocationMaterialsTab ||
			itm.Location.LocationType == item.LocationRunesTab

		for _, updatedItem := range ctx.Data.Inventory.AllItems {
			if isDLC {
				if _, used := usedUnitIDs[updatedItem.UnitID]; used {
					continue
				}
				if updatedItem.Name == itm.Name && updatedItem.Location.LocationType == item.LocationInventory {
					found = &updatedItem
					break
				}
			} else {
				if updatedItem.UnitID == itm.UnitID {
					found = &updatedItem
					break
				}
			}
		}

		if found != nil {
			usedUnitIDs[found.UnitID] = struct{}{}
		} else {
			ctx.Logger.Warn("Item not found in inventory for cube, aborting recipe",
				slog.String("Item", string(itm.Name)),
				slog.Int("UnitID", int(itm.UnitID)),
			)
			// Try to return any items already placed in the cube back to inventory.
			ctx.RefreshGameData()
			for _, stuck := range ctx.Data.Inventory.ByLocation(item.LocationCube) {
				sp := ui.GetScreenCoordsForItem(stuck)
				ctx.HID.ClickWithModifier(game.LeftButton, sp.X, sp.Y, game.CtrlKey)
				utils.PingSleep(utils.Light, 200)
			}
			step.CloseAllMenus()
			return fmt.Errorf("item %s not found in inventory for cube (inventory may be full)", itm.Name)
		}

		screenPos := ui.GetScreenCoordsForItem(*found)
		ctx.HID.ClickWithModifier(game.LeftButton, screenPos.X, screenPos.Y, game.CtrlKey)
		utils.PingSleep(utils.Light, 300)
	}

	return nil
}

func CubeTransmute() error {
	ctx := context.Get()

	err := ensureCubeIsOpen()
	if err != nil {
		return err
	}

	ctx.Logger.Debug("Transmuting items in the Horadric Cube")

	if ctx.Data.LegacyGraphics {
		ctx.HID.Click(game.LeftButton, ui.CubeTransmuteBtnXClassic, ui.CubeTransmuteBtnYClassic)
	} else {
		ctx.HID.Click(game.LeftButton, ui.CubeTransmuteBtnX, ui.CubeTransmuteBtnY)
	}

	utils.PingSleep(utils.Critical, 800)

	ctx.RefreshGameData()

	// Take the transmuted items out of the cube
	for _, itm := range ctx.Data.Inventory.ByLocation(item.LocationCube) {
		screenPos := ui.GetScreenCoordsForItem(itm)
		ctx.HID.ClickWithModifier(game.LeftButton, screenPos.X, screenPos.Y, game.CtrlKey)
		utils.PingSleep(utils.Light, 200)
	}

	return step.CloseAllMenus()
}

func EmptyCube() error {
	err := ensureCubeIsOpen()
	if err != nil {
		return err
	}

	err = ensureCubeIsEmpty()
	if err != nil {
		return err
	}

	return step.CloseAllMenus()
}

func ensureCubeIsEmpty() error {
	ctx := context.Get()
	if !ctx.Data.OpenMenus.Cube {
		return errors.New("horadric Cube window not detected")
	}

	cubeItems := ctx.Data.Inventory.ByLocation(item.LocationCube)
	if len(cubeItems) == 0 {
		return nil
	}

	ctx.Logger.Debug("Emptying the Horadric Cube")
	for _, itm := range cubeItems {
		ctx.Logger.Debug("Moving Item to the inventory", slog.String("Item", string(itm.Name)))

		screenPos := ui.GetScreenCoordsForItem(itm)

		ctx.HID.ClickWithModifier(game.LeftButton, screenPos.X, screenPos.Y, game.CtrlKey)
		utils.PingSleep(utils.Medium, 400) // Medium operation: Wait for item removal from cube

		ctx.RefreshGameData()
		updated, found := ctx.Data.Inventory.FindByID(itm.UnitID)
		if found && updated.Location.LocationType == item.LocationCube {
			return fmt.Errorf("item %s could not be removed from the cube", itm.Name)
		}
	}

	ctx.HID.PressKey(win.VK_ESCAPE)
	utils.PingSleep(utils.Light, 200) // Light operation: Wait for menu to close

	stashInventory(true)

	return ensureCubeIsOpen()
}

func ensureCubeIsOpen() error {
	ctx := context.Get()
	ctx.Logger.Debug("Opening Horadric Cube...")

	if ctx.Data.OpenMenus.Cube {
		ctx.Logger.Debug("Horadric Cube window already open")
		return nil
	}

	cube, found := ctx.Data.Inventory.Find("HoradricCube", item.LocationInventory, item.LocationStash)
	if !found {
		return errors.New("horadric cube not found in inventory")
	}

	// If cube is in stash, switch to the correct tab
	if cube.Location.LocationType == item.LocationStash || cube.Location.LocationType == item.LocationSharedStash {
		// Ensure stash is open
		if !ctx.Data.OpenMenus.Stash {
			bank, found := ctx.Data.Objects.FindOne(object.Bank)
			if !found {
				return errors.New("stash object not found nearby")
			}
			err := InteractObject(bank, func() bool {
				return ctx.Data.OpenMenus.Stash
			})
			if err != nil {
				return err
			}
		}

		SwitchStashTab(cube.Location.Page + 1)
	}

	screenPos := ui.GetScreenCoordsForItem(cube)
	cubeInInventory := cube.Location.LocationType == item.LocationInventory

	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			step.CloseAllMenus()
			utils.PingSleep(utils.Light, 150)
		}

		// The inventory panel must be visible so the cube can be right-clicked.
		if cubeInInventory && !ctx.Data.OpenMenus.Inventory && !ctx.Data.OpenMenus.Stash && !ctx.Data.OpenMenus.Cube {
			step.OpenInventory()
			utils.PingSleep(utils.Light, 150)
		}

		ctx.HID.Click(game.RightButton, screenPos.X, screenPos.Y)
		utils.PingSleep(utils.Light, 300)

		ctx.RefreshGameData()
		if ctx.Data.OpenMenus.Cube {
			return nil
		}
		ctx.Logger.Debug(fmt.Sprintf("Horadric Cube not detected, retrying (%d/4)", attempt+1))
	}

	return errors.New("horadric Cube window not detected after 4 attempts")
}
