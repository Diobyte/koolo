//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/entrance"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/quest"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/d2go/pkg/memory"
	"github.com/hectorgimenez/d2go/pkg/utils"
)

//go:embed dashboard.html
var dashboardHTML []byte

// ── JSON response types ──────────────────────────────────────────────────────

type apiResponse struct {
	OK        bool          `json:"ok"`
	Timestamp string        `json:"ts"`
	Player    playerInfo    `json:"player"`
	Game      gameInfo      `json:"game"`
	Quests    []questInfo   `json:"quests"`
	Monsters  []monsterInfo `json:"monsters"`
	Items     []itemInfo    `json:"items"`
	Objects   []objectInfo  `json:"objects"`
	Menus     menuInfo      `json:"menus"`
	Error     string        `json:"error,omitempty"`

	// Extended data for technical view
	PlayerStats playerStatsInfo  `json:"playerStats"`
	PlayerPos   positionInfo     `json:"playerPos"`
	AreaOrigin  positionInfo     `json:"areaOrigin"`
	Skills      []skillInfo      `json:"skills"`
	States      []string         `json:"states"`
	Corpse      corpseInfo       `json:"corpse"`
	Corpses     []monsterInfo    `json:"corpses"`
	Entrances   []entranceInfo   `json:"entrances"`
	AdjLevels   []adjLevelInfo   `json:"adjLevels"`
	Rooms       []roomInfo       `json:"rooms"`
	TerrorZones []terrorZoneInfo `json:"terrorZones"`
	Roster      []rosterInfo     `json:"roster"`
	Hover       hoverInfo        `json:"hover"`
	Gold        goldInfo         `json:"gold"`
	WeaponSlot  int              `json:"weaponSlot"`
	MercHP      int              `json:"mercHP"`

	// Collision grid for the current area
	CollisionGrid *collisionGridInfo `json:"collisionGrid,omitempty"`
}

type playerInfo struct {
	Name  string `json:"name"`
	Class string `json:"class"`
	Level int    `json:"level"`
	Area  string `json:"area"`
	HP    int    `json:"hp"`
	MP    int    `json:"mp"`
	Mode  string `json:"mode"`
	Dead  bool   `json:"dead"`
}

type gameInfo struct {
	Name    string `json:"name"`
	FPS     int    `json:"fps"`
	Ping    int    `json:"ping"`
	HasMerc bool   `json:"hasMerc"`
	InGame  bool   `json:"inGame"`
	Legacy  bool   `json:"legacy"`
}

type questInfo struct {
	Act    string `json:"act"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Raw    uint16 `json:"raw"`
}

type monsterInfo struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	HPPct int    `json:"hpPct"`
}

type itemInfo struct {
	Name       string `json:"name"`
	Quality    string `json:"quality"`
	Location   string `json:"location"`
	Ethereal   bool   `json:"ethereal"`
	Identified bool   `json:"identified"`
	Sockets    int    `json:"sockets"`
	Runeword   string `json:"runeword,omitempty"`
	LevelReq   int    `json:"levelReq"`
}

type objectInfo struct {
	Name       string `json:"name"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	Selectable bool   `json:"selectable"`
}

type menuInfo struct {
	Inventory     bool `json:"inventory"`
	Stash         bool `json:"stash"`
	SkillTree     bool `json:"skillTree"`
	NPCShop       bool `json:"npcShop"`
	Waypoint      bool `json:"waypoint"`
	QuitMenu      bool `json:"quitMenu"`
	MapShown      bool `json:"mapShown"`
	Character     bool `json:"character"`
	Cube          bool `json:"cube"`
	MercInventory bool `json:"mercInventory"`
	QuestLog      bool `json:"questLog"`
	ChatOpen      bool `json:"chatOpen"`
	LoadingScreen bool `json:"loadingScreen"`
	Cinematic     bool `json:"cinematic"`
}

