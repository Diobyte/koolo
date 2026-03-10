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

	// Ensure stash is open
	if !ctx.Data.OpenMenus.Stash {
		bank, _ := ctx.Data.Objects.FindOne(object.Bank)
		err := InteractObject(bank, func() bool {
			return ctx.Data.OpenMenus.Stash
		})
		if err != nil {
			return err
		}
		// The first stash open each game lands on personal; subsequent opens
		// remember the last tab/page.
		if !ctx.CurrentGame.HasOpenedStash {
			ctx.CurrentGame.CurrentStashTab = 1
			ctx.CurrentGame.HasOpenedStash = true
		}
	}
	// Clear messages like TZ change or public game spam.  Prevent bot from clicking on messages
	ClearMessages()
	ctx.Logger.Info("Adding items to the Horadric Cube", slog.Any("items", items))

	// Separate DLC tab items from regular items. DLC items will be moved
	// directly into the cube via Ctrl+Shift+Click (ROTW feature), skipping
	// the intermediate inventory step entirely.
	var dlcItems []data.Item
	var regularItems []data.Item
	for _, itm := range items {
		switch itm.Location.LocationType {
		case item.LocationGemsTab, item.LocationMaterialsTab, item.LocationRunesTab:
			dlcItems = append(dlcItems, itm)
		default:
			regularItems = append(regularItems, itm)
		}
	}

	// Open and empty the cube BEFORE picking up recipe items from stash.
	// ensureCubeIsEmpty() may call stashInventory() which would stash any
	// loose inventory items — including recipe ingredients if we picked
	// them up first.
	err := ensureCubeIsOpen()
	if err != nil {
		return err
	}

	err = ensureCubeIsEmpty()
	if err != nil {
		return err
	}

	// Close the cube so we can access stash tabs to pick up recipe items.
	ctx.HID.PressKey(win.VK_ESCAPE)
	utils.Sleep(300)
	ctx.RefreshGameData()

	// Ensure stash is open for picking up regular items
	if len(regularItems) > 0 && !ctx.Data.OpenMenus.Stash {
		bank, _ := ctx.Data.Objects.FindOne(object.Bank)
		err = InteractObject(bank, func() bool {
			return ctx.Data.OpenMenus.Stash
		})
		if err != nil {
			return err
		}
	}

	// Pick up regular stash items (personal/shared) to inventory
	for _, itm := range regularItems {
		nwIt := itm
		if nwIt.Location.LocationType != item.LocationStash &&
			nwIt.Location.LocationType != item.LocationSharedStash {
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
			}
		}

		ctx.Logger.Debug("Item found on the stash, picking it up",
			slog.String("Item", string(nwIt.Name)),
			slog.String("Location", string(nwIt.Location.LocationType)),
			slog.Int("MemPosX", nwIt.Position.X),
			slog.Int("MemPosY", nwIt.Position.Y),
		)

		screenPos := ui.GetScreenCoordsForItem(nwIt)
		ctx.Logger.Debug("Clicking item at computed screen position",
			slog.String("Item", string(nwIt.Name)),
			slog.Int("ScreenX", screenPos.X),
			slog.Int("ScreenY", screenPos.Y),
		)
		ctx.HID.ClickWithModifier(game.LeftButton, screenPos.X, screenPos.Y, game.CtrlKey)
		utils.Sleep(300)
	}

	// Open cube to place items from inventory
	err = ensureCubeIsOpen()
	if err != nil {
		return err
	}

	// Refresh game data so items reflect their current inventory positions,
	// not their original stash/DLC tab positions from before the pickup phase.
	ctx.RefreshGameData()

	// Move regular items from inventory into cube
	usedUnitIDs := make(map[data.UnitID]struct{})

	for _, itm := range regularItems {
		var found *data.Item

		for _, updatedItem := range ctx.Data.Inventory.AllItems {
			if updatedItem.UnitID == itm.UnitID {
				found = &updatedItem
				break
			}
		}

		if found == nil {
			ctx.Logger.Warn("Item not found in inventory for cube",
				slog.String("Item", string(itm.Name)),
				slog.Int("UnitID", int(itm.UnitID)),
			)
			continue
		}

		usedUnitIDs[found.UnitID] = struct{}{}

		ctx.Logger.Debug("Moving Item to the Horadric Cube",
			slog.String("Item", string(found.Name)),
			slog.String("Location", string(found.Location.LocationType)),
			slog.Int("PosX", found.Position.X),
			slog.Int("PosY", found.Position.Y),
		)

		screenPos := ui.GetScreenCoordsForItem(*found)
		ctx.HID.ClickWithModifier(game.LeftButton, screenPos.X, screenPos.Y, game.CtrlKey)
		utils.Sleep(500)
	}

	// ROTW: Move DLC tab items directly into the cube via Ctrl+Shift+Click.
	// This skips the intermediate inventory step, saving time on every recipe
	// that uses gems, runes, or materials from DLC tabs.
	//
	// The cube overlay replaces the stash panel, so we must close it first.
	// Ctrl+Shift+Click moves items into the cube container regardless of
	// whether the cube window is displayed; CubeTransmute reopens it.
	if len(dlcItems) > 0 {
		ctx.HID.PressKey(win.VK_ESCAPE)
		utils.Sleep(300)
	}
	for _, itm := range dlcItems {
		switch itm.Location.LocationType {
		case item.LocationGemsTab:
			SwitchStashTab(StashTabGems)
		case item.LocationMaterialsTab:
			SwitchStashTab(StashTabMaterials)
		case item.LocationRunesTab:
			SwitchStashTab(StashTabRunes)
		}

		screenPos := ui.GetScreenCoordsForItem(itm)
		ctx.Logger.Debug("Moving DLC item directly to cube via Ctrl+Shift+Click",
			slog.String("Item", string(itm.Name)),
			slog.Int("ScreenX", screenPos.X),
			slog.Int("ScreenY", screenPos.Y),
		)
		ctx.HID.ClickWithModifiers(game.LeftButton, screenPos.X, screenPos.Y, game.CtrlKey, game.ShiftKey)
		utils.Sleep(300)
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
	utils.Sleep(150)

	if ctx.Data.LegacyGraphics {
		ctx.HID.Click(game.LeftButton, ui.CubeTransmuteBtnXClassic, ui.CubeTransmuteBtnYClassic)
	} else {
		ctx.HID.Click(game.LeftButton, ui.CubeTransmuteBtnX, ui.CubeTransmuteBtnY)
	}

	utils.Sleep(2000)

	// Take the items out of the cube
	for _, itm := range ctx.Data.Inventory.ByLocation(item.LocationCube) {
		ctx.Logger.Debug("Moving Item to the inventory", slog.String("Item", string(itm.Name)))

		screenPos := ui.GetScreenCoordsForItem(itm)

		ctx.HID.ClickWithModifier(game.LeftButton, screenPos.X, screenPos.Y, game.CtrlKey)
		utils.Sleep(500)
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
		utils.Sleep(700)

		itm, _ = ctx.Data.Inventory.FindByID(itm.UnitID)
		if itm.Location.LocationType == item.LocationCube {
			return fmt.Errorf("item %s could not be removed from the cube", itm.Name)
		}
	}

	ctx.HID.PressKey(win.VK_ESCAPE)
	utils.Sleep(300)

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
		ctx := context.Get()

		// Ensure stash is open
		if !ctx.Data.OpenMenus.Stash {
			bank, _ := ctx.Data.Objects.FindOne(object.Bank)
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

	utils.Sleep(300)
	ctx.HID.Click(game.RightButton, screenPos.X, screenPos.Y)
	utils.Sleep(500)

	if ctx.Data.OpenMenus.Cube {
		ctx.Logger.Debug("Horadric Cube window detected")
		return nil
	}

	return errors.New("horadric Cube window not detected")
}
