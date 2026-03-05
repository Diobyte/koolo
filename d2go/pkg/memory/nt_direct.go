//go:build windows

package memory

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const ntStubSize = 11

type ntProc struct {
	addr uintptr
}

var (
	dpNtOpen  ntProc
	dpNtRead  ntProc
	dpNtWrite ntProc
	dpInitErr error
	dpOnce    sync.Once
)

func dpInit() error {
	dpOnce.Do(func() {
		pairs := []struct {
			name string
			proc *ntProc
		}{
			{"NtOpenProcess", &dpNtOpen},
			{"NtReadVirtualMemory", &dpNtRead},
			{"NtWriteVirtualMemory", &dpNtWrite},
		}
		for _, p := range pairs {
			ssn, ok := dpExtractSSN(p.name)
			if !ok {
				dpInitErr = fmt.Errorf("proc init: %s unavailable", p.name)
				return
			}
			addr, err := dpBuildStub(ssn)
			if err != nil {
				dpInitErr = fmt.Errorf("proc init: stub for %s: %w", p.name, err)
				return
			}
			p.proc.addr = addr
		}
	})
	return dpInitErr
}

func dpBuildStub(ssn uint32) (uintptr, error) {
	mem, err := windows.VirtualAlloc(0, ntStubSize,
		windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		return 0, err
	}
	p := (*[ntStubSize]byte)(unsafe.Pointer(mem))
	p[0] = 0x4C
	p[1] = 0x8B
	p[2] = 0xD1
	p[3] = 0xB8
	p[4] = byte(ssn)
	p[5] = byte(ssn >> 8)
	p[6] = byte(ssn >> 16)
	p[7] = byte(ssn >> 24)
	p[8] = 0x0F
	p[9] = 0x05
	p[10] = 0xC3
	var old uint32
	if err = windows.VirtualProtect(mem, ntStubSize, windows.PAGE_EXECUTE_READ, &old); err != nil {
		_ = windows.VirtualFree(mem, 0, windows.MEM_RELEASE)
		return 0, err
	}
	return mem, nil
}

func dpExtractSSN(name string) (uint32, bool) {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	data, err := os.ReadFile(root + `\System32\ntdll.dll`)
	if err != nil {
		return 0, false
	}
	return dpParseSSN(data, name)
}

func dpParseSSN(data []byte, name string) (uint32, bool) {
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
		if string(data[nameOff:end]) != name {
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
		if data[funcOff] == 0x4C && data[funcOff+1] == 0x8B &&
			data[funcOff+2] == 0xD1 && data[funcOff+3] == 0xB8 {
			return binary.LittleEndian.Uint32(data[funcOff+4:]), true
		}
		return 0, false
	}
	return 0, false
}

type dpClientID struct {
	UniqueProcess uintptr
	UniqueThread  uintptr
}

type dpObjectAttributes struct {
	Length                   uint32
	RootDirectory            uintptr
	ObjectName               uintptr
	Attributes               uint32
	SecurityDescriptor       uintptr
	SecurityQualityOfService uintptr
}

func ntOpenProcess(pid uint32, access uint32) (windows.Handle, error) {
	var handle windows.Handle
	cid := dpClientID{UniqueProcess: uintptr(pid)}
	oa := dpObjectAttributes{Length: uint32(unsafe.Sizeof(dpObjectAttributes{}))}

	if dpInit() == nil {
		r, _, _ := syscall.SyscallN(dpNtOpen.addr,
			uintptr(unsafe.Pointer(&handle)),
			uintptr(access),
			uintptr(unsafe.Pointer(&oa)),
			uintptr(unsafe.Pointer(&cid)),
		)
		if r != 0 {
			return 0, fmt.Errorf("open: 0x%08X", r)
		}
		return handle, nil
	}

	h, err := windows.OpenProcess(access, false, pid)
	return h, err
}

func ntReadMemory(handle windows.Handle, addr uintptr, buf *byte, size uintptr) error {
	var bytesRead uintptr

	if dpInit() == nil {
		r, _, _ := syscall.SyscallN(dpNtRead.addr,
			uintptr(handle),
			addr,
			uintptr(unsafe.Pointer(buf)),
			size,
			uintptr(unsafe.Pointer(&bytesRead)),
		)
		if r != 0 {
			return fmt.Errorf("read: 0x%08X", r)
		}
		return nil
	}

	return windows.ReadProcessMemory(handle, addr, buf, size, &bytesRead)
}

func ntWriteMemory(handle windows.Handle, addr uintptr, buf *byte, size uintptr) error {
	var bytesWritten uintptr

	if dpInit() == nil {
		r, _, _ := syscall.SyscallN(dpNtWrite.addr,
			uintptr(handle),
			addr,
			uintptr(unsafe.Pointer(buf)),
			size,
			uintptr(unsafe.Pointer(&bytesWritten)),
		)
		if r != 0 {
			return fmt.Errorf("write: 0x%08X", r)
		}
		return nil
	}

	return windows.WriteProcessMemory(handle, addr, buf, size, &bytesWritten)
}