type playerStatsInfo struct {
	Strength  int `json:"strength"`
	Dexterity int `json:"dexterity"`
	Vitality  int `json:"vitality"`
	Energy    int `json:"energy"`

	Life    int `json:"life"`
	MaxLife int `json:"maxLife"`
	Mana    int `json:"mana"`
	MaxMana int `json:"maxMana"`
	Stamina int `json:"stamina"`
	MaxStam int `json:"maxStamina"`

	FireRes      int `json:"fireRes"`
	MaxFireRes   int `json:"maxFireRes"`
	ColdRes      int `json:"coldRes"`
	MaxColdRes   int `json:"maxColdRes"`
	LightRes     int `json:"lightRes"`
	MaxLightRes  int `json:"maxLightRes"`
	PoisonRes    int `json:"poisonRes"`
	MaxPoisonRes int `json:"maxPoisonRes"`
	MagicRes     int `json:"magicRes"`

	Defense int `json:"defense"`
	FCR     int `json:"fcr"`
	FHR     int `json:"fhr"`
	FRW     int `json:"frw"`
	IAS     int `json:"ias"`
	FBR     int `json:"fbr"`
	MF      int `json:"mf"`
	GF      int `json:"gf"`

	Experience int `json:"experience"`
	NextExp    int `json:"nextExp"`
	LastExp    int `json:"lastExp"`

	StatPoints  int `json:"statPoints"`
	SkillPoints int `json:"skillPoints"`
	CastFrames  int `json:"castFrames"`

	AllSkills int  `json:"allSkills"`
	HasDebuff bool `json:"hasDebuff"`
}

type positionInfo struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type skillInfo struct {
	Name    string `json:"name"`
	ID      int    `json:"id"`
	Level   int    `json:"level"`
	Charges int    `json:"charges,omitempty"`
	Left    bool   `json:"left"`
	Right   bool   `json:"right"`
}

type corpseInfo struct {
	Found    bool         `json:"found"`
	Position positionInfo `json:"position"`
}

type entranceInfo struct {
	Name       string       `json:"name"`
	Position   positionInfo `json:"position"`
	Selectable bool         `json:"selectable"`
}

type adjLevelInfo struct {
	Area       string       `json:"area"`
	Position   positionInfo `json:"position"`
	IsEntrance bool         `json:"isEntrance"`
}

