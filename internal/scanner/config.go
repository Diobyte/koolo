//go:build windows

package scanner

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ScannerConfig defines a JSON configuration for the offset scanner,
// supporting custom signatures, manual overrides, and cross-references.
// Inspired by the GH-Offset-Dumper config format.
type ScannerConfig struct {
	Executable string            `json:"executable"`
	Signatures []SigEntry        `json:"signatures"`
	Overrides  map[string]string `json:"overrides"`
	CrossRefs  []CrossRef        `json:"crossRefs"`
}

// SigEntry is a user-defined pattern signature in the config.
type SigEntry struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Type    string `json:"type"` // "relative32add" (default), "absolute", "relative32"
}

// CrossRef derives an offset from another resolved offset plus a byte delta.
type CrossRef struct {
	Name    string `json:"name"`
	BaseRef string `json:"baseRef"`
	Delta   int64  `json:"delta"`
}

// LoadConfig reads a ScannerConfig from a JSON file.
func LoadConfig(path string) (*ScannerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg ScannerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

// ConfigSignatures converts config entries to SignatureDef values.
// Entries with empty patterns are skipped.
func (cfg *ScannerConfig) ConfigSignatures() []SignatureDef {
	sigs := make([]SignatureDef, 0, len(cfg.Signatures))
	for _, e := range cfg.Signatures {
		if e.Pattern == "" {
			continue
		}
		ot := Relative32Add
		switch strings.ToLower(e.Type) {
		case "absolute":
			ot = Absolute
		case "relative32":
			ot = Relative32
		}
		sigs = append(sigs, SignatureDef{
			Name:    e.Name,
			Pattern: e.Pattern,
			Type:    ot,
		})
	}
	return sigs
}

// ParseOverrides converts hex-string overrides to a name→offset map.
func (cfg *ScannerConfig) ParseOverrides() (map[string]uintptr, error) {
	result := make(map[string]uintptr, len(cfg.Overrides))
	for name, hexVal := range cfg.Overrides {
		clean := strings.TrimPrefix(strings.TrimPrefix(hexVal, "0x"), "0X")
		v, err := strconv.ParseUint(clean, 16, 64)
		if err != nil {
			return nil, fmt.Errorf("bad override %s=%q: %w", name, hexVal, err)
		}
		result[name] = uintptr(v)
	}
	return result, nil
}

// MergeSignatures combines built-in and custom signatures.
// Custom entries override built-in entries sharing the same name.
func MergeSignatures(builtIn, custom []SignatureDef) []SignatureDef {
	seen := make(map[string]int, len(builtIn))
	result := make([]SignatureDef, len(builtIn))
	copy(result, builtIn)
	for i, s := range result {
		seen[s.Name] = i
	}
	for _, s := range custom {
		if idx, ok := seen[s.Name]; ok {
			result[idx] = s
		} else {
			seen[s.Name] = len(result)
			result = append(result, s)
		}
	}
	return result
}
