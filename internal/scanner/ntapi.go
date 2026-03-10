//go:build windows

package scanner

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ── syscall stub builder ────────────────────────────────────────────────────

const scLen = 11

type scProc struct {
	addr uintptr
}

var (
	scRead scProc
	scOpen scProc

	scOnce sync.Once
	scErr  error
)

func scInit() error {
	scOnce.Do(func() {
		entries := []struct {
			name string
			proc *scProc
		}{
			{"NtReadVirtualMemory", &scRead},
			{"NtOpenProcess", &scOpen},
		}
		for _, e := range entries {
			ssn, ok := extractNtSSN(e.name)
			if !ok {
				scErr = fmt.Errorf("scanner: ssn extraction failed for %s", e.name)
				return
			}
			addr, err := scBuild(ssn)
			if err != nil {
				scErr = fmt.Errorf("scanner: stub build for %s: %w", e.name, err)
				return
			}
			e.proc.addr = addr
		}
	})
	return scErr
}

func scBuild(ssn uint32) (uintptr, error) {
	mem, err := windows.VirtualAlloc(0, scLen,
		windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		return 0, err
	}
	p := (*[scLen]byte)(unsafe.Pointer(mem))
	p[0] = 0x4C // mov r10, rcx
	p[1] = 0x8B
	p[2] = 0xD1
	p[3] = 0xB8 // mov eax, <ssn>
	p[4] = byte(ssn)
	p[5] = byte(ssn >> 8)
	p[6] = byte(ssn >> 16)
	p[7] = byte(ssn >> 24)
	p[8] = 0x0F // syscall
	p[9] = 0x05
	p[10] = 0xC3 // ret
	var old uint32
	if err = windows.VirtualProtect(mem, scLen, windows.PAGE_EXECUTE_READ, &old); err != nil {
		_ = windows.VirtualFree(mem, 0, windows.MEM_RELEASE)
		return 0, err
	}
	return mem, nil
}

func extractNtSSN(funcName string) (uint32, bool) {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	data, err := os.ReadFile(root + `\System32\ntdll.dll`)
	if err != nil {
		return 0, false
	}
	return parseNtSSN(data, funcName)
}

func parseNtSSN(data []byte, funcName string) (uint32, bool) {
	if len(data) < 0x40 || data[0] != 'M' || data[1] != 'Z' {
		return 0, false
	}
	peOff := int(binary.LittleEndian.Uint32(data[0x3C:]))
	if peOff+24 > len(data) || string(data[peOff:peOff+4]) != "PE\x00\x00" {
		return 0, false
	}

	optOff := peOff + 24
	if optOff+120 > len(data) {
		return 0, false
	}
	optSize := int(binary.LittleEndian.Uint16(data[peOff+20:]))
	numSect := int(binary.LittleEndian.Uint16(data[peOff+6:]))
	sectBase := peOff + 24 + optSize

	rvaToOff := func(rva int) int {
		for s := 0; s < numSect; s++ {
			b := sectBase + s*40
			if b+40 > len(data) {
				break
			}
			vAddr := int(binary.LittleEndian.Uint32(data[b+12:]))
			vSize := int(binary.LittleEndian.Uint32(data[b+8:]))
			rawOff := int(binary.LittleEndian.Uint32(data[b+20:]))
			if rva >= vAddr && rva < vAddr+vSize {
				return rawOff + (rva - vAddr)
			}
		}
		return -1
	}

	expRVA := int(binary.LittleEndian.Uint32(data[optOff+112:]))
	expOff := rvaToOff(expRVA)
	if expOff < 0 || expOff+40 > len(data) {
		return 0, false
	}

	numNames := int(binary.LittleEndian.Uint32(data[expOff+24:]))
	funcRVAs := rvaToOff(int(binary.LittleEndian.Uint32(data[expOff+28:])))
	nameRVAs := rvaToOff(int(binary.LittleEndian.Uint32(data[expOff+32:])))
	ordRVAs := rvaToOff(int(binary.LittleEndian.Uint32(data[expOff+36:])))
	if funcRVAs < 0 || nameRVAs < 0 || ordRVAs < 0 {
		return 0, false
	}

	for i := 0; i < numNames; i++ {
		nameRVA := int(binary.LittleEndian.Uint32(data[nameRVAs+i*4:]))
		nameOff := rvaToOff(nameRVA)
		if nameOff < 0 {
			continue
		}
		end := nameOff
		for end < len(data) && data[end] != 0 {
			end++
		}
		if string(data[nameOff:end]) != funcName {
			continue
		}
		ord := int(binary.LittleEndian.Uint16(data[ordRVAs+i*2:]))
		if ordRVAs+i*2+2 > len(data) || funcRVAs+ord*4+4 > len(data) {
			return 0, false
		}
		funcRVA := int(binary.LittleEndian.Uint32(data[funcRVAs+ord*4:]))
		funcOff := rvaToOff(funcRVA)
		if funcOff < 0 || funcOff+8 > len(data) {
			return 0, false
		}
		// mov r10,rcx; mov eax,<ssn>
		if data[funcOff] == 0x4C && data[funcOff+1] == 0x8B &&
			data[funcOff+2] == 0xD1 && data[funcOff+3] == 0xB8 {
			ssn := binary.LittleEndian.Uint32(data[funcOff+4:])
			return ssn, true
		}
		return 0, false
	}
	return 0, false
}