type roomInfo struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"w"`
	Height int `json:"h"`
}

type terrorZoneInfo struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type rosterInfo struct {
	Name     string       `json:"name"`
	Area     string       `json:"area"`
	Position positionInfo `json:"position"`
}

type hoverInfo struct {
	IsHovered bool   `json:"isHovered"`
	UnitID    int    `json:"unitId"`
	UnitType  string `json:"unitType"`
}

type goldInfo struct {
	Inventory int `json:"inventory"`
	Stash     int `json:"stash"`
	Total     int `json:"total"`
	Max       int `json:"max"`
}

// collisionGridInfo holds RLE-encoded collision data for the current area.
// Data is a run-length encoded byte array: each pair [walkable_count, non_walkable_count]
// is packed row by row. The frontend decodes this to render the map background.
type collisionGridInfo struct {
	OffsetX int   `json:"ox"`
	OffsetY int   `json:"oy"`
	Width   int   `json:"w"`
	Height  int   `json:"h"`
	Data    []int `json:"data"` // RLE: alternating runs of walkable/non-walkable per row
	AreaID  int   `json:"areaId"`
}

// ── map data cache ───────────────────────────────────────────────────────────

// mapLevel holds decoded collision data for one area from koolo-map.exe output.
type mapLevel struct {
	ID     int
	Name   string
	OX, OY int
	W, H   int
	Grid   [][]bool // [y][x] true = walkable
}

var (
	mapCacheMu    sync.RWMutex
	mapCacheSeed  uint
	mapCacheData  map[area.ID]*mapLevel
	d2lodPath     string
	difficultyNum string
)

// ── quest definitions ────────────────────────────────────────────────────────

type questDef struct {
	act  string
	name string
	id   quest.Quest
}

var questDefs = []questDef{
	{"I", "Den of Evil", quest.Act1DenOfEvil},
	{"I", "Sisters' Burial Grounds", quest.Act1SistersBurialGrounds},
	{"I", "Tools of the Trade", quest.Act1ToolsOfTheTrade},
	{"I", "Search for Cain", quest.Act1TheSearchForCain},
	{"I", "Forgotten Tower", quest.Act1TheForgottenTower},
	{"I", "Sisters to the Slaughter", quest.Act1SistersToTheSlaughter},
	{"II", "Radament's Lair", quest.Act2RadamentsLair},
	{"II", "Horadric Staff", quest.Act2TheHoradricStaff},
	{"II", "Tainted Sun", quest.Act2TaintedSun},
	{"II", "Arcane Sanctuary", quest.Act2ArcaneSanctuary},
	{"II", "The Summoner", quest.Act2TheSummoner},
	{"II", "Seven Tombs", quest.Act2TheSevenTombs},
	{"III", "Lam Esen's Tome", quest.Act3LamEsensTome},
	{"III", "Khalim's Will", quest.Act3KhalimsWill},
	{"III", "Blade of the Old Religion", quest.Act3BladeOfTheOldReligion},
	{"III", "Golden Bird", quest.Act3TheGoldenBird},
	{"III", "Blackened Temple", quest.Act3TheBlackenedTemple},
	{"III", "The Guardian", quest.Act3TheGuardian},
	{"IV", "Fallen Angel", quest.Act4TheFallenAngel},
	{"IV", "Hell's Forge", quest.Act4HellForge},
	{"IV", "Terror's End", quest.Act4TerrorsEnd},
	{"V", "Siege on Harrogath", quest.Act5SiegeOnHarrogath},
	{"V", "Rescue on Mt. Arreat", quest.Act5RescueOnMountArreat},
	{"V", "Prison of Ice", quest.Act5PrisonOfIce},
	{"V", "Betrayal of Harrogath", quest.Act5BetrayalOfHarrogath},
	{"V", "Rite of Passage", quest.Act5RiteOfPassage},
	{"V", "Eve of Destruction", quest.Act5EveOfDestruction},
}

// ── helpers ──────────────────────────────────────────────────────────────────

func npcDisplayName(id npc.ID) string {
	if flags, ok := npc.MonStatsFlagsByID[id]; ok && flags.Name != "" {
		return flags.Name
	}
	return fmt.Sprintf("NPC#%d", id)
}

func entranceDisplayName(n entrance.Name) string {
	if d, ok := entrance.Desc[int(n)]; ok && d.Name != "" {
		return d.Name
	}
	return fmt.Sprintf("Entrance#%d", n)
}

func areaDisplayName(id area.ID) string {
	if a, ok := area.Areas[id]; ok && a.Name != "" {
		return a.Name
	}
	return fmt.Sprintf("Area#%d", id)
}

func skillDisplayName(id skill.ID) string {
	if s, ok := skill.Skills[id]; ok && s.Name != "" {
		return s.Name
	}
	return fmt.Sprintf("Skill#%d", id)
}

func unitTypeName(ut int) string {
	switch ut {
	case 0:
		return "Player"
	case 1:
		return "Monster"
	case 2:
		return "Object"
	case 3:
		return "Missile"
	case 4:
		return "Item"
	case 5:
		return "Tile"
	default:
		return fmt.Sprintf("Type#%d", ut)
	}
}

// stateNames maps commonly encountered state IDs to readable names.
var stateNames = map[uint]string{
	0: "None", 1: "Freeze", 2: "Poison", 3: "ResistFire", 4: "ResistCold",
	5: "ResistLightning", 6: "ResistMagic", 8: "ResistAll", 9: "AmplifyDamage",
	10: "FrozenArmor", 11: "Cold", 14: "BoneArmor", 15: "Concentrate",
	16: "Enchant", 17: "InnerSight", 19: "Weaken", 20: "ChillingArmor",
	21: "Stunned", 24: "Slowed", 26: "Shout", 28: "Conviction", 29: "Convicted",
	30: "EnergyShield", 32: "BattleOrders", 33: "Might", 35: "HolyFire",
	36: "Thorns", 37: "Defiance", 38: "Thunderstorm", 40: "BlessedAim",
	42: "Concentration", 45: "Cleansing", 46: "HolyShock", 47: "Sanctuary",
	48: "Meditation", 49: "Fanaticism", 50: "Redemption", 51: "BattleCommand",
	55: "IronMaiden", 58: "LifeTap", 60: "Decrepify", 61: "LowerResist",
	62: "OpenWounds", 70: "Warmth", 80: "IncreasedStamina", 82: "IncreasedSpeed",
	101: "Fade", 102: "BurstOfSpeed",
}

func stateName(s uint) string {
	if n, ok := stateNames[s]; ok {
		return n
	}
	return fmt.Sprintf("State#%d", s)
}

func findStat(pu interface {
	FindStat(stat.ID, int) (stat.Data, bool)
}, id stat.ID) int {
	s, _ := pu.FindStat(id, 0)
	return s.Value
}

func clampPercent(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// ── map seed & collision grid ────────────────────────────────────────────────

// getMapSeed reads the map seed from the D2R process memory using the same
// pointer chain as internal/game/memory_reader.go:getMapSeed.
func getMapSeed(proc *memory.Process, playerUnitAddr uintptr) (uint, error) {
	actPtr := uintptr(proc.ReadUInt(playerUnitAddr+0x20, memory.Uint64))
	actMiscPtr := uintptr(proc.ReadUInt(actPtr+0x70, memory.Uint64))

	dwInitSeedHash1 := proc.ReadUInt(actMiscPtr+0x840, memory.Uint32)
	dwEndSeedHash1 := proc.ReadUInt(actMiscPtr+0x860, memory.Uint32)

	seed, found := utils.GetMapSeed(dwInitSeedHash1, dwEndSeedHash1)
	if !found {
		return 0, fmt.Errorf("could not calculate map seed")
	}
	return seed, nil
}

// serverLevel mirrors map_client.serverLevel for JSON parsing of koolo-map.exe output.
type serverLevel struct {
	Type   string `json:"type"`
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Offset struct {
		X int `json:"x"`
		Y int `json:"y"`
	} `json:"offset"`
	Size struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"size"`
	Map [][]int `json:"map"`
}

