//go:build windows

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/quest"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/d2go/pkg/memory"
)

// ── ANSI ──────────────────────────────────────────────────────────────────────
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiWhite  = "\033[97m"
	ansiGray   = "\033[90m"
	ansiClear  = "\033[2J\033[H"
	w          = 80 // total box width including borders
)

// ── box helpers ───────────────────────────────────────────────────────────────
func top()    { fmt.Printf("╔%s╗\n", strings.Repeat("═", w-2)) }
func bottom() { fmt.Printf("╚%s╝\n", strings.Repeat("═", w-2)) }
func divider() {
	fmt.Printf("╠%s╣\n", strings.Repeat("═", w-2))
}
func midDivider(leftW int) {
	right := w - 2 - leftW - 1
	fmt.Printf("╠%s╤%s╣\n", strings.Repeat("═", leftW), strings.Repeat("═", right))
}
func bottomSplit(leftW int) {
	right := w - 2 - leftW - 1
	fmt.Printf("╚%s╧%s╝\n", strings.Repeat("═", leftW), strings.Repeat("═", right))
}

// row prints a full-width bordered row, padding to w-2 inner chars.
func row(content string) {
	inner := stripAnsi(content)
	pad := w - 2 - len(inner)
	if pad < 0 {
		pad = 0
	}
	fmt.Printf("║%s%s║\n", content, strings.Repeat(" ", pad))
}

// twoCol prints a bordered split row.
func twoCol(left, right string, leftW int) {
	lInner := stripAnsi(left)
	rInner := stripAnsi(right)
	rightW := w - 2 - leftW - 1
	lPad := leftW - len(lInner)
	rPad := rightW - len(rInner)
	if lPad < 0 {
		lPad = 0
	}
	if rPad < 0 {
		rPad = 0
	}
	fmt.Printf("║%s%s│%s%s║\n", left, strings.Repeat(" ", lPad), right, strings.Repeat(" ", rPad))
}

func header(title string) {
	centered := center(ansiBold+ansiCyan+title+ansiReset, w-2)
	fmt.Printf("║%s║\n", centered)
}

func sectionLabel(label string) {
	row(" " + ansiBold + ansiYellow + label + ansiReset)
}

// center pads s to width n accounting for invisible ANSI bytes.
func center(s string, n int) string {
	vis := len(stripAnsi(s))
	total := n - vis
	if total <= 0 {
		return s
	}
	left := total / 2
	right := total - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// stripAnsi removes escape sequences for length calculation.
func stripAnsi(s string) string {
	out := make([]byte, 0, len(s))
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b {
			esc = true
			continue
		}
		if esc {
			if c == 'm' {
				esc = false
			}
			continue
		}
		// skip multi-byte UTF-8 continuation bytes
		if c&0xC0 == 0x80 {
			continue
		}
		// box-drawing / emoji are ≥3 bytes; count as 1 visual char
		if c >= 0xE2 {
			out = append(out, ' ')
			i += 2
			continue
		}
		out = append(out, c)
	}
	return string(out)
}

// ── bar ───────────────────────────────────────────────────────────────────────
func bar(pct, barWidth int, fillColor string) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct * barWidth / 100
	empty := barWidth - filled
	return fillColor + strings.Repeat("█", filled) + ansiGray + strings.Repeat("░", empty) + ansiReset
}

// ── quest helpers ─────────────────────────────────────────────────────────────
var questShortNames = []string{
	"DenOfEvil", "SistersBurial", "ToolsOfTrade", "SearchForCain", "ForgottenTower", "SistersSlaughter",
	"RadamentsLair", "HoradricStaff", "TaintedSun", "ArcaneSanctuary", "TheSummoner", "SevenTombs",
	"LamEsensTome", "KhalimsWill", "BladeOldReligion", "GoldenBird", "BlackenedTemple", "TheGuardian",
	"FallenAngel", "HellForge", "TerrorsEnd",
	"SiegeHarrogath", "RescueArreat", "PrisonOfIce", "BetrayalHarrogath", "RiteOfPassage", "EveOfDestruction",
}

