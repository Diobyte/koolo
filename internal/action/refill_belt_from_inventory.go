package action

import (
	"fmt"
	"strings"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/d2go/pkg/data/object"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"
)

func RefillBeltFromInventory() error {
	defer step.CloseAllMenus()

	ctx := context.Get()
	ctx.Logger.Info("Refilling belt from inventory")

	healingPotions := ctx.Data.PotionsInInventory(data.HealingPotion)
	manaPotions := ctx.Data.PotionsInInventory(data.ManaPotion)
	rejuvPotions := ctx.Data.PotionsInInventory(data.RejuvenationPotion)

	missingHealingPotionCount := ctx.BeltManager.GetMissingCount(data.HealingPotion)
	missingManaPotionCount := ctx.BeltManager.GetMissingCount(data.ManaPotion)
	missingRejuvPotionCount := ctx.BeltManager.GetMissingCount(data.RejuvenationPotion)

	if !((missingHealingPotionCount > 0 && len(healingPotions) > 0) || (missingManaPotionCount > 0 && len(manaPotions) > 0) || (missingRejuvPotionCount > 0 && len(rejuvPotions) > 0)) {
		ctx.Logger.Debug("No need to refill belt from inventory")
		return nil
	}

	// Add slight delay before opening inventory
	utils.Sleep(200)

	if err := step.OpenInventory(); err != nil {
		return err
	}

	// Refill healing potions
	for i := 0; i < missingHealingPotionCount && i < len(healingPotions); i++ {
		putPotionInBelt(ctx, healingPotions[i])
	}

	// Refill mana potions
	for i := 0; i < missingManaPotionCount && i < len(manaPotions); i++ {
		putPotionInBelt(ctx, manaPotions[i])
	}

	// Refill rejuvenation potions
	for i := 0; i < missingRejuvPotionCount && i < len(rejuvPotions); i++ {
		putPotionInBelt(ctx, rejuvPotions[i])
	}

	ctx.Logger.Info("Belt refilled from inventory")
	err := step.CloseAllMenus()
	if err != nil {
		return err
	}

	// Add slight delay after closing inventory
	utils.Sleep(200)
	return nil

}

func putPotionInBelt(ctx *context.Status, potion data.Item) {
	screenPos := ui.GetScreenCoordsForItem(potion)
	ctx.HID.ClickWithModifier(game.LeftButton, screenPos.X, screenPos.Y, game.ShiftKey)
	utils.Sleep(150)
}

// RefillRejuvsFromDLCStash pulls rejuvenation potions from the DLC Materials
// tab into the inventory when the belt + inventory are short on rejuvs. This
// leverages the ROTW stackable Materials tab where rejuv potions are stored.
// Right-clicking a DLC tab potion extracts 3 at a time; Ctrl+Click extracts 1.
func RefillRejuvsFromDLCStash() error {
	ctx := context.Get()

	if !ctx.Data.IsDLC() {
		return nil
	}

	// Calculate how many rejuvs we need (belt + inventory)
	missingInBelt := ctx.BeltManager.GetMissingCount(data.RejuvenationPotion)
	missingInInventory := ctx.Data.MissingPotionCountInInventory(data.RejuvenationPotion)
	totalNeeded := missingInBelt + missingInInventory
	if totalNeeded == 0 {
		return nil
	}

	// Check if the Materials tab has any rejuv potions
	materialsItems := FilterDLCGhostItems(ctx.Data.Inventory.ByLocation(item.LocationMaterialsTab))
	var rejuvStacks []data.Item
	for _, itm := range materialsItems {
		if strings.Contains(string(itm.Name), string(data.RejuvenationPotion)) {
			rejuvStacks = append(rejuvStacks, itm)
		}
	}

	if len(rejuvStacks) == 0 {
		return nil
	}

	// Calculate total available rejuvs
	totalAvailable := 0
	for _, stack := range rejuvStacks {
		totalAvailable += GetItemQuantity(stack)
	}
	if totalAvailable == 0 {
		return nil
	}

	ctx.Logger.Info(fmt.Sprintf("Pulling rejuv potions from DLC Materials tab (need %d, have %d stashed)", totalNeeded, totalAvailable))

	// Open stash
	bank, found := ctx.Data.Objects.FindOne(object.Bank)
	if !found {
		return nil
	}
	InteractObject(bank, func() bool {
		return ctx.Data.OpenMenus.Stash
	})
	if !ctx.CurrentGame.HasOpenedStash {
		ctx.CurrentGame.CurrentStashTab = 1
		ctx.CurrentGame.HasOpenedStash = true
	}

	SwitchStashTab(StashTabMaterials)

	extracted := 0
	for _, stack := range rejuvStacks {
		if extracted >= totalNeeded {
			break
		}

		stackQty := GetItemQuantity(stack)
		screenPos := ui.GetScreenCoordsForItem(stack)

		// Use right-click to extract 3 at a time when we need 3+ and have 3+
		for extracted < totalNeeded && stackQty >= 3 {
			ctx.HID.Click(game.RightButton, screenPos.X, screenPos.Y)
			utils.Sleep(300)
			extracted += 3
			stackQty -= 3
		}

		// Use Ctrl+Click for remaining individual extractions
		for extracted < totalNeeded && stackQty > 0 {
			ctx.HID.ClickWithModifier(game.LeftButton, screenPos.X, screenPos.Y, game.CtrlKey)
			utils.Sleep(300)
			extracted++
			stackQty--
		}
	}

	step.CloseAllMenus()
	ctx.RefreshGameData()

	ctx.Logger.Info(fmt.Sprintf("Extracted %d rejuv potions from DLC Materials tab", min(extracted, totalNeeded)))
	return nil
}