// decodeCollisionGrid decodes the RLE map data from koolo-map.exe into a
// boolean grid where true = walkable. Logic matches map_client.CollisionGrid().
func decodeCollisionGrid(lvl serverLevel) [][]bool {
	cg := make([][]bool, lvl.Size.Height)
	for y := 0; y < lvl.Size.Height; y++ {
		row := make([]bool, lvl.Size.Width)
		if y < len(lvl.Map) {
			mapRow := lvl.Map[y]
			isWalkable := false
			xPos := 0
			for k, xs := range mapRow {
				if k != 0 {
					for xOff := 0; xOff < xs && xPos+xOff < lvl.Size.Width; xOff++ {
						row[xPos+xOff] = isWalkable
					}
				}
				isWalkable = !isWalkable
				xPos += xs
			}
			for xPos < len(row) {
				row[xPos] = isWalkable
				xPos++
			}
		}
		cg[y] = row
	}
	return cg
}

// fetchMapData runs koolo-map.exe and caches collision grids keyed by area ID.
// It only re-fetches when the seed changes.
func fetchMapData(seed uint) {
	mapCacheMu.Lock()
	defer mapCacheMu.Unlock()

	if mapCacheSeed == seed && mapCacheData != nil {
		return
	}

	if d2lodPath == "" {
		log.Println("map: D2 LoD path not configured (-d2lod flag), skipping map data")
		return
	}

	cmd := exec.Command("./tools/koolo-map.exe", d2lodPath, "-s", strconv.FormatUint(uint64(seed), 10), "-d", difficultyNum)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdout, err := cmd.Output()
	if err != nil {
		log.Printf("map: koolo-map.exe error: %v", err)
		return
	}

	levels := make(map[area.ID]*mapLevel)
	for _, line := range strings.Split(string(stdout), "\r\n") {
		var lvl serverLevel
		if err := json.Unmarshal([]byte(line), &lvl); err != nil {
			continue
		}
		if lvl.Type == "" || len(lvl.Map) == 0 {
			continue
		}
		levels[area.ID(lvl.ID)] = &mapLevel{
			ID:   lvl.ID,
			Name: lvl.Name,
			OX:   lvl.Offset.X,
			OY:   lvl.Offset.Y,
			W:    lvl.Size.Width,
			H:    lvl.Size.Height,
			Grid: decodeCollisionGrid(lvl),
		}
	}

	mapCacheSeed = seed
	mapCacheData = levels
	log.Printf("map: loaded %d areas for seed %d", len(levels), seed)
}

