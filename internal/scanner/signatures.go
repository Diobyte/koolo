//go:build windows

package scanner

// D2RSignatures returns the full catalog of D2R pattern signatures.
// Sources:
//   - nyx-d2r offsets.h (ejt1/nyx-d2r, MIT license)
//   - upstream d2go offset.go (hectorgimenez/d2go)
//   - Diobyte/d2go offset names
//
// Pattern format: space-separated hex, "?" wildcard, "^" offset marker.
func D2RSignatures() []SignatureDef {
	return []SignatureDef{
		// ── d2go core offsets (from upstream hectorgimenez/d2go) ─────────────

		{
			Name:    "GameData",
			Pattern: "44 88 25 ^ ? ? ? 66 44 89 25",
			Type:    Relative32Add,
		},
		{
			Name:    "UnitTable",
			Pattern: "48 03 C7 49 8B 8C C6 ^ ? ? ? ?",
			Type:    Absolute,
		},
		{
			Name:    "UI",
			Pattern: "40 84 ED 0F 94 05 ^ ? ? ? ?",
			Type:    Relative32Add,
		},
		{
			Name:    "Hover",
			Pattern: "C6 84 C2 ^ ? ? ? ? 00 48 8B 74",
			Type:    Absolute,
		},
		{
			Name:    "Expansion",
			Pattern: "48 8B 05 ^ ? ? ? 48 8B D9 F3 0F 10 50",
			Type:    Relative32Add,
		},
		{
			Name:    "Roster",
			Pattern: "02 45 33 D2 4D 8B ^ ? ? ? ?",
			Type:    Relative32Add,
		},
		{
			Name:    "PanelManagerContainer",
			Pattern: "48 89 05 ^ ? ? ? 48 85 DB 74 1E",
			Type:    Relative32Add,
		},
		{
			Name:    "WidgetStates",
			Pattern: "48 8B 0D ^ ? ? ? 4C 8D 44 24 ? 48 03 C2",
			Type:    Relative32Add,
		},
		{
			Name:    "Waypoints",
			Pattern: "48 89 05 ^ ? ? ? 0F 11 00",
			Type:    Relative32Add,
		},
		{
			Name:    "FPS",
			Pattern: "8B 1D ^ ? ? ? 48 8D 05 ? ? ? ? 48 8D 4C 24 40",
			Type:    Relative32Add,
		},
		{
			Name:    "KeyBindings",
			Pattern: "48 8D 05 ^ AF EE",
			Type:    Relative32Add,
		},
		{
			Name:    "KeyBindingsSkills",
			Pattern: "0F 10 04 24 48 6B C8 1C 48 8D 05 ^ ? ? ? ?",
			Type:    Relative32Add,
		},
		{
			Name:    "TerrorZones",
			Pattern: "48 89 05 ^ ? ? ? 48 8D 05 ? ? ? ? 48 89 05 ? ? ? ? 48 8D 05 ? ? ? ? 48 89 15 ? ? ? ? 48 89 15",
			Type:    Relative32Add,
		},
		{
			Name:    "Quests",
			Pattern: "42 C6 84 28 ^ ? ? ? ? 00 49 FF C5 49 83 FD 29",
			Type:    Absolute,
		},
		{
			Name:    "Ping",
			Pattern: "48 8B 0D ^ ? ? ? 49 2B C7",
			Type:    Relative32Add,
		},
		{
			Name:    "LegacyGraphics",
			Pattern: "80 3D ^ ? ? ? ? 48 8D 54 24 30",
			Type:    Relative32Add,
		},

		// ── Diobyte-specific offsets (not in upstream hectorgimenez/d2go) ───
		// QuestInfo — pointer to quest data structure
		// Pattern derived from: the code that reads quest flags via QuestInfo ptr
		{
			Name:    "QuestInfo",
			Pattern: "48 8B 05 ^ ? ? ? 48 8B 08 48 85 C9 0F 84",
			Type:    Relative32Add,
		},
		// CharData — character data array header (flags, hardcore, expansion, etc.)
		{
			Name:    "CharData",
			Pattern: "48 8D 0D ^ ? ? ? E8 ? ? ? ? 48 8B D8 48 85 C0 0F 84 ? ? ? ? 80 78",
			Type:    Relative32Add,
		},
		// SelectedCharName — string pointer for the currently selected character name
		{
			Name:    "SelectedCharName",
			Pattern: "48 8D 05 ^ ? ? ? 33 D2 4C 8D 0D",
			Type:    Relative32Add,
		},
		// LastGameName — static buffer holding the last game name string
		{
			Name:    "LastGameName",
			Pattern: "48 8D 0D ^ ? ? ? E8 ? ? ? ? 48 8D 15 ? ? ? ? 48 8D 4C 24 ? E8 ? ? ? ? 85 C0 0F 85",
			Type:    Relative32Add,
		},
		// LastGamePassword — static buffer holding the last game password string
		// Located at a fixed delta from LastGameName (0x58 bytes apart)
		{
			Name:    "LastGamePassword",
			Pattern: "48 8D 15 ^ ? ? ? 48 8D 4C 24 ? E8 ? ? ? ? 85 C0 0F 85",
			Type:    Relative32Add,
		},

		// ── MapAssist-sourced patterns ──────────────────────────────────────
		// MenuData — UI menu state flags (in-game, loading, etc.)
		{
			Name:    "MenuData",
			Pattern: "48 89 45 B7 4C 8D 35 ^ ? ? ? ?",
			Type:    Relative32Add,
		},
		// InteractedNpc — last NPC the player interacted with
		{
			Name:    "InteractedNpc",
			Pattern: "43 01 84 31 ^ ? ? ? ?",
			Type:    Absolute,
		},
		// Pets — pet/summon unit list
		{
			Name:    "Pets",
			Pattern: "48 8B 05 ^ ? ? ? 48 89 41 30 89 59 08",
			Type:    Relative32Add,
		},

		// ── nyx-d2r advanced offsets ────────────────────────────────────────

		{
			Name:    "D2Allocator",
			Pattern: "48 8B 0D ^ ? ? ? 8B F8 48 85 C9",
			Type:    Relative32Add,
		},
		{
			Name:    "BcAllocator",
			Pattern: "E8 ^ ? ? ? 33 DB 48 89 05",
			Type:    Relative32Add,
		},
		{
			Name:    "kCheckData",
			Pattern: "48 8B 05 ^ ? ? ? 41 80 F0",
			Type:    Relative32Add,
		},

		// ── nyx-d2r maphack offsets ─────────────────────────────────────────

		{
			Name:    "DRLG_AllocLevel",
			Pattern: "E8 ^ ? ? ? 48 8B D8 83 3B",
			Type:    Relative32Add,
		},
		{
			Name:    "DRLG_InitLevel",
			Pattern: "E8 ^ ? ? ? 44 8B 8C 24 ? ? ? ? 41 83 F9",
			Type:    Relative32Add,
		},
		{
			Name:    "ROOMS_AddRoomData",
			Pattern: "E8 ^ ? ? ? 49 BB ? ? ? ? ? ? ? ? FF C6",
			Type:    Relative32Add,
		},
		{
			Name:    "GetLevelDef",
			Pattern: "E8 ^ ? ? ? 44 0F B6 90",
			Type:    Relative32Add,
		},
		{
			Name:    "AutomapLayerLink",
			Pattern: "48 8B 05 ^ ? ? ? 49 89 86",
			Type:    Relative32Add,
		},
		{
			Name:    "CurrentAutomapLayer",
			Pattern: "48 8B 05 ^ ? ? ? 8B 08",
			Type:    Relative32Add,
		},
		{
			Name:    "ClearLinkedList",
			Pattern: "E8 ^ ? ? ? 48 8D 3D ? ? ? ? 48 8D 2D",
			Type:    Relative32Add,
		},
		{
			Name:    "AUTOMAP_NewAutomapCell",
			Pattern: "E8 ^ ? ? ? 48 8B 75 ? 48 85 F6 0F 84 ? ? ? ? E8 ? ? ? ? 8D 57",
			Type:    Relative32Add,
		},
		{
			Name:    "AUTOMAP_AddAutomapCell",
			Pattern: "E8 ^ ? ? ? 4D 89 1F",
			Type:    Relative32Add,
		},

		// ── nyx-d2r widget offsets ──────────────────────────────────────────

		{
			Name:    "Widget_GetScaledPosition",
			Pattern: "E8 ^ ? ? ? 8B 10 8B 48",
			Type:    Relative32Add,
		},
		{
			Name:    "Widget_GetScaledSize",
			Pattern: "E8 ^ ? ? ? 41 3B F3",
			Type:    Relative32Add,
		},
		{
			Name:    "PanelManager_GetScreenSizeX",
			Pattern: "E8 ^ ? ? ? 0F 57 C0 0F 57 FF",
			Type:    Relative32Add,
		},
		{
			Name:    "PanelManager",
			Pattern: "0F 84 ? ? ? ? 48 8B 05 ^ ? ? ? 0F 57 C9",
			Type:    Relative32Add,
		},
		{
			Name:    "AutoMapPanel_GetMode",
			Pattern: "E8 ^ ? ? ? 83 F8 ? 75 ? 33 D2 48 8B CF",
			Type:    Relative32Add,
		},
		{
			Name:    "AutoMapPanel_CreateAutoMapData",
			Pattern: "4C 89 44 24 ? 53 55 56 57 41 54 41 56",
			Type:    Relative32Add,
		},
		{
			Name:    "AutoMapPanel_PrecisionToAutomap",
			Pattern: "48 89 5C 24 ? 55 56 57 48 8B EC 48 83 EC ? 49 8B D8",
			Type:    Relative32Add,
		},
		{
			Name:    "AutoMapPanel_spdwShift",
			Pattern: "8B 0D ^ ? ? ? 8B 35",
			Type:    Relative32Add,
		},

		// ── nyx-d2r data table offsets ──────────────────────────────────────

		{
			Name:    "sgptDataTbls",
			Pattern: "48 8D 15 ^ ? ? ? 49 8B 9E",
			Type:    Relative32Add,
		},
		{
			Name:    "DATATBLS_GetAutomapCellId",
			Pattern: "48 89 5C 24 ? 48 89 74 24 ? 57 48 83 EC ? 48 63 D9 45 8B D9",
			Type:    Relative32Add,
		},

		// ── nyx-d2r unit offsets ────────────────────────────────────────────

		{
			Name:    "PlayerUnitIndex",
			Pattern: "8B 0D ^ ? ? ? 48 8B 58 18",
			Type:    Relative32Add,
		},
		{
			Name:    "ClientSideUnitHashTable",
			Pattern: "48 63 C1 48 8D 0D ^ ? ? ? 48 C1 E0",
			Type:    Relative32Add,
		},
		{
			Name:    "GetClientSideUnitHashTableByType",
			Pattern: "E8 ^ ? ? ? 8B D5 41 B9",
			Type:    Relative32Add,
		},
		{
			Name:    "GetServerSideUnitHashTableByType",
			Pattern: "E8 ^ ? ? ? 45 8B C1 41 83 E0",
			Type:    Relative32Add,
		},
		{
			Name:    "EncTransformValue",
			Pattern: "E8 ^ ? ? ? 44 39 45",
			Type:    Relative32Add,
		},
		{
			Name:    "EncEncryptionKeys",
			Pattern: "48 8B 05 ^ ? ? ? 8B 80",
			Type:    Relative32Add,
		},
		{
			Name:    "PlayerIndexToIDEncryptedTable",
			Pattern: "48 8D 15 ^ ? ? ? 8B DF",
			Type:    Relative32Add,
		},
	}
}

