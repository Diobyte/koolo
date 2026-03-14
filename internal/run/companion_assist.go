package run

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/town"
	"github.com/hectorgimenez/koolo/internal/utils"
)

// CompanionAssist is a run that makes a follower follow the party leader,
// assist with killing monsters, and pick up items. Instead of running its own
// configured runs, the follower stays near the leader and helps with combat.
type CompanionAssist struct {
	ctx *context.Status
}

func NewCompanionAssist() *CompanionAssist {
	return &CompanionAssist{
		ctx: context.Get(),
	}
}

func (ca CompanionAssist) Name() string {
	return string(config.CompanionAssistRun)
}

func (ca CompanionAssist) CheckConditions(_ *RunParameters) SequencerResult {
	if !ca.ctx.CharacterCfg.Companion.Enabled || ca.ctx.CharacterCfg.Companion.Leader {
		return SequencerSkip
	}
	return SequencerOk
}

func (ca CompanionAssist) SkipTownRoutines() bool {
	return false
}

func (ca CompanionAssist) Run(_ *RunParameters) error {
	ca.ctx.SetLastAction("CompanionAssist")
	defer ca.ctx.WaitingForParty.Store(false)

	leaderName := ca.ctx.CharacterCfg.Companion.LeaderName
	if leaderName == "" {
		// AssistLeader requires a specific leader name to follow via roster
		ca.ctx.Logger.Warn("Companion assist: leaderName is empty, cannot follow unknown leader")
		return nil
	}

	ca.ctx.Logger.Info("Companion assist: following leader",
		slog.String("leader", leaderName))

	const (
		followDistance  = 10 // Move toward leader when farther than this
		combatRadius    = 15 // Kill monsters within this radius of player
		tickInterval    = 500 * time.Millisecond
		maxIdleTime     = 5 * time.Minute // Stop if leader not found for this long
		maxPathFailures = 8               // Give up on area after this many consecutive path failures
	)

	lastLeaderSeen := time.Now()
	consecutivePathFails := 0
	lastPathFailArea := area.ID(0)

	for {
		// Check if the bot has been stopped (e.g., global idle detection, chicken, death)
		if ca.ctx.ExecutionPriority >= context.PriorityStop {
			ca.ctx.Logger.Debug("Companion assist: bot stopped, ending run")
			return nil
		}

		// Check if the run context has been cancelled (game exit, chicken, death, timeout)
		if !ca.ctx.Manager.InGame() {
			ca.ctx.Logger.Debug("Companion assist: no longer in game, ending run")
			return nil
		}

		// Find leader in roster (game data is refreshed by background goroutine)
		leader, found := ca.ctx.Data.Roster.FindByName(leaderName)
		if !found {
			if time.Since(lastLeaderSeen) > maxIdleTime {
				ca.ctx.Logger.Warn("Companion assist: leader not found in roster for too long, ending run",
					slog.String("leader", leaderName))
				return nil
			}
			ca.ctx.SetLastAction("CompanionAssist:WaitingForLeader")
			ca.ctx.WaitingForParty.Store(true)
			utils.Sleep(int(tickInterval.Milliseconds()))
			continue
		}
		lastLeaderSeen = time.Now()

		// If leader is in town and we're not, go to town
		if leader.Area.IsTown() && !ca.ctx.Data.PlayerUnit.Area.IsTown() {
			ca.ctx.WaitingForParty.Store(false)
			ca.ctx.SetLastAction("CompanionAssist:ReturningToTown")
			ca.ctx.Logger.Debug("Companion assist: leader is in town, returning to town")
			if err := action.ReturnTown(); err != nil {
				ca.ctx.Logger.Warn("Companion assist: failed to return to town", slog.Any("error", err))
			}
			utils.Sleep(int(tickInterval.Milliseconds()))
			continue
		}

		// If both in town — check if same act. If different act, WP to leader's town.
		if leader.Area.IsTown() && ca.ctx.Data.PlayerUnit.Area.IsTown() {
			if leader.Area != ca.ctx.Data.PlayerUnit.Area {
				ca.ctx.WaitingForParty.Store(false)
				ca.ctx.SetLastAction("CompanionAssist:TravelingToLeaderTown")
				ca.ctx.Logger.Info("Companion assist: leader is in a different town, using waypoint",
					slog.String("leaderTown", leader.Area.Area().Name),
					slog.String("myTown", ca.ctx.Data.PlayerUnit.Area.Area().Name))
				if err := action.WayPoint(leader.Area); err != nil {
					ca.ctx.Logger.Warn("Companion assist: failed to WP to leader's town", slog.Any("error", err))
				}
			} else {
				ca.ctx.SetLastAction("CompanionAssist:IdleInTown")
				ca.ctx.WaitingForParty.Store(true)
			}
			utils.Sleep(int(tickInterval.Milliseconds()))
			continue
		}

		// Active gameplay from here — suppress stuck detection bypass
		ca.ctx.WaitingForParty.Store(false)

		// If leader entered a different area, try to follow via portal or area transition
		if leader.Area != ca.ctx.Data.PlayerUnit.Area {
			ca.ctx.SetLastAction("CompanionAssist:FollowingToArea")

			// Track consecutive path failures to the same area to avoid infinite retries
			if leader.Area != lastPathFailArea {
				consecutivePathFails = 0
				lastPathFailArea = leader.Area
			}

			if consecutivePathFails >= maxPathFailures {
				ca.ctx.Logger.Warn("Companion assist: too many path failures to leader area, waiting in town",
					slog.String("leaderArea", leader.Area.Area().Name),
					slog.Int("failures", consecutivePathFails))
				// Return to town and wait instead of endlessly retrying
				if !ca.ctx.Data.PlayerUnit.Area.IsTown() {
					_ = action.ReturnTown()
				}
				ca.ctx.WaitingForParty.Store(true)
				utils.Sleep(int(tickInterval.Milliseconds()))
				continue
			}

			if err := ca.followLeaderToArea(leader); err != nil {
				consecutivePathFails++
				ca.ctx.Logger.Debug("Companion assist: waiting for leader area transition",
					slog.String("leaderArea", leader.Area.Area().Name),
					slog.String("myArea", ca.ctx.Data.PlayerUnit.Area.Area().Name),
					slog.Int("pathFailures", consecutivePathFails))
				utils.Sleep(int(tickInterval.Milliseconds()))
				continue
			}
			consecutivePathFails = 0
			continue
		}

		// Successfully in the same area — reset path failure tracking
		consecutivePathFails = 0

		// Same area as leader — clear monsters and follow
		distToLeader := ca.ctx.PathFinder.DistanceFromMe(leader.Position)

		// Kill nearby monsters first
		ca.ctx.SetLastAction("CompanionAssist:Combat")
		if err := action.ClearAreaAroundPlayer(combatRadius, data.MonsterAnyFilter()); err != nil {
			ca.ctx.Logger.Debug("Companion assist: clear area error", slog.Any("error", err))
		}

		// Pick up items
		ca.ctx.SetLastAction("CompanionAssist:Pickup")
		action.ItemPickup(combatRadius)

		// Move toward leader if too far
		if distToLeader > followDistance {
			ca.ctx.SetLastAction("CompanionAssist:MovingToLeader")
			ca.ctx.Logger.Debug("Companion assist: moving toward leader",
				slog.Int("distance", distToLeader),
				slog.Any("leaderPos", leader.Position))
			if err := step.MoveTo(leader.Position, step.WithDistanceToFinish(followDistance)); err != nil {
				ca.ctx.Logger.Debug("Companion assist: move to leader failed", slog.Any("error", err))
			}
		}

		utils.Sleep(int(tickInterval.Milliseconds()))
	}
}