// rleEncodeGrid produces a compact RLE representation of a walkable grid.
// Format: flat array of run lengths, starting with a walkable run (may be 0).
// Alternates walkable / non-walkable run lengths, row by row concatenated.
func rleEncodeGrid(grid [][]bool, w, h int) []int {
	var rle []int
	for y := 0; y < h; y++ {
		if y >= len(grid) {
			// Missing row: entire row non-walkable (0 walkable, w non-walkable)
			rle = append(rle, 0, w)
			continue
		}
		row := grid[y]
		x := 0
		for x < w {
			// Count walkable run
			wRun := 0
			for x+wRun < w && x+wRun < len(row) && row[x+wRun] {
				wRun++
			}
			// Count non-walkable run
			nRun := 0
			for x+wRun+nRun < w && (x+wRun+nRun >= len(row) || !row[x+wRun+nRun]) {
				nRun++
			}
			rle = append(rle, wRun, nRun)
			x += wRun + nRun
		}
	}
	return rle
}

// getCollisionGrid returns the collision grid info for the given area, or nil.
func getCollisionGrid(areaID area.ID) *collisionGridInfo {
	mapCacheMu.RLock()
	defer mapCacheMu.RUnlock()

	lvl, ok := mapCacheData[areaID]
	if !ok || lvl == nil {
		return nil
	}

	return &collisionGridInfo{
		OffsetX: lvl.OX,
		OffsetY: lvl.OY,
		Width:   lvl.W,
		Height:  lvl.H,
		Data:    rleEncodeGrid(lvl.Grid, lvl.W, lvl.H),
		AreaID:  lvl.ID,
	}
}

// ── data collection ──────────────────────────────────────────────────────────

var grMu sync.Mutex

