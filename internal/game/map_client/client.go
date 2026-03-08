package map_client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/difficulty"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/object"
	"github.com/hectorgimenez/koolo/internal/config"
)

func GetMapData(seed string, difficulty difficulty.Difficulty) (MapData, error) {
	mapExe := "./tools/koolo-map.exe"
	d2path := config.Koolo.D2LoDPath
	diffNum := getDifficultyAsNum(difficulty)

	// Pre-flight: verify koolo-map.exe exists
	if _, err := os.Stat(mapExe); err != nil {
		return nil, fmt.Errorf("koolo-map.exe not found at %q: %w", mapExe, err)
	}

	// Pre-flight: verify D2LoDPath contains expected files
	if d2path == "" {
		return nil, fmt.Errorf("D2LoDPath is empty, please configure it in koolo.yaml")
	}
	if _, err := os.Stat(filepath.Join(d2path, "d2data.mpq")); err != nil {
		return nil, fmt.Errorf("D2LoDPath %q does not contain d2data.mpq (required D2 LoD 1.13c file): %w", d2path, err)
	}

	cmd := exec.Command(mapExe, d2path, "-s", seed, "-d", diffNum)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdout, err := cmd.Output()
	if err != nil {
		errMsg := fmt.Sprintf("koolo-map.exe failed (D2LoDPath=%q, seed=%s, difficulty=%s)",
			d2path, seed, diffNum)
		if stderr.Len() > 0 {
			errMsg += fmt.Sprintf(", stderr: %s", strings.TrimSpace(stderr.String()))
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			errMsg += fmt.Sprintf(", exit code: %d", exitErr.ExitCode())
		}
		return nil, fmt.Errorf("%s: %w", errMsg, err)
	}

	stdoutLines := strings.Split(string(stdout), "\r\n")

	lvls := make([]serverLevel, 0)
	for _, line := range stdoutLines {
		var lvl serverLevel
		err = json.Unmarshal([]byte(line), &lvl)
		// Discard empty lines or lines that don't contain level information
		if err == nil && lvl.Type != "" && len(lvl.Map) > 0 {
			lvls = append(lvls, lvl)
		}
	}

	return lvls, nil
}

func getDifficultyAsNum(df difficulty.Difficulty) string {
	switch df {
	case difficulty.Normal:
		return "0"
	case difficulty.Nightmare:
		return "1"
	case difficulty.Hell:
		return "2"
	}

	return "0"
}

type MapData []serverLevel

func (lvl serverLevel) CollisionGrid() [][]bool {
	var cg [][]bool

	for y := 0; y < lvl.Size.Height; y++ {
		var row []bool
		for x := 0; x < lvl.Size.Width; x++ {
			row = append(row, false)
		}

		// Documentation about how this works: https://github.com/blacha/diablo2/tree/master/packages/map
		if len(lvl.Map) > y {
			mapRow := lvl.Map[y]
			isWalkable := false
			xPos := 0
			for k, xs := range mapRow {
				if k != 0 {
					for xOffset := 0; xOffset < xs; xOffset++ {
						row[xPos+xOffset] = isWalkable
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

		cg = append(cg, row)
	}

	return cg
}

func (lvl serverLevel) NPCsExitsAndObjects() (data.NPCs, []data.Level, []data.Object, []data.Room) {
	var npcs []data.NPC
	var exits []data.Level
	var objects []data.Object
	var rooms []data.Room

	for _, r := range lvl.Rooms {
		rooms = append(rooms, data.Room{
			Position: data.Position{X: r.X,
				Y: r.Y,
			},
			Width:  r.Width,
			Height: r.Height,
		})
	}

	for _, obj := range lvl.Objects {
		switch obj.Type {
		case "npc":
			n := data.NPC{
				ID:   npc.ID(obj.ID),
				Name: obj.Name,
				Positions: []data.Position{{
					X: obj.X + lvl.Offset.X,
					Y: obj.Y + lvl.Offset.Y,
				}},
			}
			npcs = append(npcs, n)
		case "exit":
			exit := data.Level{
				Area: area.ID(obj.ID),
				Position: data.Position{
					X: obj.X + lvl.Offset.X,
					Y: obj.Y + lvl.Offset.Y,
				},
				IsEntrance: true,
			}
			exits = append(exits, exit)
		case "object":
			o := data.Object{
				Name: object.Name(obj.ID),
				Position: data.Position{
					X: obj.X + lvl.Offset.X,
					Y: obj.Y + lvl.Offset.Y,
				},
			}
			objects = append(objects, o)
		}
	}

	for _, obj := range lvl.Objects {
		switch obj.Type {
		case "exit_area":
			found := false
			for _, exit := range exits {
				if exit.Area == area.ID(obj.ID) {
					exit.IsEntrance = false
					found = true
					break
				}
			}

			if !found {
				lvl := data.Level{
					Area: area.ID(obj.ID),
					Position: data.Position{
						X: obj.X + lvl.Offset.X,
						Y: obj.Y + lvl.Offset.Y,
					},
					IsEntrance: false,
				}
				exits = append(exits, lvl)
			}
		}

	}

	return npcs, exits, objects, rooms
}
