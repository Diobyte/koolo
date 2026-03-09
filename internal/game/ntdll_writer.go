package game

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ntdll                      = windows.NewLazySystemDLL("ntdll.dll")
	procNtWriteVirtualMemory   = ntdll.NewProc("NtWriteVirtualMemory")
	procNtReadVirtualMemory    = ntdll.NewProc("NtReadVirtualMemory")
	procNtOpenProcess          = ntdll.NewProc("NtOpenProcess")
	procNtClose                = ntdll.NewProc("NtClose")
	procNtProtectVirtualMemory = ntdll.NewProc("NtProtectVirtualMemory")
)

const scLen = 11

type scProc struct {
	addr uintptr
}

var (
	scWrite   scProc
	scRead    scProc
	scOpen    scProc
	scClose   scProc
	scProtect scProc

	scOnce sync.Once
	scErr  error
)

func scInit() error {
	scOnce.Do(func() {
		entries := []struct {
			name string
			proc *scProc
		}{
			{"NtWriteVirtualMemory", &scWrite},
			{"NtReadVirtualMemory", &scRead},
			{"NtOpenProcess", &scOpen},
			{"NtClose", &scClose},
			{"NtProtectVirtualMemory", &scProtect},
		}
		for _, e := range entries {
			ssn, ok := extractNtSSN(e.name)
			if !ok {
				scErr = fmt.Errorf("proc init: %s unavailable", e.name)
				return
			}
			addr, err := scBuild(ssn)
			if err != nil {
				scErr = fmt.Errorf("proc init: stub for %s: %w", e.name, err)
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
		if data[funcOff] == 0x4C && data[funcOff+1] == 0x8B &&
			data[funcOff+2] == 0xD1 && data[funcOff+3] == 0xB8 {
			ssn := binary.LittleEndian.Uint32(data[funcOff+4:])
			return ssn, true
		}
		return 0, false
	}
	return 0, false
}

func secureZero(buf []byte) {
	if len(buf) == 0 {
		return
	}
	for i := range buf {
		buf[i] = 0
	}
	_ = buf[len(buf)-1]
}

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
			return 0, fmt.Errorf("open: 0x%08X", r)
		}
		return handle, nil
	}

	if err := procNtOpenProcess.Find(); err != nil {
		return 0, fmt.Errorf("open: unavailable: %w", err)
	}
	r, _, _ := procNtOpenProcess.Call(
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

func ntClose(handle windows.Handle) error {
	if scInit() == nil {
		r, _, _ := syscall.SyscallN(scClose.addr, uintptr(handle))
		if r != 0 {
			return fmt.Errorf("close: 0x%08X", r)
		}
		return nil
	}

	if err := procNtClose.Find(); err != nil {
		return windows.CloseHandle(handle)
	}
	r, _, _ := procNtClose.Call(uintptr(handle))
	if r != 0 {
		return fmt.Errorf("close: 0x%08X", r)
	}
	return nil
}

func ntWriteMemory(handle windows.Handle, addr uintptr, buf *byte, size uintptr) error {
	var bytesWritten uintptr

	if scInit() == nil {
		r, _, _ := syscall.SyscallN(scWrite.addr,
			uintptr(handle),
			addr,
			uintptr(unsafe.Pointer(buf)),
			size,
			uintptr(unsafe.Pointer(&bytesWritten)),
		)
		if r != 0 {
			return fmt.Errorf("write: 0x%08X", r)
		}
		if bytesWritten != size {
			return fmt.Errorf("write: partial (%d/%d)", bytesWritten, size)
		}
		return nil
	}

	if err := procNtWriteVirtualMemory.Find(); err != nil {
		return fmt.Errorf("write: unavailable: %w", err)
	}
	r, _, _ := procNtWriteVirtualMemory.Call(
		uintptr(handle),
		addr,
		uintptr(unsafe.Pointer(buf)),
		size,
		uintptr(unsafe.Pointer(&bytesWritten)),
	)
	if r != 0 {
		return fmt.Errorf("write: 0x%08X", r)
	}
	if bytesWritten != size {
		return fmt.Errorf("write: partial (%d/%d)", bytesWritten, size)
	}
	return nil
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
			return fmt.Errorf("read: 0x%08X", r)
		}
		if bytesRead != size {
			return fmt.Errorf("read: partial (%d/%d)", bytesRead, size)
		}
		return nil
	}

	if err := procNtReadVirtualMemory.Find(); err != nil {
		return fmt.Errorf("read: unavailable: %w", err)
	}
	r, _, _ := procNtReadVirtualMemory.Call(
		uintptr(handle),
		addr,
		uintptr(unsafe.Pointer(buf)),
		size,
		uintptr(unsafe.Pointer(&bytesRead)),
	)
	if r != 0 {
		return fmt.Errorf("read: 0x%08X", r)
	}
	if bytesRead != size {
		return fmt.Errorf("read: partial (%d/%d)", bytesRead, size)
	}
	return nil
}