// followLeaderToArea attempts to reach the leader's area via adjacent walk, town portals, or waypoints.
func (ca CompanionAssist) followLeaderToArea(leader data.RosterMember) error {
	playerArea := ca.ctx.Data.PlayerUnit.Area

	// If leader is in an adjacent area, walk there directly
	for _, adj := range ca.ctx.Data.AreaData.AdjacentLevels {
		if adj.Area == leader.Area {
			ca.ctx.Logger.Debug("Companion assist: leader is in adjacent area, moving there",
				slog.String("area", leader.Area.Area().Name))
			return action.MoveToArea(leader.Area)
		}
	}

	// If we're in town, try to reach the leader's area
	if playerArea.IsTown() {
		return ca.travelFromTownToLeader(leader)
	}

	// We're in the field but not adjacent — return to town first, then figure it out
	ca.ctx.Logger.Debug("Companion assist: leader is in a different area, returning to town",
		slog.String("leaderArea", leader.Area.Area().Name))
	if err := action.ReturnTown(); err != nil {
		return err
	}
	return ca.travelFromTownToLeader(leader)
}

// travelFromTownToLeader handles reaching the leader from town. Tries leader's TP first,
// then uses waypoints if the leader is in a different act or no TP is available.
func (ca CompanionAssist) travelFromTownToLeader(leader data.RosterMember) error {
	playerArea := ca.ctx.Data.PlayerUnit.Area
	leaderAct := leader.Area.Act()
	myAct := playerArea.Act()

	// If different act, WP to the leader's act town first
	if myAct != leaderAct {
		leaderTown := town.GetTownByArea(leader.Area).TownArea()
		ca.ctx.Logger.Info("Companion assist: leader is in a different act, using waypoint to travel",
			slog.String("leaderArea", leader.Area.Area().Name),
			slog.Int("leaderAct", leaderAct))
		if err := action.WayPoint(leaderTown); err != nil {
			return fmt.Errorf("failed to WP to leader's act town: %w", err)
		}
	}

	// Now in the same act's town — try leader's TP
	if err := ca.useLeaderTP(); err == nil {
		return nil
	}

	// No TP found — try to WP to the closest waypoint to the leader's area
	wpDest := ca.findClosestWP(leader.Area)
	if wpDest == 0 {
		return fmt.Errorf("no waypoint route to leader area %s", leader.Area.Area().Name)
	}

	ca.ctx.Logger.Info("Companion assist: using waypoint to get near leader",
		slog.String("wp", wpDest.Area().Name),
		slog.String("leaderArea", leader.Area.Area().Name))
	if err := action.WayPoint(wpDest); err != nil {
		return fmt.Errorf("failed to WP near leader: %w", err)
	}
	return nil
}

