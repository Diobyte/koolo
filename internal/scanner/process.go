//go:build windows

// Package scanner provides a remote-process pattern scanner for D2R.
// It reads the target PE once into a local buffer (4 KB pages), then scans
// for byte patterns with wildcard / offset-marker support.
package scanner

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const moduleName = "d2r.exe"

// ProcessInfo describes an attached remote process.
type ProcessInfo struct {
	PID        uint32
	Handle     windows.Handle
	BaseAddr   uintptr
	ModuleSize uint32
}

// PESection describes one section from the remote PE header.
type PESection struct {
	Name            string
	VirtualAddress  uint32
	VirtualSize     uint32
	RawDataSize     uint32
	Characteristics uint32
}

// Attach finds d2r.exe, opens a handle with VM_READ, and returns ProcessInfo.
func Attach() (*ProcessInfo, error) {
	pids := make([]uint32, 2048)
	var needed uint32
	if err := windows.EnumProcesses(pids, &needed); err != nil {
		return nil, fmt.Errorf("EnumProcesses: %w", err)
	}
	count := needed / 4
	for _, pid := range pids[:count] {
		if pid == 0 {
			continue
		}
		pi, ok := probeModule(pid)
		if ok {
			return pi, nil
		}
	}
	return nil, fmt.Errorf("d2r.exe not found")
}

// AttachPID opens a specific process by PID.
func AttachPID(pid uint32) (*ProcessInfo, error) {
	pi, ok := probeModule(pid)
	if !ok {
		return nil, fmt.Errorf("d2r.exe module not found in PID %d", pid)
	}
	return pi, nil
}

func probeModule(pid uint32) (*ProcessInfo, bool) {
	h, err := ntOpenProcess(pid, windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ)
	if err != nil {
		return nil, false
	}

	var modules [1024]windows.Handle
	var needed uint32
	if err := windows.EnumProcessModules(h, &modules[0],
		uint32(unsafe.Sizeof(modules[0]))*1024, &needed); err != nil {
		windows.CloseHandle(h)
		return nil, false
	}
	modCount := needed / uint32(unsafe.Sizeof(modules[0]))

	for i := uint32(0); i < modCount; i++ {
		var mi windows.ModuleInfo
		if err := windows.GetModuleInformation(h, modules[i], &mi,
			uint32(unsafe.Sizeof(mi))); err != nil {
			continue
		}
		var nameBuf [windows.MAX_PATH]uint16
		if err := windows.GetModuleFileNameEx(h, modules[i], &nameBuf[0], windows.MAX_PATH); err != nil {
			continue
		}
		name := windows.UTF16ToString(nameBuf[:])
		if !strings.HasSuffix(strings.ToLower(name), moduleName) {
			continue
		}

		// Re-open with just VM_READ for scanning
		windows.CloseHandle(h)
		readH, err := ntOpenProcess(pid, windows.PROCESS_VM_READ|windows.PROCESS_QUERY_INFORMATION)
		if err != nil {
			return nil, false
		}

		return &ProcessInfo{
			PID:        pid,
			Handle:     readH,
			BaseAddr:   mi.BaseOfDll,
			ModuleSize: mi.SizeOfImage,
		}, true
	}

	windows.CloseHandle(h)
	return nil, false
}

// Close releases the process handle.
func (p *ProcessInfo) Close() {
	if p.Handle != 0 {
		windows.CloseHandle(p.Handle)
		p.Handle = 0
	}
}

// ParsePESections reads the DOS/NT headers from the remote process and
// returns the section table. This is done once at startup.
func (p *ProcessInfo) ParsePESections() ([]PESection, error) {
	// Read DOS header (64 bytes is enough for e_lfanew)
	dosHdr := make([]byte, 64)
	if err := ntReadMemory(p.Handle, p.BaseAddr, &dosHdr[0], 64); err != nil {
		return nil, fmt.Errorf("read DOS header: %w", err)
	}
	if dosHdr[0] != 'M' || dosHdr[1] != 'Z' {
		return nil, fmt.Errorf("invalid DOS signature")
	}
	peOffset := binary.LittleEndian.Uint32(dosHdr[0x3C:])

	// Read NT headers (signature + file header + start of optional header + enough for sections)
	// File header: 20 bytes, optional header size varies. Read a generous buffer.
	ntBuf := make([]byte, 4096)
	if err := ntReadMemory(p.Handle, p.BaseAddr+uintptr(peOffset), &ntBuf[0], 4096); err != nil {
		return nil, fmt.Errorf("read NT headers: %w", err)
	}
	if string(ntBuf[:4]) != "PE\x00\x00" {
		return nil, fmt.Errorf("invalid PE signature")
	}

	numSections := binary.LittleEndian.Uint16(ntBuf[6:])
	optHeaderSize := binary.LittleEndian.Uint16(ntBuf[20:])
	sectionsOffset := 24 + uint32(optHeaderSize) // relative to PE signature

	var sections []PESection
	for i := uint16(0); i < numSections; i++ {
		off := sectionsOffset + uint32(i)*40
		if int(off)+40 > len(ntBuf) {
			break
		}
		sec := ntBuf[off : off+40]
		name := strings.TrimRight(string(sec[:8]), "\x00")
		sections = append(sections, PESection{
			Name:            name,
			VirtualSize:     binary.LittleEndian.Uint32(sec[8:]),
			VirtualAddress:  binary.LittleEndian.Uint32(sec[12:]),
			RawDataSize:     binary.LittleEndian.Uint32(sec[16:]),
			Characteristics: binary.LittleEndian.Uint32(sec[36:]),
		})
	}

	if len(sections) == 0 {
		return nil, fmt.Errorf("no PE sections found")
	}
	return sections, nil
}

// ReadModuleMemory reads the entire module image into a local byte buffer
// using 4 KB page-aligned reads. Failed pages are zero-filled.
func (p *ProcessInfo) ReadModuleMemory() ([]byte, error) {
	size := uintptr(p.ModuleSize)
	buf := make([]byte, size)
	const pageSize = 4096

	for off := uintptr(0); off < size; off += pageSize {
		chunk := uintptr(pageSize)
		if off+pageSize > size {
			chunk = size - off
		}
		if err := ntReadMemory(p.Handle, p.BaseAddr+off, &buf[off], chunk); err != nil {
			// Zero-fill on failure (guard page, etc.)
			for i := off; i < off+chunk; i++ {
				buf[i] = 0
			}
		}
	}
	return buf, nil
}

// ReadBytes reads arbitrary bytes from the remote process.
func (p *ProcessInfo) ReadBytes(addr uintptr, size uint) []byte {
	buf := make([]byte, size)
	_ = ntReadMemory(p.Handle, addr, &buf[0], uintptr(size))
	return buf
}

// ReadUint32 reads a uint32 from the remote process.
func (p *ProcessInfo) ReadUint32(addr uintptr) uint32 {
	b := p.ReadBytes(addr, 4)
	return binary.LittleEndian.Uint32(b)
}

// ReadUint64 reads a uint64 from the remote process.
func (p *ProcessInfo) ReadUint64(addr uintptr) uint64 {
	b := p.ReadBytes(addr, 8)
	return binary.LittleEndian.Uint64(b)
}