func perCallReturn1() []byte {
	b := make([]byte, 1)
	if _, err := rand.Read(b); err == nil {
		variant := int(b[0]) % 5
		switch variant {
		case 0:
			return []byte{0xB8, 0x01, 0x00, 0x00, 0x00, 0xC3}
		case 1:
			return []byte{0x31, 0xC0, 0xFF, 0xC0, 0xC3}
		case 2:
			return []byte{0x31, 0xC0, 0xB0, 0x01, 0xC3}
		case 3:
			return []byte{0x6A, 0x01, 0x58, 0xC3}
		default:
			return []byte{0x31, 0xC9, 0x8D, 0x41, 0x01, 0xC3}
		}
	}
	return []byte{0x31, 0xC9, 0x8D, 0x41, 0x01, 0xC3}
}

func polyGetCursorPosPerCall(x, y int) []byte {
	b := make([]byte, 1)
	variant := 0
	if _, err := rand.Read(b); err == nil {
		variant = int(b[0]) % 3
	}

	xBytes := make([]byte, 4)
	yBytes := make([]byte, 4)
	xBytes[0] = byte(x)
	xBytes[1] = byte(x >> 8)
	xBytes[2] = byte(x >> 16)
	xBytes[3] = byte(x >> 24)
	yBytes[0] = byte(y)
	yBytes[1] = byte(y >> 8)
	yBytes[2] = byte(y >> 16)
	yBytes[3] = byte(y >> 24)

	switch variant {
	case 0:
		code := []byte{0x50, 0x48, 0x89, 0xC8,
			0xC7, 0x00, 0x00, 0x00, 0x00, 0x00,
			0xC7, 0x40, 0x04, 0x00, 0x00, 0x00, 0x00,
			0x58, 0xB0, 0x01, 0xC3}
		copy(code[6:10], xBytes)
		copy(code[13:17], yBytes)
		return code

	case 1:
		code := []byte{
			0xC7, 0x01, 0x00, 0x00, 0x00, 0x00,
			0xC7, 0x41, 0x04, 0x00, 0x00, 0x00, 0x00,
			0xB8, 0x01, 0x00, 0x00, 0x00,
			0xC3,
		}
		copy(code[2:6], xBytes)
		copy(code[9:13], yBytes)
		return code

	default:
		code := []byte{
			0xC7, 0x01, 0x00, 0x00, 0x00, 0x00,
			0xC7, 0x41, 0x04, 0x00, 0x00, 0x00, 0x00,
			0x31, 0xC0,
			0xFF, 0xC0,
			0xC3,
		}
		copy(code[2:6], xBytes)
		copy(code[9:13], yBytes)
		return code
	}
}

func polyGetKeyStateHookPerCall(key byte) []byte {
	b := make([]byte, 1)
	variant := 0
	if _, err := rand.Read(b); err == nil {
		variant = int(b[0]) % 3
	}
	switch variant {
	case 0:
		return []byte{0x80, 0xF9, key, 0x0F, 0x94, 0xC0, 0x66, 0xC1, 0xE0, 0x0F, 0xC3}
	case 1:
		return []byte{
			0x31, 0xC0,
			0x80, 0xF9, key,
			0x75, 0x04,
			0x66, 0xB8, 0x00, 0x80,
			0xC3,
		}
	default:
		return []byte{
			0x31, 0xC0,
			0x80, 0xF9, key,
			0x0F, 0x94, 0xC0,
			0x66, 0xF7, 0xD8,
			0x66, 0x25, 0x00, 0x80,
			0xC3,
		}
	}
}

