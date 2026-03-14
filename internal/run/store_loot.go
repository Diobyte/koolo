package run

import (
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/event"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/hectorgimenez/koolo/internal/utils"
)

const (
	storeLootPickupDelay  = 300             // ms between item pickups
	storeLootScanInterval = 2000            // ms between ground scans when idle
	storeLootIdleTimeout  = 5 * time.Minute // exit game if no items for this long
	storeLootActionDelay  = 500             // ms between stash operations
)

type StoreLoot struct{}

func NewStoreLoot() StoreLoot {
	return StoreLoot{}
}

func (s StoreLoot) Name() string {
	return string(config.StoreLootRun)
}

func (s StoreLoot) CheckConditions(_ *RunParameters) SequencerResult {
	return SequencerOk
}

// SkipTownRoutines prevents the bot from running PreRun/PostRun town sequences.
func (s StoreLoot) SkipTownRoutines() bool {
	return true
}

func (s StoreLoot) Run(_ *RunParameters) error {
	ctx := context.Get()

	ctx.WaitForGameToLoad()
	if !ctx.Data.PlayerUnit.Area.IsTown() {
		ctx.Logger.Error("StoreLoot: must start in town")
		return nil
	}

	// Broadcast game info so farmers know where to drop items.
	// Use the config password directly since it's what we typed into the lobby;
	// LastGamePass() may not always reflect it from memory.
	gameName := ctx.GameReader.LastGameName()
	gamePassword := ctx.CharacterCfg.StoreLoot.GamePassword
	ctx.Logger.Info("StoreLoot: broadcasting game info to farmers", "game", gameName)
	event.Send(event.RequestStoreLootJoinGame(
		event.Text(ctx.Name, "StoreLoot game created"),
		ctx.Name, gameName, gamePassword,
	))

	// Ensure cleanup broadcast on exit.
	defer func() {
		event.Send(event.ResetStoreLootGameInfo(
			event.Text(ctx.Name, "StoreLoot game ended"),
			ctx.Name,
		))
	}()

	stashFull := false
	var stashFullSince time.Time
	lastItemTime := time.Now()

	for {
		ctx.RefreshGameData()

		if !ctx.GameReader.InGame() {
			ctx.Logger.Warn("StoreLoot: no longer in game, exiting run")
			return nil
		}

		groundItems := s.findGroundItems(ctx)

		if len(groundItems) > 0 {
			lastItemTime = time.Now()
			ctx.Logger.Info("StoreLoot: found items on ground", "count", len(groundItems))

			pickedAny := false
			for _, itm := range groundItems {
				// Check inventory space before picking up.
				if _, found := findInventorySpace(ctx, itm); !found {
					if stashFull {
						// Both stash and inventory full — cannot pick up more.
						break
					}
					ctx.Logger.Info("StoreLoot: inventory full, depositing to stash first")
					if err := s.depositToStash(ctx); err != nil {
						ctx.Logger.Error("StoreLoot: failed to deposit items", "error", err)
						return nil
					}
					// Re-check after depositing.
					if _, found := findInventorySpace(ctx, itm); !found {
						ctx.Logger.Warn("StoreLoot: all stash tabs and inventory full")
						stashFull = true
						stashFullSince = time.Now()

						// Signal farmers to stop sending items.
						event.Send(event.ResetStoreLootGameInfo(
							event.Text(ctx.Name, "StoreLoot mule stash full"),
							ctx.Name,
						))
						break
					}
				}

				if err := step.PickupItem(itm, 0); err != nil {
					ctx.Logger.Warn("StoreLoot: failed to pick up item", "item", itm.Name, "error", err)
				} else {
					pickedAny = true
				}
				utils.Sleep(storeLootPickupDelay)
				ctx.RefreshGameData()
			}

			// Deposit picked-up items to stash (skip if already known full).
			if !stashFull && pickedAny {
				if err := s.depositToStash(ctx); err != nil {
					ctx.Logger.Error("StoreLoot: failed to deposit after pickup", "error", err)
					return nil
				}

				if s.allStashTabsFull(ctx) {
					ctx.Logger.Warn("StoreLoot: all stash tabs full after deposit")
					stashFull = true
					stashFullSince = time.Now()

					// Signal farmers to stop sending items.
					event.Send(event.ResetStoreLootGameInfo(
						event.Text(ctx.Name, "StoreLoot mule stash full"),
						ctx.Name,
					))
				}
			}

			if stashFull {
				remaining := s.findGroundItems(ctx)
				if len(remaining) == 0 {
					ctx.Logger.Info("StoreLoot: ground clear, safe to exit with stash full")
					return s.handleStashFull(ctx)
				}
				// Grace period: farmers may still be in the game dropping items.
				// After 2 minutes with stash full and items stuck on the ground,
				// exit rather than spinning forever — these items cannot be saved.
				if time.Since(stashFullSince) > 2*time.Minute {
					ctx.Logger.Warn("StoreLoot: stash full grace period expired, items remain on ground",
						"remainingOnGround", len(remaining))
					return s.handleStashFull(ctx)
				}
			}
		} else {
			// No items on the ground.
			if stashFull {
				ctx.Logger.Info("StoreLoot: ground clear and stash full, exiting")
				return s.handleStashFull(ctx)
			}
			if time.Since(lastItemTime) > storeLootIdleTimeout {
				ctx.Logger.Info("StoreLoot: idle timeout reached, exiting game to re-create")
				return nil
			}
		}

		utils.Sleep(storeLootScanInterval)
	}
}

