package character

import (
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/difficulty"
	"github.com/hectorgimenez/d2go/pkg/data/mode"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/d2go/pkg/data/state"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/health"
	"github.com/hectorgimenez/koolo/internal/pather"
	"github.com/hectorgimenez/koolo/internal/utils"
)

const (
	sorceressMaxAttacksLoop         = 40
	coldImmuneMaxAttacks            = 12 // Reduced budget for cold immunes — let merc handle
	minBlizzSorceressAttackDistance = 8
	maxBlizzSorceressAttackDistance = 16
	dangerDistance                  = 8  // Monsters closer than this are considered dangerous
	safeDistance                    = 10 // Distance to teleport away to
)

type BlizzardSorceress struct {
	BaseCharacter
}

func (s BlizzardSorceress) ShouldIgnoreMonster(m data.Monster) bool {
	return false
}

func (s BlizzardSorceress) CheckKeyBindings() []skill.ID {
	requireKeybindings := []skill.ID{skill.Blizzard, skill.Teleport, skill.TomeOfTownPortal, skill.ShiverArmor, skill.StaticField}
	missingKeybindings := []skill.ID{}

	for _, cskill := range requireKeybindings {
		if _, found := s.Data.KeyBindings.KeyBindingForSkill(cskill); !found {
			switch cskill {
			// Since we can have one of 3 armors:
			case skill.ShiverArmor:
				_, found1 := s.Data.KeyBindings.KeyBindingForSkill(skill.FrozenArmor)
				_, found2 := s.Data.KeyBindings.KeyBindingForSkill(skill.ChillingArmor)
				if !found1 && !found2 {
					missingKeybindings = append(missingKeybindings, skill.ShiverArmor)
				}
			default:
				missingKeybindings = append(missingKeybindings, cskill)
			}
		}
	}

	if len(missingKeybindings) > 0 {
		s.Logger.Debug("There are missing required key bindings.", slog.Any("Bindings", missingKeybindings))
	}

	return missingKeybindings
}

func (s BlizzardSorceress) KillMonsterSequence(
	monsterSelector func(d game.Data) (data.UnitID, bool),
	skipOnImmunities []stat.Resist,
) error {
	completedAttackLoops := 0
	previousUnitID := 0
	lastReposition := time.Now()

	attackOpts := step.StationaryDistance(minBlizzSorceressAttackDistance, maxBlizzSorceressAttackDistance)

	for {
		context.Get().PauseIfNotPriority()

		if s.Data.PlayerUnit.IsDead() {
			s.Logger.Info("Player detected as dead during KillMonsterSequence, stopping actions.")
			time.Sleep(500 * time.Millisecond)
			return health.ErrDied
		}

		// Reposition when enemies are dangerously close (do this before target selection
		// so we move first, then pick the best target from the new position)
		tooClose, _ := s.needsRepositioning()
		if tooClose && time.Since(lastReposition) > time.Second*1 {
			lastReposition = time.Now()

			targetID, found := monsterSelector(*s.Data)
			if !found {
				return nil
			}
			targetMonster, found := s.Data.Monsters.FindByID(targetID)
			if !found {
				return nil
			}

			safePos, posFound := s.findSafePosition(targetMonster)
			if posFound {
				step.MoveTo(safePos, step.WithIgnoreMonsters())
			}
			// Fall through to attack — never skip the attack phase
		}

		id, found := monsterSelector(*s.Data)
		if !found {
			return nil
		}

		if previousUnitID != int(id) {
			completedAttackLoops = 0
		}

		if !s.preBattleChecks(id, skipOnImmunities) {
			return nil
		}

		monster, found := s.Data.Monsters.FindByID(id)
		if !found {
			s.Logger.Info("Monster not found", slog.String("monster", fmt.Sprintf("%v", monster)))
			return nil
		}

		isColdImmune := monster.IsImmune(stat.ColdImmune)
		maxLoops := sorceressMaxAttacksLoop
		if isColdImmune {
			maxLoops = coldImmuneMaxAttacks
		}
		if completedAttackLoops >= maxLoops {
			return nil
		}

		// Attack from current position
		if isColdImmune {
			// Let merc handle cold immunes — just primary attack
			step.PrimaryAttack(id, 1, true, attackOpts)
		} else {
			if s.Data.PlayerUnit.States.HasState(state.Cooldown) {
				step.PrimaryAttack(id, 2, true, attackOpts)
			}
			step.SecondaryAttack(skill.Blizzard, id, 1, attackOpts)
		}

		completedAttackLoops++
		previousUnitID = int(id)
	}
}