// polyGetKeyStateHookDual patches GetKeyState to return 0x8000 (key pressed)
// when the queried key matches EITHER key1 or key2. This enables simultaneous
// modifier combinations like Ctrl+Shift for ROTW direct-to-cube transfers.
// Three variants are selected randomly to vary the byte pattern on each call,
// matching the polymorphic style of polyGetKeyStateHookPerCall.
// All variants fit within the 18-byte getKeyStateOrigBytes buffer.
func polyGetKeyStateHookDual(key1, key2 byte) []byte {
	b := make([]byte, 1)
	variant := 0
	if _, err := rand.Read(b); err == nil {
		variant = int(b[0]) % 3
	}
	switch variant {
	case 0:
		// key1 first, 17 bytes
		return []byte{
			0x31, 0xC0, // xor eax, eax
			0x80, 0xF9, key1, // cmp cl, key1
			0x74, 0x05, // je pressed (+5 → 0x0C)
			0x80, 0xF9, key2, // cmp cl, key2
			0x75, 0x04, // jne done (+4 → 0x10)
			0x66, 0xB8, 0x00, 0x80, // pressed: mov ax, 0x8000
			0xC3, // done: ret
		}
	case 1:
		// key2 first, nop prefix, 18 bytes
		return []byte{
			0x90,       // nop
			0x31, 0xC0, // xor eax, eax
			0x80, 0xF9, key2, // cmp cl, key2
			0x74, 0x05, // je pressed (+5 → 0x0D)
			0x80, 0xF9, key1, // cmp cl, key1
			0x75, 0x04, // jne done (+4 → 0x11)
			0x66, 0xB8, 0x00, 0x80, // pressed: mov ax, 0x8000
			0xC3, // done: ret
		}
	default:
		// key2 first, nop before ret, 18 bytes
		return []byte{
			0x31, 0xC0, // xor eax, eax
			0x80, 0xF9, key2, // cmp cl, key2
			0x74, 0x05, // je pressed (+5 → 0x0C)
			0x80, 0xF9, key1, // cmp cl, key1
			0x75, 0x05, // jne done (+5 → 0x11)
			0x66, 0xB8, 0x00, 0x80, // pressed: mov ax, 0x8000
			0x90, // nop
			0xC3, // done: ret
		}
	}
}

func ntProtectMemory(handle windows.Handle, addr uintptr, size uintptr, newProtect uint32) (uint32, error) {
	baseAddr := addr
	regionSize := size
	var oldProtect uint32

	if scInit() == nil {
		r, _, _ := syscall.SyscallN(scProtect.addr,
			uintptr(handle),
			uintptr(unsafe.Pointer(&baseAddr)),
			uintptr(unsafe.Pointer(&regionSize)),
			uintptr(newProtect),
			uintptr(unsafe.Pointer(&oldProtect)),
		)
		if r != 0 {
			return 0, fmt.Errorf("protect: 0x%08X", r)
		}
		return oldProtect, nil
	}

	if err := procNtProtectVirtualMemory.Find(); err != nil {
		return 0, fmt.Errorf("protect: unavailable: %w", err)
	}
	r, _, _ := procNtProtectVirtualMemory.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&baseAddr)),
		uintptr(unsafe.Pointer(&regionSize)),
		uintptr(newProtect),
		uintptr(unsafe.Pointer(&oldProtect)),
	)
	if r != 0 {
		return 0, fmt.Errorf("protect: 0x%08X", r)
	}
	return oldProtect, nil
}

func ntWriteCode(handle windows.Handle, addr uintptr, buf *byte, size uintptr) error {
	oldProt, err := ntProtectMemory(handle, addr, size, windows.PAGE_EXECUTE_READWRITE)
	if err != nil {
		return fmt.Errorf("pre-write %w", err)
	}
	err = ntWriteMemory(handle, addr, buf, size)
	_, _ = ntProtectMemory(handle, addr, size, oldProt)
	return err
}

func writeAndClean(handle windows.Handle, addr uintptr, code []byte) error {
	err := ntWriteCode(handle, addr, &code[0], uintptr(len(code)))
	secureZero(code)
	return err
}
