//go:build windows

// Command scanner attaches to a running D2R process, parses the PE header
// once, reads the module image in 4 KB pages (skipping guard pages), and
// scans for byte patterns to resolve game offsets dynamically.
//
// Usage:
//
//	scanner.exe [-pid N] [-core] [-json] [-verify] [-config scanner.json] [-go]
package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/hectorgimenez/koolo/internal/scanner"
)

func main() {
	pid := flag.Uint("pid", 0, "attach to a specific PID (0 = auto-detect)")
	coreOnly := flag.Bool("core", false, "scan only core d2go offsets")
	jsonOut := flag.Bool("json", false, "output results as JSON")
	verify := flag.Bool("verify", false, "read back resolved addresses for validation")
	configPath := flag.String("config", "", "path to scanner.json config with custom sigs/overrides")
	goOutput := flag.Bool("go", false, "generate Go source for Diobyte offset.go")
	flag.Parse()

	// ── Phase 0: load config (optional) ────────────────────────────────
	var cfg *scanner.ScannerConfig
	var cfgOverrides map[string]uintptr
	if *configPath != "" {
		var loadErr error
		cfg, loadErr = scanner.LoadConfig(*configPath)
		if loadErr != nil {
			fmt.Fprintf(os.Stderr, "[scanner] WARNING: config load failed: %v\n", loadErr)
		} else {
			fmt.Printf("[scanner] Loaded config: %s\n", *configPath)
			if len(cfg.Overrides) > 0 {
				cfgOverrides, loadErr = cfg.ParseOverrides()
				if loadErr != nil {
					fmt.Fprintf(os.Stderr, "[scanner] WARNING: bad overrides: %v\n", loadErr)
				} else {
					fmt.Printf("[scanner]   %d manual overrides loaded\n", len(cfgOverrides))
				}
			}
			if len(cfg.Signatures) > 0 {
				fmt.Printf("[scanner]   %d custom signatures loaded\n", len(cfg.Signatures))
			}
		}
	}

	// ── Phase 1: attach ─────────────────────────────────────────────────
	fmt.Println("[scanner] Phase 1: attaching to D2R process...")
	randomDelay(50, 150)

	var proc *scanner.ProcessInfo
	var err error
	if *pid != 0 {
		proc, err = scanner.AttachPID(uint32(*pid))
	} else {
		proc, err = scanner.Attach()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[scanner] FATAL: %v\n", err)
		os.Exit(1)
	}
	defer proc.Close()

	fmt.Printf("[scanner]   PID        = %d\n", proc.PID)
	fmt.Printf("[scanner]   Base       = 0x%X\n", proc.BaseAddr)
	fmt.Printf("[scanner]   Image size = 0x%X (%d MB)\n",
		proc.ModuleSize, proc.ModuleSize/(1024*1024))

	// ── Phase 2: parse PE sections ──────────────────────────────────────
	fmt.Println("[scanner] Phase 2: parsing PE sections...")
	randomDelay(30, 100)

	sections, err := proc.ParsePESections()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[scanner] FATAL: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[scanner]   Found %d sections:\n", len(sections))
	for _, s := range sections {
		tag := ""
		if s.Characteristics&0x20 != 0 { // IMAGE_SCN_CNT_CODE
			tag = " [CODE]"
		}
		if s.Characteristics&0x40 != 0 { // IMAGE_SCN_CNT_INITIALIZED_DATA
			tag += " [DATA]"
		}
		fmt.Printf("[scanner]     %-8s  VA=0x%08X  Size=0x%08X  Flags=0x%08X%s\n",
			s.Name, s.VirtualAddress, s.VirtualSize, s.Characteristics, tag)
	}

	// ── Phase 3: read module memory (4 KB pages) ────────────────────────
	fmt.Println("[scanner] Phase 3: reading module memory (4 KB pages)...")
	randomDelay(20, 80)

	t0 := time.Now()
	buf, err := proc.ReadModuleMemory()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[scanner] FATAL: %v\n", err)
		os.Exit(1)
	}
	readDur := time.Since(t0)
	fmt.Printf("[scanner]   Read %d bytes in %v\n", len(buf), readDur.Round(time.Millisecond))

	// ── Phase 4: pattern scan ───────────────────────────────────────────
	var sigs []scanner.SignatureDef
	if *coreOnly {
		sigs = scanner.D2RCoreSignatures()
	} else {
		sigs = scanner.D2RSignatures()
	}
	// Merge in any custom config signatures
	if cfg != nil {
		customSigs := cfg.ConfigSignatures()
		if len(customSigs) > 0 {
			sigs = scanner.MergeSignatures(sigs, customSigs)
		}
	}
	fmt.Printf("[scanner] Phase 4: scanning %d patterns...\n", len(sigs))
	randomDelay(10, 60)

	t1 := time.Now()

	// Scan within code sections for function patterns, full buffer for data
	codeSections := scanner.SectionsContaining(sections, 0x20) // IMAGE_SCN_CNT_CODE
	found, err := scanner.ScanSections(buf, proc.BaseAddr, codeSections, sigs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[scanner] WARNING: %v\n", err)
	}

	// Retry unfound patterns against full buffer (some data patterns aren't in code sections)
	var unfound []int
	for i := range sigs {
		if !sigs[i].Found {
			unfound = append(unfound, i)
		}
	}
	if len(unfound) > 0 {
		for _, idx := range unfound {
			subSigs := []scanner.SignatureDef{sigs[idx]}
			n, _ := scanner.ScanAll(buf, proc.BaseAddr, subSigs)
			if n > 0 {
				sigs[idx] = subSigs[0]
				found++
			}
		}
	}

	scanDur := time.Since(t1)
	fmt.Printf("[scanner]   Resolved %d/%d offsets in %v\n",
		found, len(sigs), scanDur.Round(time.Microsecond))

	// Track how each offset was resolved
	sources := make(map[string]string, len(sigs))
	for _, s := range sigs {
		if s.Found {
			sources[s.Name] = "scanned"
		}
	}

	// ── Phase 5: verify (optional) ──────────────────────────────────────
	if *verify {
		fmt.Println("[scanner] Phase 5: verifying resolved addresses...")
		randomDelay(10, 40)
		verified := 0
		for i := range sigs {
			if !sigs[i].Found {
				continue
			}
			// Try to read 8 bytes from the resolved address
			b := proc.ReadBytes(sigs[i].Resolved, 8)
			nonZero := false
			for _, v := range b {
				if v != 0 {
					nonZero = true
					break
				}
			}
			if nonZero {
				verified++
			} else {
				fmt.Printf("[scanner]   WARNING: %s at 0x%X reads as all zeros\n",
					sigs[i].Name, sigs[i].Resolved)
			}
		}
		fmt.Printf("[scanner]   Verified %d/%d addresses are readable\n", verified, found)
	}

	// ── Phase 6: store in obfuscated vault ──────────────────────────────
	store := scanner.NewObfuscatedStore()
	store.StoreAll(sigs)
	fmt.Printf("[scanner] Phase 6: stored %d offsets in XOR-encrypted vault\n", store.Count())

	// ── Phase 6b: apply config overrides ────────────────────────────────
	if len(cfgOverrides) > 0 {
		var applied int
		sigs, applied = applyOverrides(sigs, cfgOverrides, proc.BaseAddr)
		for _, s := range sigs {
			if s.Found && sources[s.Name] == "" {
				sources[s.Name] = "override"
			}
		}
		fmt.Printf("[scanner] Phase 6b: applied %d manual overrides\n", applied)
	}

	// ── Phase 6c: apply cross-references ────────────────────────────────
	if cfg != nil && len(cfg.CrossRefs) > 0 {
		xApplied := applyCrossRefs(sigs, cfg.CrossRefs)
		for _, s := range sigs {
			if s.Found && sources[s.Name] == "" {
				sources[s.Name] = "crossref"
			}
		}
		fmt.Printf("[scanner] Phase 6c: applied %d cross-references\n", xApplied)
	}

	// ── Phase 7: output report ──────────────────────────────────────────
	if *jsonOut {
		printJSON(sigs, proc)
	} else {
		printTable(sigs, proc)
	}

	// ── Phase 8: cross-reference with Diobyte hardcoded offsets ─────────
	printDiobyteComparison(sigs)

	// ── Phase 9: required offset coverage ───────────────────────────────
	missingCount := printCoverageReport(sigs, sources)

	// ── Phase 10: generate Go source for offset.go ─────────────────────
	if *goOutput {
		generateGoSource(sigs)
	}

	if missingCount > 0 {
		fmt.Fprintf(os.Stderr, "\n[scanner] ERROR: %d required offset(s) could not be resolved.\n", missingCount)
		fmt.Fprintf(os.Stderr, "[scanner] Add overrides in scanner.json or update patterns in signatures.go.\n")
		os.Exit(2)
	}
}