var questActBoundaries = []struct {
	start int
	label string
}{
	{0, "ACT I"}, {6, "ACT II"}, {12, "ACT III"}, {18, "ACT IV"}, {21, "ACT V"},
}

var questOrder = []quest.Quest{
	quest.Act1DenOfEvil, quest.Act1SistersBurialGrounds, quest.Act1ToolsOfTheTrade,
	quest.Act1TheSearchForCain, quest.Act1TheForgottenTower, quest.Act1SistersToTheSlaughter,
	quest.Act2RadamentsLair, quest.Act2TheHoradricStaff, quest.Act2TaintedSun,
	quest.Act2ArcaneSanctuary, quest.Act2TheSummoner, quest.Act2TheSevenTombs,
	quest.Act3LamEsensTome, quest.Act3KhalimsWill, quest.Act3BladeOfTheOldReligion,
	quest.Act3TheGoldenBird, quest.Act3TheBlackenedTemple, quest.Act3TheGuardian,
	quest.Act4TheFallenAngel, quest.Act4HellForge, quest.Act4TerrorsEnd,
	quest.Act5SiegeOnHarrogath, quest.Act5RescueOnMountArreat, quest.Act5PrisonOfIce,
	quest.Act5BetrayalOfHarrogath, quest.Act5RiteOfPassage, quest.Act5EveOfDestruction,
}

func questSymbol(s quest.Status) string {
	switch {
	case s.Completed():
		return ansiGreen + "✓" + ansiReset
	case !s.NotStarted():
		return ansiYellow + "·" + ansiReset
	default:
		return ansiGray + "-" + ansiReset
	}
}

// ── item quality color ────────────────────────────────────────────────────────
func qualityColor(q item.Quality) string {
	switch q {
	case item.QualityUnique:
		return ansiYellow
	case item.QualitySet:
		return ansiGreen
	case item.QualityRare:
		return "\033[93m"
	case item.QualityMagic:
		return "\033[34m"
	default:
		return ansiGray
	}
}

// ── npc name ──────────────────────────────────────────────────────────────────
func npcName(id npc.ID) string {
	if flags, ok := npc.MonStatsFlagsByID[id]; ok && flags.Name != "" {
		return flags.Name
	}
	return fmt.Sprintf("NPC#%d", id)
}