// D2RCoreSignatures returns only the core d2go offsets needed for game reading.
func D2RCoreSignatures() []SignatureDef {
	return []SignatureDef{
		{Name: "GameData", Pattern: "44 88 25 ^ ? ? ? 66 44 89 25", Type: Relative32Add},
		{Name: "UnitTable", Pattern: "48 03 C7 49 8B 8C C6 ^ ? ? ? ?", Type: Absolute},
		{Name: "UI", Pattern: "40 84 ED 0F 94 05 ^ ? ? ? ?", Type: Relative32Add},
		{Name: "Hover", Pattern: "C6 84 C2 ^ ? ? ? ? 00 48 8B 74", Type: Absolute},
		{Name: "Expansion", Pattern: "48 8B 05 ^ ? ? ? 48 8B D9 F3 0F 10 50", Type: Relative32Add},
		{Name: "Roster", Pattern: "02 45 33 D2 4D 8B ^ ? ? ? ?", Type: Relative32Add},
		{Name: "PanelManagerContainer", Pattern: "48 89 05 ^ ? ? ? 48 85 DB 74 1E", Type: Relative32Add},
		{Name: "WidgetStates", Pattern: "48 8B 0D ^ ? ? ? 4C 8D 44 24 ? 48 03 C2", Type: Relative32Add},
		{Name: "Waypoints", Pattern: "48 89 05 ^ ? ? ? 0F 11 00", Type: Relative32Add},
		{Name: "FPS", Pattern: "8B 1D ^ ? ? ? 48 8D 05 ? ? ? ? 48 8D 4C 24 40", Type: Relative32Add},
		{Name: "KeyBindings", Pattern: "48 8D 05 ^ AF EE", Type: Relative32Add},
		{Name: "KeyBindingsSkills", Pattern: "0F 10 04 24 48 6B C8 1C 48 8D 05 ^ ? ? ? ?", Type: Relative32Add},
		{Name: "QuestInfo", Pattern: "48 8B 05 ^ ? ? ? 48 8B 08 48 85 C9 0F 84", Type: Relative32Add},
		{Name: "TerrorZones", Pattern: "48 89 05 ^ ? ? ? 48 8D 05 ? ? ? ? 48 89 05 ? ? ? ? 48 8D 05 ? ? ? ? 48 89 15 ? ? ? ? 48 89 15", Type: Relative32Add},
		{Name: "Quests", Pattern: "42 C6 84 28 ^ ? ? ? ? 00 49 FF C5 49 83 FD 29", Type: Absolute},
		{Name: "Ping", Pattern: "48 8B 0D ^ ? ? ? 49 2B C7", Type: Relative32Add},
		{Name: "LegacyGraphics", Pattern: "80 3D ^ ? ? ? ? 48 8D 54 24 30", Type: Relative32Add},
		{Name: "CharData", Pattern: "48 8D 0D ^ ? ? ? E8 ? ? ? ? 48 8B D8 48 85 C0 0F 84 ? ? ? ? 80 78", Type: Relative32Add},
		{Name: "SelectedCharName", Pattern: "48 8D 05 ^ ? ? ? 33 D2 4C 8D 0D", Type: Relative32Add},
		{Name: "LastGameName", Pattern: "48 8D 0D ^ ? ? ? E8 ? ? ? ? 48 8D 15 ? ? ? ? 48 8D 4C 24 ? E8 ? ? ? ? 85 C0 0F 85", Type: Relative32Add},
		{Name: "LastGamePassword", Pattern: "48 8D 15 ^ ? ? ? 48 8D 4C 24 ? E8 ? ? ? ? 85 C0 0F 85", Type: Relative32Add},
	}
}