func (s BlizzardSorceress) killMonster(npc npc.ID, t data.MonsterType) error {
	return s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
		m, found := d.Monsters.FindOne(npc, t)
		if !found {
			return 0, false
		}

		return m.UnitID, true
	}, nil)
}

func (s BlizzardSorceress) killMonsterByName(id npc.ID, monsterType data.MonsterType, skipOnImmunities []stat.Resist) error {
	// while the monster is alive, keep attacking it
	for {
		if m, found := s.Data.Monsters.FindOne(id, monsterType); found {
			if m.Stats[stat.Life] <= 0 {
				break
			}

			s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
				if m, found := d.Monsters.FindOne(id, monsterType); found {
					return m.UnitID, true
				}

				return 0, false
			}, skipOnImmunities)
		} else {
			break
		}
	}
	return nil
}

func (s BlizzardSorceress) BuffSkills() []skill.ID {
	skillsList := make([]skill.ID, 0)
	if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.EnergyShield); found {
		skillsList = append(skillsList, skill.EnergyShield)
	}

	armors := []skill.ID{skill.ChillingArmor, skill.ShiverArmor, skill.FrozenArmor}
	for _, armor := range armors {
		if _, found := s.Data.KeyBindings.KeyBindingForSkill(armor); found {
			skillsList = append(skillsList, armor)
			return skillsList
		}
	}

	return skillsList
}

func (s BlizzardSorceress) PreCTABuffSkills() []skill.ID {
	return []skill.ID{}
}

func (s BlizzardSorceress) KillCountess() error {
	return s.killMonsterByName(npc.DarkStalker, data.MonsterTypeSuperUnique, nil)
}

func (s BlizzardSorceress) KillAndariel() error {
	return s.killMonsterByName(npc.Andariel, data.MonsterTypeUnique, nil)
}

func (s BlizzardSorceress) KillSummoner() error {
	return s.killMonsterByName(npc.Summoner, data.MonsterTypeUnique, nil)
}

func (s BlizzardSorceress) KillDuriel() error {
	return s.killMonsterByName(npc.Duriel, data.MonsterTypeUnique, nil)
}

func (s BlizzardSorceress) KillCouncil() error {
	return s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
		// Exclude monsters that are not council members
		var councilMembers []data.Monster
		var coldImmunes []data.Monster
		for _, m := range d.Monsters.Enemies() {
			if m.Name == npc.CouncilMember || m.Name == npc.CouncilMember2 || m.Name == npc.CouncilMember3 {
				if m.IsImmune(stat.ColdImmune) {
					coldImmunes = append(coldImmunes, m)
				} else {
					councilMembers = append(councilMembers, m)
				}
			}
		}

		councilMembers = append(councilMembers, coldImmunes...)

		for _, m := range councilMembers {
			return m.UnitID, true
		}

		return 0, false
	}, nil)
}

/*
func (s BlizzardSorceress) KillMephisto() error {
    // Find Mephisto
    mephisto, found := s.Data.Monsters.FindOne(npc.Mephisto, data.MonsterTypeUnique)
    if !found || mephisto.Stats[stat.Life] <= 0 {
        // If Mephisto is not found or already dead, just return (or handle as needed)
        return nil
    }

    s.Logger.Info("Mephisto detected, applying Static Field")

    // Apply Static Field to Mephisto
    // The parameters (unitID, attacks, distance options) are similar to Diablo's Static Field usage
    _ = step.SecondaryAttack(skill.StaticField, mephisto.UnitID, 5, step.Distance(3, 8))

    // Now, proceed with the regular monster killing sequence (Blizzard etc.)
    return s.killMonsterByName(npc.Mephisto, data.MonsterTypeUnique, nil)
}
*/