// ── render ────────────────────────────────────────────────────────────────────
func render(gr *memory.GameReader, sections map[string]bool) {
	d := gr.GetData()
	ts := time.Now().Format("2006-01-02 15:04:05")
	fmt.Print(ansiClear)

	// ── title ────────────────────────────────────────────────────────────────
	top()
	header(fmt.Sprintf("D2R MEMORY DUMP  ·  %s", ts))

	// ── player ───────────────────────────────────────────────────────────────
	if sections["player"] || sections["all"] {
		divider()
		sectionLabel("PLAYER")
		lvl, _ := d.PlayerUnit.FindStat(stat.Level, 0)
		classStr := fmt.Sprintf("%v", d.PlayerUnit.Class)
		nameStr := fmt.Sprintf("%s%-16s%s", ansiWhite, d.PlayerUnit.Name, ansiReset)
		areaStr := fmt.Sprintf("Area: %s%v%s", ansiCyan, d.PlayerUnit.Area, ansiReset)
		row(fmt.Sprintf(" %s  %s  Class: %s%-12s%s  Lvl: %s%d%s",
			nameStr, areaStr,
			ansiCyan, classStr, ansiReset,
			ansiWhite, lvl.Value, ansiReset))
		hp := d.PlayerUnit.HPPercent()
		mp := d.PlayerUnit.MPPercent()
		hpColor := ansiGreen
		if hp < 50 {
			hpColor = ansiYellow
		}
		if hp < 25 {
			hpColor = ansiRed
		}
		hpBar := bar(hp, 20, hpColor)
		mpBar := bar(mp, 20, "\033[34m")
		row(fmt.Sprintf("  HP [%s] %3d%%   MP [%s] %3d%%   Mode: %s%v%s",
			hpBar, hp, mpBar, mp, ansiGray, d.PlayerUnit.Mode, ansiReset))
	}

	// ── game ─────────────────────────────────────────────────────────────────
	if sections["game"] || sections["all"] {
		divider()
		sectionLabel("GAME")
		merc := ansiGray + "no " + ansiReset
		if d.HasMerc {
			merc = ansiGreen + "yes" + ansiReset
		}
		ingame := ansiGray + "no " + ansiReset
		if d.IsIngame {
			ingame = ansiGreen + "yes" + ansiReset
		}
		legacy := ansiGray + "no " + ansiReset
		if d.LegacyGraphics {
			legacy = ansiYellow + "yes" + ansiReset
		}
		row(fmt.Sprintf("  Game: %s%-20s%s  FPS: %s%3d%s  Ping: %s%4dms%s  Merc: %s  InGame: %s  Legacy: %s",
			ansiWhite, d.Game.LastGameName, ansiReset,
			ansiCyan, d.Game.FPS, ansiReset,
			ansiCyan, d.Game.Ping, ansiReset,
			merc, ingame, legacy))
	}

	// ── quests ───────────────────────────────────────────────────────────────
	if sections["quests"] || sections["all"] {
		divider()
		sectionLabel(fmt.Sprintf("QUESTS  %s✓ complete%s  %s· started%s  %s- not started%s",
			ansiGreen, ansiReset, ansiYellow, ansiReset, ansiGray, ansiReset))
		actIdx := 0
		for i, q := range questOrder {
			for actIdx+1 < len(questActBoundaries) && questActBoundaries[actIdx+1].start == i {
				actIdx++
			}
			if actIdx < len(questActBoundaries) && questActBoundaries[actIdx].start == i {
				row(fmt.Sprintf("  %s── %s ─────────────────────────────────────────%s",
					ansiGray, questActBoundaries[actIdx].label, ansiReset))
			}
			s := d.Quests[q]
			name := ""
			if i < len(questShortNames) {
				name = questShortNames[i]
			}
			sym := questSymbol(s)
			row(fmt.Sprintf("   %s %-22s  %sraw:0x%04X%s",
				sym, name, ansiGray, uint16(s), ansiReset))
		}
	}

	// ── monsters ─────────────────────────────────────────────────────────────
	if sections["monsters"] || sections["all"] {
		enemies := d.Monsters.Enemies()
		divider()
		sectionLabel(fmt.Sprintf("MONSTERS  %s%d total  %d alive%s",
			ansiCyan, len(d.Monsters), len(enemies), ansiReset))
		shown := 0
		for _, m := range d.Monsters {
			if shown >= 15 {
				row(fmt.Sprintf("  %s… %d more …%s", ansiGray, len(d.Monsters)-shown, ansiReset))
				break
			}
			maxLife := m.Stats[stat.MaxLife]
			life := m.Stats[stat.Life]
			lifePct := 0
			if maxLife > 0 {
				lifePct = life * 100 / maxLife
			}
			typeStr := fmt.Sprintf("%-10s", string(m.Type))
			typeColor := ansiGray
			if m.Type == data.MonsterTypeUnique || m.Type == data.MonsterTypeSuperUnique {
				typeColor = ansiYellow
			} else if m.Type == data.MonsterTypeChampion {
				typeColor = ansiCyan
			}
			lifeColor := ansiGreen
			if lifePct < 50 {
				lifeColor = ansiYellow
			}
			if lifePct < 25 {
				lifeColor = ansiRed
			}
			row(fmt.Sprintf("  %s%s%s %-22s  X:%-5d Y:%-5d  HP:%s%3d%%%s",
				typeColor, typeStr, ansiReset,
				npcName(m.Name),
				m.Position.X, m.Position.Y,
				lifeColor, lifePct, ansiReset))
			shown++
		}
		if len(d.Monsters) == 0 {
			row(fmt.Sprintf("  %sno monsters in range%s", ansiGray, ansiReset))
		}
	}

	// ── objects ──────────────────────────────────────────────────────────────
	if sections["objects"] || sections["all"] {
		divider()
		sectionLabel(fmt.Sprintf("OBJECTS  %s%d%s", ansiCyan, len(d.Objects), ansiReset))
		shown := 0
		for _, o := range d.Objects {
			if shown >= 10 {
				row(fmt.Sprintf("  %s… %d more …%s", ansiGray, len(d.Objects)-shown, ansiReset))
				break
			}
			sel := ansiGray + "[ ]" + ansiReset
			if o.Selectable {
				sel = ansiGreen + "[✓]" + ansiReset
			}
			row(fmt.Sprintf("  %s %-28v  X:%-5d Y:%d",
				sel, o.Name, o.Position.X, o.Position.Y))
			shown++
		}
		if len(d.Objects) == 0 {
			row(fmt.Sprintf("  %snone%s", ansiGray, ansiReset))
		}
	}

	// ── items ─────────────────────────────────────────────────────────────────
	if sections["items"] || sections["all"] {
		inv := d.Inventory.AllItems
		divider()
		sectionLabel(fmt.Sprintf("ITEMS  %s%d%s", ansiCyan, len(inv), ansiReset))
		shown := 0
		for _, it := range inv {
			if shown >= 12 {
				row(fmt.Sprintf("  %s… %d more …%s", ansiGray, len(inv)-shown, ansiReset))
				break
			}
			qc := qualityColor(it.Quality)
			row(fmt.Sprintf("  %s%-24s%s  %-10v  loc:%-10v",
				qc, it.Name, ansiReset, it.Quality.ToString(), it.Location.LocationType))
			shown++
		}
		if len(inv) == 0 {
			row(fmt.Sprintf("  %sempty%s", ansiGray, ansiReset))
		}
	}

	// ── menus (split with flags) ──────────────────────────────────────────────
	if sections["menus"] || sections["all"] {
		const lw = 38
		midDivider(lw)
		boolStr := func(b bool) string {
			if b {
				return ansiGreen + "YES" + ansiReset
			}
			return ansiGray + "no " + ansiReset
		}
		om := d.OpenMenus
		twoCol(
			fmt.Sprintf(" %sMENUS%s", ansiBold+ansiYellow, ansiReset),
			fmt.Sprintf(" %sFLAGS%s", ansiBold+ansiYellow, ansiReset),
			lw)
		twoCol(
			fmt.Sprintf("  Inventory:  %s  NPCShop: %s", boolStr(om.Inventory), boolStr(om.NPCShop)),
			fmt.Sprintf("  Ingame:  %s  Legacy: %s", boolStr(d.IsIngame), boolStr(d.LegacyGraphics)),
			lw)
		twoCol(
			fmt.Sprintf("  Stash:      %s  Waypoint:%s", boolStr(om.Stash), boolStr(om.Waypoint)),
			fmt.Sprintf("  Merc:    %s  MapShown:%s", boolStr(d.HasMerc), boolStr(om.MapShown)),
			lw)
		twoCol(
			fmt.Sprintf("  SkillTree:  %s  QuitMenu:%s", boolStr(om.SkillTree), boolStr(om.QuitMenu)),
			fmt.Sprintf("  FPS: %s%4d%s   Ping: %s%4dms%s", ansiCyan, d.Game.FPS, ansiReset, ansiCyan, d.Game.Ping, ansiReset),
			lw)
		bottomSplit(lw)
	} else {
		bottom()
	}
}

// ── main ─────────────────────────────────────────────────────────────────────
func main() {
	filter := flag.String("filter", "all", "sections: player,quests,monsters,items,objects,menus,game,all")
	watch := flag.Int("watch", 1, "refresh interval in seconds (0 = once and exit)")
	flag.Parse()

	sections := parseSections(*filter)

	proc, err := memory.NewProcess()
	if err != nil {
		fmt.Fprintf(os.Stderr, "attach failed: %v\n", err)
		os.Exit(1)
	}
	defer proc.Close()

	gr := memory.NewGameReader(proc)

	render(gr, sections)
	if *watch > 0 {
		ticker := time.NewTicker(time.Duration(*watch) * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			render(gr, sections)
		}
	}
}

func parseSections(s string) map[string]bool {
	m := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		m[strings.TrimSpace(strings.ToLower(part))] = true
	}
	return m
}