func collectData(gr *memory.GameReader) (resp apiResponse) {
	grMu.Lock()
	defer grMu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			resp = apiResponse{OK: false, Error: fmt.Sprintf("%v", r)}
		}
	}()

	d := gr.GetData()
	resp.OK = true
	resp.Timestamp = time.Now().Format("15:04:05")

	pu := d.PlayerUnit
	lvl := findStat(pu, stat.Level)

	resp.Player = playerInfo{
		Name:  pu.Name,
		Class: fmt.Sprintf("%v", pu.Class),
		Level: lvl,
		Area:  areaDisplayName(pu.Area),
		HP:    clampPercent(pu.HPPercent()),
		MP:    clampPercent(pu.MPPercent()),
		Mode:  fmt.Sprintf("%v", pu.Mode),
		Dead:  pu.IsDead(),
	}

	resp.Game = gameInfo{
		Name:    d.Game.LastGameName,
		FPS:     d.Game.FPS,
		Ping:    d.Game.Ping,
		HasMerc: d.HasMerc,
		InGame:  d.IsIngame,
		Legacy:  d.LegacyGraphics,
	}

	// ── Player stats (technical) ──
	resp.PlayerStats = playerStatsInfo{
		Strength:  findStat(pu, stat.Strength),
		Dexterity: findStat(pu, stat.Dexterity),
		Vitality:  findStat(pu, stat.Vitality),
		Energy:    findStat(pu, stat.Energy),

		Life:    findStat(pu, stat.Life),
		MaxLife: findStat(pu, stat.MaxLife),
		Mana:    findStat(pu, stat.Mana),
		MaxMana: findStat(pu, stat.MaxMana),
		Stamina: findStat(pu, stat.Stamina),
		MaxStam: findStat(pu, stat.MaxStamina),

		FireRes:      findStat(pu, stat.FireResist),
		MaxFireRes:   findStat(pu, stat.MaxFireResist),
		ColdRes:      findStat(pu, stat.ColdResist),
		MaxColdRes:   findStat(pu, stat.MaxColdResist),
		LightRes:     findStat(pu, stat.LightningResist),
		MaxLightRes:  findStat(pu, stat.MaxLightningResist),
		PoisonRes:    findStat(pu, stat.PoisonResist),
		MaxPoisonRes: findStat(pu, stat.MaxPoisonResist),
		MagicRes:     findStat(pu, stat.MagicResist),

		Defense: findStat(pu, stat.Defense),
		FCR:     findStat(pu, stat.FasterCastRate),
		FHR:     findStat(pu, stat.FasterHitRecovery),
		FRW:     findStat(pu, stat.FasterRunWalk),
		IAS:     findStat(pu, stat.IncreasedAttackSpeed),
		FBR:     findStat(pu, stat.FasterBlockRate),
		MF:      findStat(pu, stat.MagicFind),
		GF:      findStat(pu, stat.GoldFind),

		Experience: findStat(pu, stat.Experience),
		NextExp:    findStat(pu, stat.NextExp),
		LastExp:    findStat(pu, stat.LastExp),

		StatPoints:  findStat(pu, stat.StatPoints),
		SkillPoints: findStat(pu, stat.SkillPoints),
		CastFrames:  pu.CastingFrames(),

		AllSkills: findStat(pu, stat.AllSkills),
		HasDebuff: pu.HasDebuff(),
	}

	// ── Position ──
	resp.PlayerPos = positionInfo{X: pu.Position.X, Y: pu.Position.Y}
	resp.AreaOrigin = positionInfo{X: d.AreaOrigin.X, Y: d.AreaOrigin.Y}

	// ── Active states ──
	for _, s := range pu.States {
		resp.States = append(resp.States, stateName(uint(s)))
	}

	// ── Skills ──
	type skillEntry struct {
		id   skill.ID
		info skillInfo
	}
	var skillEntries []skillEntry
	for sid, pts := range pu.Skills {
		if pts.Level == 0 && pts.Charges == 0 {
			continue
		}
		se := skillEntry{
			id: sid,
			info: skillInfo{
				Name:    skillDisplayName(sid),
				ID:      int(sid),
				Level:   int(pts.Level),
				Charges: int(pts.Charges),
				Left:    sid == pu.LeftSkill,
				Right:   sid == pu.RightSkill,
			},
		}
		skillEntries = append(skillEntries, se)
	}
	sort.Slice(skillEntries, func(i, j int) bool {
		return skillEntries[i].id < skillEntries[j].id
	})
	for _, se := range skillEntries {
		resp.Skills = append(resp.Skills, se.info)
	}

	// ── Corpse ──
	resp.Corpse = corpseInfo{
		Found:    d.Corpse.Found,
		Position: positionInfo{X: d.Corpse.Position.X, Y: d.Corpse.Position.Y},
	}

	// ── Quests ──
	for _, qd := range questDefs {
		s := d.Quests[qd.id]
		status := "none"
		if s.Completed() {
			status = "done"
		} else if !s.NotStarted() {
			status = "wip"
		}
		resp.Quests = append(resp.Quests, questInfo{
			Act:    qd.act,
			Name:   qd.name,
			Status: status,
			Raw:    uint16(s),
		})
	}

	// ── Monsters ──
	for _, m := range d.Monsters {
		maxHP := m.Stats[stat.MaxLife]
		curHP := m.Stats[stat.Life]
		pct := 0
		if maxHP > 0 {
			pct = curHP * 100 / maxHP
		}
		resp.Monsters = append(resp.Monsters, monsterInfo{
			Name:  npcDisplayName(m.Name),
			Type:  string(m.Type),
			X:     m.Position.X,
			Y:     m.Position.Y,
			HPPct: pct,
		})
	}

	// ── Corpses (dead monsters) ──
	for _, m := range d.Corpses {
		resp.Corpses = append(resp.Corpses, monsterInfo{
			Name:  npcDisplayName(m.Name),
			Type:  string(m.Type),
			X:     m.Position.X,
			Y:     m.Position.Y,
			HPPct: 0,
		})
	}

	// ── Items ──
	for _, it := range d.Inventory.AllItems {
		ii := itemInfo{
			Name:       string(it.Name),
			Quality:    it.Quality.ToString(),
			Location:   fmt.Sprintf("%v", it.Location.LocationType),
			Ethereal:   it.Ethereal,
			Identified: it.Identified,
			Sockets:    len(it.Sockets),
			LevelReq:   it.LevelReq,
		}
		if it.IsRuneword {
			ii.Runeword = string(it.RunewordName)
		}
		resp.Items = append(resp.Items, ii)
	}

	// ── Objects ──
	for _, o := range d.Objects {
		resp.Objects = append(resp.Objects, objectInfo{
			Name:       fmt.Sprintf("%v", o.Name),
			X:          o.Position.X,
			Y:          o.Position.Y,
			Selectable: o.Selectable,
		})
	}

	// ── Entrances ──
	for _, e := range d.Entrances {
		resp.Entrances = append(resp.Entrances, entranceInfo{
			Name:       entranceDisplayName(e.Name),
			Position:   positionInfo{X: e.Position.X, Y: e.Position.Y},
			Selectable: e.Selectable,
		})
	}

	// ── Adjacent levels ──
	for _, lvl := range d.AdjacentLevels {
		resp.AdjLevels = append(resp.AdjLevels, adjLevelInfo{
			Area:       areaDisplayName(lvl.Area),
			Position:   positionInfo{X: lvl.Position.X, Y: lvl.Position.Y},
			IsEntrance: lvl.IsEntrance,
		})
	}

	// ── Rooms ──
	for _, r := range d.Rooms {
		resp.Rooms = append(resp.Rooms, roomInfo{
			X:      r.Position.X,
			Y:      r.Position.Y,
			Width:  r.Width,
			Height: r.Height,
		})
	}

	// ── Terror Zones ──
	for _, tz := range d.TerrorZones {
		resp.TerrorZones = append(resp.TerrorZones, terrorZoneInfo{
			ID:   int(tz),
			Name: areaDisplayName(tz),
		})
	}

	// ── Roster ──
	for _, rm := range d.Roster {
		resp.Roster = append(resp.Roster, rosterInfo{
			Name:     rm.Name,
			Area:     areaDisplayName(rm.Area),
			Position: positionInfo{X: rm.Position.X, Y: rm.Position.Y},
		})
	}

	// ── Hover ──
	resp.Hover = hoverInfo{
		IsHovered: d.HoverData.IsHovered,
		UnitID:    int(d.HoverData.UnitID),
		UnitType:  unitTypeName(d.HoverData.UnitType),
	}

	// ── Gold ──
	invGold := findStat(pu, stat.Gold)
	stashGold := findStat(pu, stat.StashGold)
	resp.Gold = goldInfo{
		Inventory: invGold,
		Stash:     stashGold,
		Total:     pu.TotalPlayerGold(),
		Max:       pu.MaxGold(),
	}

	// ── Weapon slot & merc ──
	resp.WeaponSlot = d.ActiveWeaponSlot
	resp.MercHP = clampPercent(d.MercHPPercent())

	// ── Menus ──
	om := d.OpenMenus
	resp.Menus = menuInfo{
		Inventory:     om.Inventory,
		Stash:         om.Stash,
		SkillTree:     om.SkillTree,
		NPCShop:       om.NPCShop,
		Waypoint:      om.Waypoint,
		QuitMenu:      om.QuitMenu,
		MapShown:      om.MapShown,
		Character:     om.Character,
		Cube:          om.Cube,
		MercInventory: om.MercInventory,
		QuestLog:      om.QuestLog,
		ChatOpen:      om.ChatOpen,
		LoadingScreen: om.LoadingScreen,
		Cinematic:     om.Cinematic,
	}

	// ── Collision grid (map background) ──
	if d2lodPath != "" && pu.Address != 0 {
		seed, err := getMapSeed(gr.Process, pu.Address)
		if err == nil && seed != 0 {
			// Fetch map data in background if seed changed (blocks only first time)
			go fetchMapData(seed)
			resp.CollisionGrid = getCollisionGrid(pu.Area)
		}
	}

	return resp
}