func (s BlizzardSorceress) KillMephisto() error {

	if s.CharacterCfg.Character.BlizzardSorceress.UseStaticOnMephisto {

		staticFieldRange := step.Distance(0, 4)
		var attackOption step.AttackOption = step.Distance(SorceressLevelingMinDistance, SorceressLevelingMaxDistance)
		err := step.MoveTo(data.Position{X: 17563, Y: 8072}, step.WithIgnoreMonsters())
		if err != nil {
			return err
		}

		monster, found := s.Data.Monsters.FindOne(npc.Mephisto, data.MonsterTypeUnique)
		if !found {
			s.Logger.Error("Mephisto not found at initial approach, aborting kill.")
			return nil
		}

		if s.Data.PlayerUnit.Skills[skill.Blizzard].Level > 0 {
			s.Logger.Info("Applying initial Blizzard cast.")
			step.SecondaryAttack(skill.Blizzard, monster.UnitID, 1, attackOption)
			time.Sleep(time.Millisecond * 300) // Wait for cast to register and apply chill
		}

		canCastStaticField := s.Data.PlayerUnit.Skills[skill.StaticField].Level > 0
		_, isStaticFieldBound := s.Data.KeyBindings.KeyBindingForSkill(skill.StaticField)

		if canCastStaticField && isStaticFieldBound {
			s.Logger.Info("Starting aggressive Static Field phase on Mephisto.")

			requiredLifePercent := 0.0
			switch s.CharacterCfg.Game.Difficulty {
			case difficulty.Normal, difficulty.Nightmare:
				requiredLifePercent = 40.0
			case difficulty.Hell:
				requiredLifePercent = 70.0
			}

			maxStaticAttacks := 50
			staticAttackCount := 0

			for staticAttackCount < maxStaticAttacks {
				monster, found = s.Data.Monsters.FindOne(npc.Mephisto, data.MonsterTypeUnique)
				if !found || monster.Stats[stat.Life] <= 0 {
					s.Logger.Info("Mephisto died or vanished during Static Phase.")
					break
				}

				monsterLifePercent := float64(monster.Stats[stat.Life]) / float64(monster.Stats[stat.MaxLife]) * 100

				if monsterLifePercent <= requiredLifePercent {
					s.Logger.Info(fmt.Sprintf("Mephisto life threshold (%.0f%%) reached. Transitioning to moat movement.", requiredLifePercent))
					break
				}

				distanceToMonster := pather.DistanceFromPoint(s.Data.PlayerUnit.Position, monster.Position)

				if distanceToMonster > StaticFieldEffectiveRange && s.Data.PlayerUnit.Skills[skill.Teleport].Level > 0 {
					s.Logger.Debug("Mephisto too far for Static Field, repositioning closer.")

					step.MoveTo(monster.Position, step.WithIgnoreMonsters())
					utils.Sleep(150)
					continue
				}

				if s.Data.PlayerUnit.Mode != mode.CastingSkill {
					s.Logger.Debug("Using Static Field on Mephisto.")
					step.SecondaryAttack(skill.StaticField, monster.UnitID, 1, staticFieldRange)
					time.Sleep(time.Millisecond * 150)
				} else {
					time.Sleep(time.Millisecond * 50)
				}
				staticAttackCount++
			}
		} else {
			s.Logger.Info("Static Field not available or bound, skipping Static Phase.")
		}

		err = step.MoveTo(data.Position{X: 17563, Y: 8072}, step.WithIgnoreMonsters())
		if err != nil {
			return err
		}

	}

	if !s.CharacterCfg.Character.BlizzardSorceress.UseMoatTrick {

		return s.killMonsterByName(npc.Mephisto, data.MonsterTypeUnique, nil)

	} else {

		ctx := context.Get()
		opts := step.Distance(15, 80)
		ctx.ForceAttack = true

		defer func() {
			ctx.ForceAttack = false
		}()

		type positionAndWaitTime struct {
			x        int
			y        int
			duration int
		}

		// Move to initial position
		utils.Sleep(350)
		err := step.MoveTo(data.Position{X: 17563, Y: 8072}, step.WithIgnoreMonsters())
		if err != nil {
			return err
		}

		utils.Sleep(350)

		// Initial movement sequence
		initialPositions := []positionAndWaitTime{
			{17575, 8086, 350}, {17584, 8088, 1200},
			{17600, 8090, 550}, {17609, 8090, 2500},
		}

		for _, pos := range initialPositions {
			err := step.MoveTo(data.Position{X: pos.x, Y: pos.y}, step.WithIgnoreMonsters())
			if err != nil {
				return err
			}
			utils.Sleep(pos.duration)
		}

		// Clear area around position
		err = action.ClearAreaAroundPosition(data.Position{X: 17609, Y: 8090}, 10, data.MonsterAnyFilter())
		if err != nil {
			return err
		}

		err = step.MoveTo(data.Position{X: 17609, Y: 8090}, step.WithIgnoreMonsters())
		if err != nil {
			return err
		}

		maxAttack := 100
		attackCount := 0

		for attackCount < maxAttack {
			ctx.PauseIfNotPriority()

			monster, found := s.Data.Monsters.FindOne(npc.Mephisto, data.MonsterTypeUnique)

			if !found {
				return nil
			}

			if s.Data.PlayerUnit.States.HasState(state.Cooldown) {
				step.PrimaryAttack(monster.UnitID, 2, true, opts)
				utils.Sleep(50)
			}

			step.SecondaryAttack(skill.Blizzard, monster.UnitID, 1, opts)
			utils.Sleep(100)
			attackCount++
		}
		return nil

	}
}

