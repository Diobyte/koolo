//go:build windows

package scanner

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"unicode"
)

// OffsetType determines how the value at the ^ marker is resolved.
type OffsetType int

const (
	// Relative32Add — 32-bit RIP-relative: match_addr + ^pos + 4 + int32(val)
	Relative32Add OffsetType = iota
	// Relative32 — same as Relative32Add (alias kept for clarity)
	Relative32
	// Absolute — direct 8-byte pointer value at the ^ position
	Absolute
)

// ParsedPattern is the pre-processed form of a pattern string.
type ParsedPattern struct {
	Bytes          []byte // pattern bytes (0 for wildcards)
	Mask           []byte // 1 = must match, 0 = wildcard / offset marker
	HasOffsetMark  bool
	OffsetPosition int // index of ^ within the byte array
}

// SignatureDef describes one pattern to scan for.
type SignatureDef struct {
	Name     string
	Pattern  string
	Type     OffsetType
	Parsed   *ParsedPattern // lazily populated
	Offset   uint64         // relative offset from module base (populated after scan)
	Resolved uintptr        // absolute address (populated after scan)
	Found    bool
}

// ParsePattern converts a nyx-style pattern string to a ParsedPattern.
//
// Format:
//
//	hex bytes: "48 8B 0D" (space-separated)
//	wildcard:  "?" or "??" (matches any single byte)
//	offset:    "^" marks where to extract the offset value (also a wildcard byte)
//
// Example: "48 8B 0D ^ ? ? ?"
func ParsePattern(pattern string) (*ParsedPattern, error) {
	pp := &ParsedPattern{}
	tokens := strings.Fields(pattern)

	for _, tok := range tokens {
		if tok == "^" {
			if pp.HasOffsetMark {
				return nil, fmt.Errorf("duplicate ^ in pattern")
			}
			pp.OffsetPosition = len(pp.Bytes)
			pp.HasOffsetMark = true
			pp.Bytes = append(pp.Bytes, 0)
			pp.Mask = append(pp.Mask, 0)
			continue
		}

		if tok == "?" || tok == "??" {
			pp.Bytes = append(pp.Bytes, 0)
			pp.Mask = append(pp.Mask, 0)
			continue
		}

		// Must be a hex byte
		if len(tok) != 2 || !isHex(tok[0]) || !isHex(tok[1]) {
			return nil, fmt.Errorf("invalid token %q in pattern", tok)
		}
		b := hexVal(tok[0])<<4 | hexVal(tok[1])
		pp.Bytes = append(pp.Bytes, b)
		pp.Mask = append(pp.Mask, 1)
	}

	if len(pp.Bytes) == 0 {
		return nil, fmt.Errorf("empty pattern")
	}
	return pp, nil
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

// ScanBuffer searches the buffer for the pattern and returns the offset
// within buf, or -1 if not found.
func ScanBuffer(buf []byte, pp *ParsedPattern) int {
	if len(pp.Bytes) == 0 || len(buf) < len(pp.Bytes) {
		return -1
	}

	scanEnd := len(buf) - len(pp.Bytes)
	patLen := len(pp.Bytes)
	patBytes := pp.Bytes
	patMask := pp.Mask

	for i := 0; i <= scanEnd; i++ {
		match := true
		for j := 0; j < patLen; j++ {
			if patMask[j] != 0 && buf[i+j] != patBytes[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// ResolveOffset interprets the bytes at the ^ position according to the OffsetType.
// matchAddr is the absolute address of the match in the remote process.
// data points to the matched bytes in the local buffer copy.
func ResolveOffset(matchAddr uintptr, pp *ParsedPattern, offsetType OffsetType, data []byte) uintptr {
	if !pp.HasOffsetMark {
		return matchAddr
	}

	pos := pp.OffsetPosition
	switch offsetType {
	case Absolute:
		if pos+8 > len(data) {
			return 0
		}
		return uintptr(binary.LittleEndian.Uint64(data[pos:]))

	case Relative32, Relative32Add:
		if pos+4 > len(data) {
			return 0
		}
		rel := int32(binary.LittleEndian.Uint32(data[pos:]))
		// RIP-relative: instruction_addr + offset_pos + 4 (sizeof int32) + rel
		return matchAddr + uintptr(pos) + 4 + uintptr(rel)
	}
	return 0
}

// ScanAll resolves every signature against the provided buffer.
// moduleBase is the remote base address. Returns the count of found signatures.
func ScanAll(buf []byte, moduleBase uintptr, sigs []SignatureDef) (int, error) {
	found := 0
	for i := range sigs {
		sig := &sigs[i]

		if sig.Parsed == nil {
			pp, err := ParsePattern(sig.Pattern)
			if err != nil {
				return found, fmt.Errorf("pattern %q: %w", sig.Name, err)
			}
			sig.Parsed = pp
		}

		off := ScanBuffer(buf, sig.Parsed)
		if off < 0 {
			sig.Found = false
			sig.Resolved = 0
			sig.Offset = 0
			continue
		}

		matchAddr := moduleBase + uintptr(off)
		if !sig.Parsed.HasOffsetMark {
			sig.Resolved = matchAddr
		} else {
			sig.Resolved = ResolveOffset(matchAddr, sig.Parsed, sig.Type, buf[off:])
		}

		if sig.Resolved != 0 && sig.Resolved > moduleBase {
			sig.Offset = uint64(sig.Resolved - moduleBase)
		} else if sig.Resolved != 0 {
			sig.Offset = uint64(sig.Resolved)
		}
		sig.Found = true
		found++
	}
	return found, nil
}

// ── helpers for the upstream d2go legacy pattern format ──────────────────────

// ConvertLegacyPattern converts the old d2go style (raw byte string + mask)
// into a nyx-style pattern string. The legacy format uses:
//
//	pattern: raw bytes (e.g. "\x44\x88\x25...")
//	mask:    "xxx????xxxx????"  (x = match, ? = wildcard)
//
// The operand offset (if any) is at position 3 (the first ? after leading x's)
// for FindPatternByOperand. Pass operandPos=-1 if no ^ marker is needed.
func ConvertLegacyPattern(raw string, mask string, operandPos int) string {
	var sb strings.Builder
	for i := 0; i < len(raw) && i < len(mask); i++ {
		if i > 0 {
			sb.WriteByte(' ')
		}
		if i == operandPos {
			sb.WriteByte('^')
			continue
		}
		if mask[i] == '?' {
			sb.WriteByte('?')
		} else {
			sb.WriteString(fmt.Sprintf("%02X", raw[i]))
		}
	}
	return sb.String()
}

// PatternHash computes a simple hash of all pattern strings for cache
// invalidation purposes.
func PatternHash(sigs []SignatureDef) uint32 {
	h := uint32(0x811C9DC5) // FNV-1a offset basis
	for _, s := range sigs {
		for _, c := range s.Pattern {
			if unicode.IsSpace(c) {
				continue
			}
			h ^= uint32(c)
			h *= 0x01000193
		}
	}
	return h
}

// SectionsContaining returns a sub-slice of sections with the given
// characteristic flags (e.g. IMAGE_SCN_CNT_CODE = 0x20).
func SectionsContaining(sections []PESection, flags uint32) []PESection {
	var out []PESection
	for _, s := range sections {
		if s.Characteristics&flags != 0 {
			out = append(out, s)
		}
	}
	return out
}

// ScanSections performs pattern scanning only within the specified PE sections.
// This is more targeted than scanning the full module buffer.
func ScanSections(buf []byte, moduleBase uintptr, sections []PESection, sigs []SignatureDef) (int, error) {
	found := 0
	for i := range sigs {
		sig := &sigs[i]
		if sig.Parsed == nil {
			pp, err := ParsePattern(sig.Pattern)
			if err != nil {
				return found, fmt.Errorf("pattern %q: %w", sig.Name, err)
			}
			sig.Parsed = pp
		}

		for _, sec := range sections {
			start := int(sec.VirtualAddress)
			end := start + int(sec.VirtualSize)
			if start < 0 || end > len(buf) {
				continue
			}

			off := ScanBuffer(buf[start:end], sig.Parsed)
			if off < 0 {
				continue
			}

			globalOff := start + off
			matchAddr := moduleBase + uintptr(globalOff)
			if !sig.Parsed.HasOffsetMark {
				sig.Resolved = matchAddr
			} else {
				sig.Resolved = ResolveOffset(matchAddr, sig.Parsed, sig.Type, buf[globalOff:])
			}

			if sig.Resolved != 0 && sig.Resolved > moduleBase {
				sig.Offset = uint64(sig.Resolved - moduleBase)
			} else if sig.Resolved != 0 {
				sig.Offset = uint64(sig.Resolved)
			}
			sig.Found = true
			found++
			break
		}
	}
	return found, nil
}

// FormatOffset returns a human-readable hex representation of the offset,
// handling zero and very large values gracefully.
func FormatOffset(offset uint64) string {
	if offset == 0 {
		return "NOT FOUND"
	}
	if offset > math.MaxUint32 {
		return fmt.Sprintf("0x%016X", offset)
	}
	return fmt.Sprintf("0x%08X", offset)
}