// ── low-level NT wrappers ───────────────────────────────────────────────────

// Fallback procs (only used if syscall stubs fail).
var (
	ntdll                   = windows.NewLazySystemDLL("ntdll.dll")
	procNtReadVirtualMemory = ntdll.NewProc("NtReadVirtualMemory")
	procNtOpenProcess       = ntdll.NewProc("NtOpenProcess")
)

type clientID struct {
	UniqueProcess uintptr
	UniqueThread  uintptr
}

type objectAttributes struct {
	Length                   uint32
	RootDirectory            uintptr
	ObjectName               uintptr
	Attributes               uint32
	SecurityDescriptor       uintptr
	SecurityQualityOfService uintptr
}

func ntOpenProcess(pid uint32, access uint32) (windows.Handle, error) {
	var handle windows.Handle
	cid := clientID{UniqueProcess: uintptr(pid)}
	oa := objectAttributes{Length: uint32(unsafe.Sizeof(objectAttributes{}))}

	if scInit() == nil {
		r, _, _ := syscall.SyscallN(scOpen.addr,
			uintptr(unsafe.Pointer(&handle)),
			uintptr(access),
			uintptr(unsafe.Pointer(&oa)),
			uintptr(unsafe.Pointer(&cid)),
		)
		if r != 0 {
			return 0, fmt.Errorf("NtOpenProcess: 0x%08X", r)
		}
		return handle, nil
	}

	if err := procNtOpenProcess.Find(); err != nil {
		return 0, fmt.Errorf("NtOpenProcess unavailable: %w", err)
	}
	r, _, _ := procNtOpenProcess.Call(
		uintptr(unsafe.Pointer(&handle)),
		uintptr(access),
		uintptr(unsafe.Pointer(&oa)),
		uintptr(unsafe.Pointer(&cid)),
	)
	if r != 0 {
		return 0, fmt.Errorf("NtOpenProcess: 0x%08X", r)
	}
	return handle, nil
}

func ntReadMemory(handle windows.Handle, addr uintptr, buf *byte, size uintptr) error {
	var bytesRead uintptr

	if scInit() == nil {
		r, _, _ := syscall.SyscallN(scRead.addr,
			uintptr(handle),
			addr,
			uintptr(unsafe.Pointer(buf)),
			size,
			uintptr(unsafe.Pointer(&bytesRead)),
		)
		if r != 0 {
			return fmt.Errorf("NtReadVirtualMemory: 0x%08X", r)
		}
		return nil
	}

	if err := procNtReadVirtualMemory.Find(); err != nil {
		return fmt.Errorf("NtReadVirtualMemory unavailable: %w", err)
	}
	r, _, _ := procNtReadVirtualMemory.Call(
		uintptr(handle),
		addr,
		uintptr(unsafe.Pointer(buf)),
		size,
		uintptr(unsafe.Pointer(&bytesRead)),
	)
	if r != 0 {
		return fmt.Errorf("NtReadVirtualMemory: 0x%08X", r)
	}
	return nil
}