func printTable(sigs []scanner.SignatureDef, proc *scanner.ProcessInfo) {
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════")
	fmt.Printf("  D2R Pattern Scanner — PID %d, Base 0x%X\n", proc.PID, proc.BaseAddr)
	fmt.Println("═══════════════════════════════════════════════════════════════════════════")

	// Sort by name for readability
	sorted := make([]scanner.SignatureDef, len(sigs))
	copy(sorted, sigs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintf(w, "  STATUS\tNAME\tOFFSET\tABSOLUTE\n")
	fmt.Fprintf(w, "  ──────\t────\t──────\t────────\n")

	foundCount := 0
	for _, s := range sorted {
		status := "  MISS"
		offset := "—"
		abs := "—"
		if s.Found {
			status = "  OK  "
			offset = scanner.FormatOffset(s.Offset)
			abs = fmt.Sprintf("0x%X", s.Resolved)
			foundCount++
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", status, s.Name, offset, abs)
	}
	w.Flush()

	fmt.Println()
	fmt.Printf("  Total: %d/%d resolved\n", foundCount, len(sigs))
	fmt.Println("═══════════════════════════════════════════════════════════════════════════")
}

type jsonResult struct {
	PID     uint32       `json:"pid"`
	Base    string       `json:"base"`
	Size    uint32       `json:"size"`
	Found   int          `json:"found"`
	Total   int          `json:"total"`
	Offsets []jsonOffset `json:"offsets"`
}

type jsonOffset struct {
	Name     string `json:"name"`
	Found    bool   `json:"found"`
	Offset   string `json:"offset,omitempty"`
	Absolute string `json:"absolute,omitempty"`
	Pattern  string `json:"pattern"`
}

func printJSON(sigs []scanner.SignatureDef, proc *scanner.ProcessInfo) {
	found := 0
	var offsets []jsonOffset
	for _, s := range sigs {
		jo := jsonOffset{
			Name:    s.Name,
			Found:   s.Found,
			Pattern: s.Pattern,
		}
		if s.Found {
			jo.Offset = scanner.FormatOffset(s.Offset)
			jo.Absolute = fmt.Sprintf("0x%X", s.Resolved)
			found++
		}
		offsets = append(offsets, jo)
	}

	result := jsonResult{
		PID:     proc.PID,
		Base:    fmt.Sprintf("0x%X", proc.BaseAddr),
		Size:    proc.ModuleSize,
		Found:   found,
		Total:   len(sigs),
		Offsets: offsets,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(result)
}

// diobyte known hardcoded offsets for comparison (from Diobyte/d2go offset.go).
// GameData and Quests are struct fields but NOT populated in calculateOffsets.
var diobyteOffsets = map[string]uintptr{
	"UnitTable":             0x1E9E350,
	"UI":                    0x1EAE04A,
	"Hover":                 0x1DF2000,
	"Expansion":             0x1DF1468,
	"Roster":                0x1EB4668,
	"PanelManagerContainer": 0x1E08DC0,
	"WidgetStates":          0x1ED6680,
	"Waypoints":             0x1D503C0,
	"FPS":                   0x1D50394,
	"KeyBindings":           0x19C95B4,
	"KeyBindingsSkills":     0x1DF2110,
	"QuestInfo":             0x1EBACD8,
	"TerrorZones":           0x25A8AF0,
	"Ping":                  0x1DF1468,
	"LegacyGraphics":        0x1EBAF46,
	"CharData":              0x1DF55F8,
	"SelectedCharName":      0x1D47195,
	"LastGameName":          0x25F1450,
	"LastGamePassword":      0x25F14A8,
}

func printDiobyteComparison(sigs []scanner.SignatureDef) {
	fmt.Println()
	fmt.Println("── Diobyte hardcoded offset comparison ────────────────────────────────────")

	matches := 0
	mismatches := 0
	notFound := 0

	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintf(w, "  NAME\tSCANNED\tHARDCODED\tSTATUS\n")
	fmt.Fprintf(w, "  ────\t───────\t─────────\t──────\n")

	// Build lookup
	sigMap := make(map[string]*scanner.SignatureDef)
	for i := range sigs {
		sigMap[sigs[i].Name] = &sigs[i]
	}

	names := make([]string, 0, len(diobyteOffsets))
	for n := range diobyteOffsets {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		hardcoded := diobyteOffsets[name]
		sig, exists := sigMap[name]

		if !exists || !sig.Found {
			fmt.Fprintf(w, "  %s\t—\t0x%X\tNOT SCANNED\n", name, hardcoded)
			notFound++
			continue
		}

		scanned := sig.Offset
		status := "MATCH"
		if uint64(hardcoded) != scanned {
			status = "DIFFER"
			mismatches++
		} else {
			matches++
		}
		fmt.Fprintf(w, "  %s\t0x%X\t0x%X\t%s\n", name, scanned, hardcoded, status)
	}
	w.Flush()

	fmt.Printf("\n  Summary: %d match, %d differ, %d not scanned\n", matches, mismatches, notFound)

	if mismatches > 0 {
		fmt.Println("  NOTE: Differences are expected if your D2R version differs from the Diobyte fork's target.")
	}
	fmt.Println("────────────────────────────────────────────────────────────────────────────")
}

// printCoverageReport shows the resolution status of every required Diobyte offset.
// Returns the number of missing offsets.
func printCoverageReport(sigs []scanner.SignatureDef, sources map[string]string) int {
	fmt.Println()
	fmt.Println("══ REQUIRED OFFSET COVERAGE ════════════════════════════════════════════════")

	sigMap := make(map[string]*scanner.SignatureDef, len(sigs))
	for i := range sigs {
		sigMap[sigs[i].Name] = &sigs[i]
	}

	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintf(w, "  FIELD\tOFFSET\tSOURCE\tSTATUS\n")
	fmt.Fprintf(w, "  ─────\t──────\t──────\t──────\n")

	found := 0
	missing := 0
	for _, name := range scanner.DiobyteRequiredOffsets {
		field := scanner.DiobyteFieldMap[name]
		sig := sigMap[name]
		source := sources[name]

		if sig != nil && sig.Found {
			fmt.Fprintf(w, "  %s\t0x%X\t%s\tOK\n", field, sig.Offset, source)
			found++
		} else {
			fmt.Fprintf(w, "  %s\t—\t—\tMISSING\n", field)
			missing++
		}
	}
	w.Flush()

	total := len(scanner.DiobyteRequiredOffsets)
	fmt.Println()
	if missing > 0 {
		fmt.Printf("  Coverage: %d/%d required offsets resolved (%d MISSING)\n", found, total, missing)
		fmt.Println("  Use -config scanner.json with overrides to fill missing offsets.")
	} else {
		fmt.Printf("  Coverage: %d/%d required offsets resolved — COMPLETE\n", found, total)
	}
	fmt.Println("════════════════════════════════════════════════════════════════════════════")

	return missing
}

// randomDelay sleeps for a random duration between min and max milliseconds.
func randomDelay(minMs, maxMs int) {
	if minMs >= maxMs {
		time.Sleep(time.Duration(minMs) * time.Millisecond)
		return
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(maxMs-minMs)))
	if err != nil {
		time.Sleep(time.Duration(minMs) * time.Millisecond)
		return
	}
	time.Sleep(time.Duration(minMs+int(n.Int64())) * time.Millisecond)
}

// humanSize formats bytes into a human-readable string.
func humanSize(b uint32) string {
	const (
		kb = 1024
		mb = kb * 1024
	)
	switch {
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func init() {
	// Suppress the default flag usage prefix
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "D2R Pattern Scanner\n\n")
		fmt.Fprintf(os.Stderr, "Attaches to a running D2R process, parses PE headers, reads module\n")
		fmt.Fprintf(os.Stderr, "memory in 4KB pages, and scans for byte patterns to resolve game\n")
		fmt.Fprintf(os.Stderr, "offsets dynamically (no hardcoded addresses).\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  scanner.exe [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  scanner.exe                              Auto-detect D2R, scan all patterns\n")
		fmt.Fprintf(os.Stderr, "  scanner.exe -core                        Scan only core d2go offsets\n")
		fmt.Fprintf(os.Stderr, "  scanner.exe -json                        Output as JSON\n")
		fmt.Fprintf(os.Stderr, "  scanner.exe -pid 1234 -verify             Attach to specific PID, verify reads\n")
		fmt.Fprintf(os.Stderr, "  scanner.exe -config scanner.json          Load custom signatures/overrides\n")
		fmt.Fprintf(os.Stderr, "  scanner.exe -config scanner.json -go      Generate Go offset.go source\n")
	}
}

// applyOverrides injects manual hex overrides into the sig results.
// Returns the updated slice (may grow) and the number of overrides applied.
func applyOverrides(sigs []scanner.SignatureDef, overrides map[string]uintptr, moduleBase uintptr) ([]scanner.SignatureDef, int) {
	applied := 0
	for i := range sigs {
		if ov, ok := overrides[sigs[i].Name]; ok {
			sigs[i].Offset = uint64(ov)
			sigs[i].Resolved = moduleBase + ov
			sigs[i].Found = true
			applied++
		}
	}
	// Add overrides for names not in the sig list
	for name, ov := range overrides {
		found := false
		for _, s := range sigs {
			if s.Name == name {
				found = true
				break
			}
		}
		if !found {
			sigs = append(sigs, scanner.SignatureDef{
				Name:     name,
				Pattern:  "(manual override)",
				Offset:   uint64(ov),
				Resolved: moduleBase + ov,
				Found:    true,
			})
			applied++
		}
	}
	return sigs, applied
}

// applyCrossRefs derives offsets from other resolved offsets.
func applyCrossRefs(sigs []scanner.SignatureDef, refs []scanner.CrossRef) int {
	sigMap := make(map[string]*scanner.SignatureDef, len(sigs))
	for i := range sigs {
		sigMap[sigs[i].Name] = &sigs[i]
	}
	applied := 0
	for _, ref := range refs {
		base, ok := sigMap[ref.BaseRef]
		if !ok || !base.Found {
			fmt.Fprintf(os.Stderr, "[scanner] WARNING: cross-ref %q base %q not found\n", ref.Name, ref.BaseRef)
			continue
		}
		derived := int64(base.Offset) + ref.Delta
		if derived < 0 {
			fmt.Fprintf(os.Stderr, "[scanner] WARNING: cross-ref %q resolved negative offset\n", ref.Name)
			continue
		}
		if target, exists := sigMap[ref.Name]; exists {
			target.Offset = uint64(derived)
			target.Resolved = uintptr(int64(base.Resolved) + ref.Delta)
			target.Found = true
		}
		applied++
	}
	return applied
}

// generateGoSource prints a Go source snippet for Diobyte's offset.go.
func generateGoSource(sigs []scanner.SignatureDef) {
	// Build lookup from scanner names
	sigMap := make(map[string]*scanner.SignatureDef, len(sigs))
	for i := range sigs {
		sigMap[sigs[i].Name] = &sigs[i]
	}

	fmt.Println()
	fmt.Println("── Generated Go source for offset.go ──────────────────────────────────────")
	fmt.Println()
	fmt.Println("// Auto-generated by scanner.exe — paste into Diobyte/d2go offset.go")
	fmt.Println("func calculateOffsets(_ *Process) Offset {")
	fmt.Println("\treturn Offset{")

	missing := 0
	for _, scannerName := range scanner.DiobyteRequiredOffsets {
		fieldName := scanner.DiobyteFieldMap[scannerName]
		sig, ok := sigMap[scannerName]
		if ok && sig.Found {
			fmt.Printf("\t\t%-28s uintptr(0x%X),\n", fieldName+":", sig.Offset)
		} else {
			// Fall back to the hardcoded Diobyte value with a warning comment
			if hc, known := diobyteOffsets[scannerName]; known {
				fmt.Printf("\t\t%-28s uintptr(0x%X), // WARNING: not scanned, using previous hardcoded value\n", fieldName+":", hc)
			} else {
				fmt.Printf("\t\t%-28s 0, // ERROR: no pattern and no known value\n", fieldName+":")
			}
			missing++
		}
	}

	fmt.Println("\t}")
	fmt.Println("}")
	fmt.Println()

	if missing > 0 {
		fmt.Printf("  WARNING: %d offsets could not be resolved by pattern scanning.\n", missing)
		fmt.Println("  Add patterns or overrides in scanner.json to fill the gaps.")
	} else {
		fmt.Println("  All Diobyte offsets resolved successfully.")
	}

	// Also write to file
	outPath := filepath.Join(".", "offset_generated.go")
	f, err := os.Create(outPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[scanner] WARNING: could not write %s: %v\n", outPath, err)
		return
	}
	defer f.Close()

	fmt.Fprintln(f, "package memory")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "// Code generated by scanner.exe. DO NOT EDIT.")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "func calculateOffsets(_ *Process) Offset {")
	fmt.Fprintln(f, "\treturn Offset{")

	for _, scannerName := range scanner.DiobyteRequiredOffsets {
		fieldName := scanner.DiobyteFieldMap[scannerName]
		sig, ok := sigMap[scannerName]
		if ok && sig.Found {
			fmt.Fprintf(f, "\t\t%-28s uintptr(0x%X),\n", fieldName+":", sig.Offset)
		} else if hc, known := diobyteOffsets[scannerName]; known {
			fmt.Fprintf(f, "\t\t%-28s uintptr(0x%X), // WARNING: not scanned\n", fieldName+":", hc)
		} else {
			fmt.Fprintf(f, "\t\t%-28s 0, // ERROR: unknown\n", fieldName+":")
		}
	}

	fmt.Fprintln(f, "\t}")
	fmt.Fprintln(f, "}")

	fmt.Printf("\n  Written to: %s\n", outPath)
	fmt.Println("────────────────────────────────────────────────────────────────────────────")
}