// DiobyteFieldMap maps scanner signature names to Diobyte offset.go struct field names.
var DiobyteFieldMap = map[string]string{
	"GameData":              "GameData",
	"UnitTable":             "UnitTable",
	"UI":                    "UI",
	"Hover":                 "Hover",
	"Expansion":             "Expansion",
	"Roster":                "RosterOffset",
	"PanelManagerContainer": "PanelManagerContainerOffset",
	"WidgetStates":          "WidgetStatesOffset",
	"Waypoints":             "WaypointTableOffset",
	"FPS":                   "FPS",
	"KeyBindings":           "KeyBindingsOffset",
	"KeyBindingsSkills":     "KeyBindingsSkillsOffset",
	"QuestInfo":             "QuestInfo",
	"TerrorZones":           "TZ",
	"Quests":                "Quests",
	"Ping":                  "Ping",
	"LegacyGraphics":        "LegacyGraphics",
	"CharData":              "CharData",
	"SelectedCharName":      "SelectedCharName",
	"LastGameName":          "LastGameName",
	"LastGamePassword":      "LastGamePassword",
}

// DiobyteRequiredOffsets lists the scanner names for every offset
// that Diobyte d2go's calculateOffsets must return, in struct order.
var DiobyteRequiredOffsets = []string{
	"GameData",
	"UnitTable",
	"UI",
	"Hover",
	"Expansion",
	"Roster",
	"PanelManagerContainer",
	"WidgetStates",
	"Waypoints",
	"FPS",
	"KeyBindings",
	"KeyBindingsSkills",
	"QuestInfo",
	"TerrorZones",
	"Quests",
	"Ping",
	"LegacyGraphics",
	"CharData",
	"SelectedCharName",
	"LastGameName",
	"LastGamePassword",
}