func (s BlizzardSorceress) KillIzual() error {
	m, found := s.Data.Monsters.FindOne(npc.Izual, data.MonsterTypeUnique)
	if !found {
		s.Logger.Error("Izual not found")
		return nil
	}
	_ = step.SecondaryAttack(skill.StaticField, m.UnitID, 4, step.Distance(5, 8))

	return s.killMonsterByName(npc.Izual, data.MonsterTypeUnique, nil)
}

func (s BlizzardSorceress) KillDiablo() error {
	timeout := time.Second * 20
	startTime := time.Now()
	diabloFound := false

	for {
		if time.Since(startTime) > timeout && !diabloFound {
			s.Logger.Error("Diablo was not found, timeout reached")
			return nil
		}

		diablo, found := s.Data.Monsters.FindOne(npc.Diablo, data.MonsterTypeUnique)
		if !found || diablo.Stats[stat.Life] <= 0 {
			// Already dead
			if diabloFound {
				return nil
			}

			// Keep waiting...
			time.Sleep(200 * time.Millisecond)
			continue
		}

		diabloFound = true
		s.Logger.Info("Diablo detected, attacking")

		_ = step.SecondaryAttack(skill.StaticField, diablo.UnitID, 5, step.Distance(3, 8))

		return s.killMonsterByName(npc.Diablo, data.MonsterTypeUnique, nil)
	}
}

func (s BlizzardSorceress) KillPindle() error {
	return s.killMonsterByName(npc.DefiledWarrior, data.MonsterTypeSuperUnique, s.CharacterCfg.Game.Pindleskin.SkipOnImmunities)
}

func (s BlizzardSorceress) KillNihlathak() error {
	return s.killMonsterByName(npc.Nihlathak, data.MonsterTypeSuperUnique, nil)
}

func (s BlizzardSorceress) KillBaal() error {
	m, found := s.Data.Monsters.FindOne(npc.BaalCrab, data.MonsterTypeUnique)
	if !found {
		s.Logger.Error("Baal not found")
		return nil
	}
	step.SecondaryAttack(skill.StaticField, m.UnitID, 4, step.Distance(5, 8))

	return s.killMonsterByName(npc.BaalCrab, data.MonsterTypeUnique, nil)
}

func (s BlizzardSorceress) needsRepositioning() (bool, data.Monster) {
	for _, monster := range s.Data.Monsters.Enemies() {
		if monster.Stats[stat.Life] <= 0 {
			continue
		}

		distance := pather.DistanceFromPoint(s.Data.PlayerUnit.Position, monster.Position)
		if distance < dangerDistance {
			return true, monster
		}
	}

	return false, data.Monster{}
}