func (s StoreLoot) findGroundItems(ctx *context.Status) []data.Item {
	var items []data.Item
	for _, itm := range ctx.Data.Inventory.ByLocation(item.LocationGround) {
		items = append(items, itm)
	}
	return items
}

// depositToStash opens the stash and Ctrl+Clicks all inventory items into it.
// Tries personal stash (tab 1) first, then overflows to shared stash tabs.
func (s StoreLoot) depositToStash(ctx *context.Status) error {
	if err := action.OpenStash(); err != nil {
		return err
	}
	utils.Sleep(storeLootActionDelay)

	sharedPages := action.SharedStashPageCount(ctx)
	maxTab := 1 + sharedPages

	currentTab := 1
	action.SwitchStashTab(currentTab)
	utils.Sleep(storeLootActionDelay)

	ctx.RefreshGameData()

	items := ctx.Data.Inventory.ByLocation(item.LocationInventory)
	for _, itm := range items {
		screenPos := ui.GetScreenCoordsForItem(itm)

		// Attempt Ctrl+Click on current tab.
		ctx.HID.ClickWithModifier(game.LeftButton, screenPos.X, screenPos.Y, game.CtrlKey)
		utils.Sleep(storeLootActionDelay)
		ctx.RefreshGameData()

		// Check if item is still in inventory (deposit failed → tab full).
		if s.itemStillInInventory(ctx, itm.UnitID) {
			deposited := false
			for tab := currentTab + 1; tab <= maxTab; tab++ {
				action.SwitchStashTab(tab)
				currentTab = tab
				utils.Sleep(storeLootActionDelay)

				// Re-fetch screen position after tab switch.
				screenPos = ui.GetScreenCoordsForItem(itm)
				ctx.HID.ClickWithModifier(game.LeftButton, screenPos.X, screenPos.Y, game.CtrlKey)
				utils.Sleep(storeLootActionDelay)
				ctx.RefreshGameData()

				if !s.itemStillInInventory(ctx, itm.UnitID) {
					deposited = true
					break
				}
			}
			if !deposited {
				ctx.Logger.Info("StoreLoot: all stash tabs full, cannot deposit more")
				break
			}
		}
	}

	// Close stash.
	ctx.HID.PressKey(0x1B) // Escape
	utils.Sleep(storeLootActionDelay)
	ctx.RefreshGameData()

	return nil
}

// itemStillInInventory checks whether an item with the given UnitID is still
// in the player's inventory after a stash attempt.
func (s StoreLoot) itemStillInInventory(ctx *context.Status, unitID data.UnitID) bool {
	for _, it := range ctx.Data.Inventory.ByLocation(item.LocationInventory) {
		if it.UnitID == unitID {
			return true
		}
	}
	return false
}

// allStashTabsFull checks whether all stash tabs (personal + shared) lack a 2x2 free space.
func (s StoreLoot) allStashTabsFull(ctx *context.Status) bool {
	// Personal stash (LocationStash).
	if !isPrivateStashFull(ctx) {
		return false
	}

	// Shared stash pages (LocationSharedStash).
	sharedPages := action.SharedStashPageCount(ctx)
	allShared := ctx.Data.Inventory.ByLocation(item.LocationSharedStash)

	for page := 1; page <= sharedPages; page++ {
		var occupied [10][10]bool
		for _, it := range allShared {
			if it.Location.Page != page {
				continue
			}
			for y := 0; y < it.Desc().InventoryHeight; y++ {
				for x := 0; x < it.Desc().InventoryWidth; x++ {
					if it.Position.Y+y < 10 && it.Position.X+x < 10 {
						occupied[it.Position.Y+y][it.Position.X+x] = true
					}
				}
			}
		}
		// Check for a 2x2 free space on this page.
		for y := 0; y <= 8; y++ {
			for x := 0; x <= 8; x++ {
				if !occupied[y][x] && !occupied[y+1][x] && !occupied[y][x+1] && !occupied[y+1][x+1] {
					return false // Found space on this shared page.
				}
			}
		}
	}

	return true
}

// handleStashFull rotates to the next mule character when stash is full.
// If no more mule characters are available, performs a clean stop.
// Caller must ensure no items remain on the ground before calling this.
func (s StoreLoot) handleStashFull(ctx *context.Status) error {
	muleChars := ctx.CharacterCfg.StoreLoot.MuleCharacters
	ctx.CurrentGame.CurrentMuleIndex++

	if ctx.CurrentGame.CurrentMuleIndex < len(muleChars) {
		nextMule := muleChars[ctx.CurrentGame.CurrentMuleIndex]
		ctx.Logger.Info("StoreLoot: stash full, switching to next mule character",
			"next", nextMule, "index", ctx.CurrentGame.CurrentMuleIndex)
		ctx.CurrentGame.SwitchToCharacter = nextMule
		ctx.RestartWithCharacter = nextMule
	} else {
		ctx.Logger.Warn("StoreLoot: all mule characters are full, stopping")
	}

	ctx.CleanStopRequested = true
	ctx.CharacterCfg.KillD2OnStop = true
	if err := ctx.Manager.ExitGame(); err != nil {
		ctx.Logger.Error("StoreLoot: failed to exit game", "error", err)
	}
	utils.Sleep(2000)
	ctx.StopSupervisor()
	return nil
}