// ── main ─────────────────────────────────────────────────────────────────────

func main() {
	port := flag.Int("port", 0, "HTTP port (0 = auto-assign)")
	noBrowser := flag.Bool("no-browser", false, "skip auto-open")
	d2lod := flag.String("d2lod", "", "path to D2 LoD 1.13c installation (enables map backgrounds)")
	diff := flag.String("difficulty", "0", "game difficulty: 0=Normal, 1=Nightmare, 2=Hell")
	flag.Parse()

	d2lodPath = strings.TrimSpace(*d2lod)
	if d2lodPath != "" {
		d2lodPath = strings.ReplaceAll(strings.ToLower(d2lodPath), "game.exe", "")
		log.Printf("map: D2 LoD path: %s, difficulty: %s", d2lodPath, *diff)
	}
	difficultyNum = *diff

	proc, err := memory.NewProcess()
	if err != nil {
		fmt.Fprintf(os.Stderr, "attach: %v\n", err)
		os.Exit(1)
	}
	defer proc.Close()

	gr := memory.NewGameReader(proc)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
		w.Write(dashboardHTML)
	})
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(collectData(gr))
	})

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		os.Exit(1)
	}

	url := "http://" + ln.Addr().String()
	fmt.Println("D2R Dashboard:", url)

	if !*noBrowser {
		_ = exec.Command("cmd", "/c", "start", url).Start()
	}

	fmt.Println("Ctrl+C to stop")
	_ = http.Serve(ln, mux)
}