// findClosestWP returns the best waypoint destination to reach the leader's area.
// If the leader's area itself has a WP, return it. Otherwise walk backwards through
// LinkedFrom chains to find a WP area that leads toward the leader.
func (ca CompanionAssist) findClosestWP(leaderArea area.ID) area.ID {
	// If leader's exact area has a waypoint, use it
	if _, ok := area.WPAddresses[leaderArea]; ok {
		return leaderArea
	}

	// Check if the leader's area appears in any WP's LinkedFrom chain (meaning
	// you can walk FROM that chain THROUGH the leader's area to reach the WP).
	// We want the WP whose LinkedFrom includes or passes through the leader's area.
	// The most practical approach: find any WP in the same act — the follower will
	// WP there and then be in range for the next loop iteration to use adjacent walk.
	leaderAct := leaderArea.Act()
	var bestWP area.ID
	bestRow := 0
	for wpArea, addr := range area.WPAddresses {
		if wpArea.Act() != leaderAct {
			continue
		}
		// Check if leader's area is in the LinkedFrom chain (direct connection)
		for _, linked := range addr.LinkedFrom {
			if linked == leaderArea {
				return wpArea
			}
		}
		// Track highest row in the same act as fallback (deepest WP)
		if addr.Row > bestRow {
			bestRow = addr.Row
			bestWP = wpArea
		}
	}
	return bestWP
}

// useLeaderTP walks to and enters the leader's town portal. Returns error if no TP found.
func (ca CompanionAssist) useLeaderTP() error {
	leaderName := ca.ctx.CharacterCfg.Companion.LeaderName

	// First pass: prefer leader's portal
	for _, obj := range ca.ctx.Data.Objects {
		if obj.IsPortal() && obj.Owner == leaderName {
			ca.ctx.Logger.Debug("Companion assist: found leader's town portal, entering")
			return action.InteractObject(obj, func() bool {
				return !ca.ctx.Data.PlayerUnit.Area.IsTown()
			})
		}
	}

	// Fallback: take any portal
	for _, obj := range ca.ctx.Data.Objects {
		if obj.IsPortal() {
			ca.ctx.Logger.Debug("Companion assist: found town portal, entering",
				slog.String("owner", obj.Owner))
			return action.InteractObject(obj, func() bool {
				return !ca.ctx.Data.PlayerUnit.Area.IsTown()
			})
		}
	}
	return fmt.Errorf("no town portal found")
}
