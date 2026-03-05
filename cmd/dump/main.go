//go:build windows

package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/quest"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/d2go/pkg/memory"
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
}

type playerInfo struct {
	Name  string `json:"name"`
	Class string `json:"class"`
	Level int    `json:"level"`
	Area  string `json:"area"`
	HP    int    `json:"hp"`
	MP    int    `json:"mp"`
	Mode  string `json:"mode"`
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
	Name     string `json:"name"`
	Quality  string `json:"quality"`
	Location string `json:"location"`
}

type objectInfo struct {
	Name       string `json:"name"`
	X          int    `json:"x"`
	Y          int    `json:"y"`
	Selectable bool   `json:"selectable"`
}

type menuInfo struct {
	Inventory bool `json:"inventory"`
	Stash     bool `json:"stash"`
	SkillTree bool `json:"skillTree"`
	NPCShop   bool `json:"npcShop"`
	Waypoint  bool `json:"waypoint"`
	QuitMenu  bool `json:"quitMenu"`
	MapShown  bool `json:"mapShown"`
}

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

	lvl, _ := d.PlayerUnit.FindStat(stat.Level, 0)
	resp.Player = playerInfo{
		Name:  d.PlayerUnit.Name,
		Class: fmt.Sprintf("%v", d.PlayerUnit.Class),
		Level: lvl.Value,
		Area:  fmt.Sprintf("%v", d.PlayerUnit.Area),
		HP:    d.PlayerUnit.HPPercent(),
		MP:    d.PlayerUnit.MPPercent(),
		Mode:  fmt.Sprintf("%v", d.PlayerUnit.Mode),
	}

	resp.Game = gameInfo{
		Name:    d.Game.LastGameName,
		FPS:     d.Game.FPS,
		Ping:    d.Game.Ping,
		HasMerc: d.HasMerc,
		InGame:  d.IsIngame,
		Legacy:  d.LegacyGraphics,
	}

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

	for _, it := range d.Inventory.AllItems {
		resp.Items = append(resp.Items, itemInfo{
			Name:     string(it.Name),
			Quality:  it.Quality.ToString(),
			Location: fmt.Sprintf("%v", it.Location.LocationType),
		})
	}

	for _, o := range d.Objects {
		resp.Objects = append(resp.Objects, objectInfo{
			Name:       fmt.Sprintf("%v", o.Name),
			X:          o.Position.X,
			Y:          o.Position.Y,
			Selectable: o.Selectable,
		})
	}

	om := d.OpenMenus
	resp.Menus = menuInfo{
		Inventory: om.Inventory,
		Stash:     om.Stash,
		SkillTree: om.SkillTree,
		NPCShop:   om.NPCShop,
		Waypoint:  om.Waypoint,
		QuitMenu:  om.QuitMenu,
		MapShown:  om.MapShown,
	}

	return resp
}

// ── main ─────────────────────────────────────────────────────────────────────

func main() {
	port := flag.Int("port", 0, "HTTP port (0 = auto-assign)")
	noBrowser := flag.Bool("no-browser", false, "skip auto-open")
	flag.Parse()

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
