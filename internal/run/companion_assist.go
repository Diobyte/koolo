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

	leaderName := ca.ctx.CharacterCfg.Companion.LeaderName

	ca.ctx.Logger.Info("Companion assist: following leader",
		slog.String("leader", leaderName))

	const (
		followDistance = 10 // Move toward leader when farther than this
		combatRadius   = 15 // Kill monsters within this radius of player
		tickInterval   = 500 * time.Millisecond
		maxIdleTime    = 5 * time.Minute // Stop if leader not found for this long
		useTPDistance  = 40              // If leader is very far, try using their TP
	)

	lastLeaderSeen := time.Now()
	var lastLeaderArea area.ID

	for {
		ca.ctx.RefreshGameData()

		// Find leader in roster
		leader, found := ca.ctx.Data.Roster.FindByName(leaderName)
		if !found {
			if time.Since(lastLeaderSeen) > maxIdleTime {
				ca.ctx.Logger.Warn("Companion assist: leader not found in roster for too long, ending run",
					slog.String("leader", leaderName))
				return nil
			}
			utils.Sleep(int(tickInterval.Milliseconds()))
			continue
		}
		lastLeaderSeen = time.Now()

		// If leader is in town and we're not, go to town
		if leader.Area.IsTown() && !ca.ctx.Data.PlayerUnit.Area.IsTown() {
			ca.ctx.Logger.Debug("Companion assist: leader is in town, returning to town")
			if err := action.ReturnTown(); err != nil {
				ca.ctx.Logger.Warn("Companion assist: failed to return to town", slog.Any("error", err))
			}
			utils.Sleep(int(tickInterval.Milliseconds()))
			continue
		}

		// If we're in town and leader is in town, wait
		if leader.Area.IsTown() && ca.ctx.Data.PlayerUnit.Area.IsTown() {
			utils.Sleep(int(tickInterval.Milliseconds()))
			continue
		}

		// If leader entered a different area, try to follow via portal or area transition
		if leader.Area != ca.ctx.Data.PlayerUnit.Area {
			if err := ca.followLeaderToArea(leader, lastLeaderArea); err != nil {
				ca.ctx.Logger.Debug("Companion assist: waiting for leader area transition",
					slog.String("leaderArea", leader.Area.Area().Name),
					slog.String("myArea", ca.ctx.Data.PlayerUnit.Area.Area().Name))
				utils.Sleep(int(tickInterval.Milliseconds()))
				continue
			}
			lastLeaderArea = leader.Area
			continue
		}
		lastLeaderArea = leader.Area

		// Same area as leader — clear monsters and follow
		distToLeader := ca.ctx.PathFinder.DistanceFromMe(leader.Position)

		// Kill nearby monsters first
		if err := action.ClearAreaAroundPlayer(combatRadius, data.MonsterAnyFilter()); err != nil {
			ca.ctx.Logger.Debug("Companion assist: clear area error", slog.Any("error", err))
		}

		// Pick up items
		action.ItemPickup(combatRadius)

		// Move toward leader if too far
		if distToLeader > followDistance {
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

// followLeaderToArea attempts to reach the leader's area via town portals or area transitions.
func (ca CompanionAssist) followLeaderToArea(leader data.RosterMember, lastLeaderArea area.ID) error {
	// If we're in town and leader is not, look for a TP to take
	if ca.ctx.Data.PlayerUnit.Area.IsTown() && !leader.Area.IsTown() {
		ca.ctx.Logger.Debug("Companion assist: leader left town, looking for town portal",
			slog.String("leaderArea", leader.Area.Area().Name))
		if err := ca.useNearestTP(); err != nil {
			return fmt.Errorf("no portal to leader's area: %w", err)
		}
		return nil
	}

	// If leader is in an adjacent area, try to walk there
	for _, adj := range ca.ctx.Data.AreaData.AdjacentLevels {
		if adj.Area == leader.Area {
			ca.ctx.Logger.Debug("Companion assist: leader is in adjacent area, moving there",
				slog.String("area", leader.Area.Area().Name))
			return action.MoveToArea(leader.Area)
		}
	}

	// If leader went to a completely different area, return to town and use TP
	if !ca.ctx.Data.PlayerUnit.Area.IsTown() {
		ca.ctx.Logger.Debug("Companion assist: leader is in a different area, returning to town",
			slog.String("leaderArea", leader.Area.Area().Name))
		if err := action.ReturnTown(); err != nil {
			return err
		}
		return ca.useNearestTP()
	}

	return fmt.Errorf("cannot reach leader area %s", leader.Area.Area().Name)
}

// useNearestTP walks to and enters the nearest town portal.
func (ca CompanionAssist) useNearestTP() error {
	for _, obj := range ca.ctx.Data.Objects {
		if obj.IsPortal() {
			ca.ctx.Logger.Debug("Companion assist: found town portal, entering")
			return action.InteractObject(obj, func() bool {
				return !ca.ctx.Data.PlayerUnit.Area.IsTown()
			})
		}
	}
	return fmt.Errorf("no town portal found")
}