func (s BlizzardSorceress) findSafePosition(targetMonster data.Monster) (data.Position, bool) {
	ctx := context.Get()
	playerPos := s.Data.PlayerUnit.Position

	// Generate candidate positions in a ring around the TARGET at attack range.
	// This ensures we teleport to a position where we can actually cast, rather than
	// generating escape positions around the player that may not be in range.
	type scoredPosition struct {
		pos   data.Position
		score float64
	}

	var scoredPositions []scoredPosition

	// Collect alive enemies once for scoring
	aliveEnemies := make([]data.Monster, 0)
	for _, m := range s.Data.Monsters.Enemies() {
		if m.Stats[stat.Life] > 0 {
			aliveEnemies = append(aliveEnemies, m)
		}
	}

	// Generate ring of candidates at attack range (8-16 tiles) around the target
	for angle := 0; angle < 360; angle += 15 {
		radians := float64(angle) * math.Pi / 180
		for dist := minBlizzSorceressAttackDistance; dist <= maxBlizzSorceressAttackDistance; dist += 4 {
			dx := int(math.Cos(radians) * float64(dist))
			dy := int(math.Sin(radians) * float64(dist))

			pos := data.Position{
				X: targetMonster.Position.X + dx,
				Y: targetMonster.Position.Y + dy,
			}

			if !s.Data.AreaData.IsWalkable(pos) {
				continue
			}

			// Must have line of sight to the target
			if !ctx.PathFinder.LineOfSight(pos, targetMonster.Position) {
				continue
			}

			// Calculate minimum distance to any alive enemy
			minMonsterDist := math.MaxFloat64
			for _, m := range aliveEnemies {
				d := float64(pather.DistanceFromPoint(pos, m.Position))
				if d < minMonsterDist {
					minMonsterDist = d
				}
			}

			// Skip positions that are too close to any monster
			if minMonsterDist < float64(dangerDistance) {
				continue
			}

			// Distance from player — shorter teleport is preferable
			playerDist := float64(pather.DistanceFromPoint(pos, playerPos))

			// Score: balance safety with short teleports for speed
			score := minMonsterDist*2.0 - playerDist*1.0

			scoredPositions = append(scoredPositions, scoredPosition{pos: pos, score: score})
		}
	}

	// If no positions in the ring work (tight spaces, surrounded), try positions
	// in the opposite direction from the closest threat as a fallback escape
	if len(scoredPositions) == 0 {
		closestEnemy := data.Position{}
		closestDist := math.MaxFloat64
		for _, m := range aliveEnemies {
			d := float64(pather.DistanceFromPoint(playerPos, m.Position))
			if d < closestDist {
				closestDist = d
				closestEnemy = m.Position
			}
		}

		// Flee direction: away from closest enemy
		vx := float64(playerPos.X - closestEnemy.X)
		vy := float64(playerPos.Y - closestEnemy.Y)
		length := math.Sqrt(vx*vx + vy*vy)
		if length > 0 {
			vx /= length
			vy /= length
		} else {
			vx, vy = 1, 0
		}

		for dist := safeDistance; dist <= safeDistance+6; dist += 2 {
			for spread := -3; spread <= 3; spread++ {
				pos := data.Position{
					X: playerPos.X + int(vx*float64(dist)) + spread,
					Y: playerPos.Y + int(vy*float64(dist)) + spread,
				}
				if !s.Data.AreaData.IsWalkable(pos) {
					continue
				}

				minMonsterDist := math.MaxFloat64
				for _, m := range aliveEnemies {
					d := float64(pather.DistanceFromPoint(pos, m.Position))
					if d < minMonsterDist {
						minMonsterDist = d
					}
				}

				playerDist := float64(pather.DistanceFromPoint(pos, playerPos))
				score := minMonsterDist*2.0 - playerDist*1.0

				scoredPositions = append(scoredPositions, scoredPosition{pos: pos, score: score})
			}
		}
	}

	// Sort by score (highest = safest)
	sort.Slice(scoredPositions, func(i, j int) bool {
		return scoredPositions[i].score > scoredPositions[j].score
	})

	if len(scoredPositions) > 0 {
		return scoredPositions[0].pos, true
	}

	return data.Position{}, false
}
