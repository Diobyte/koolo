package game

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"syscall"

	"github.com/hectorgimenez/d2go/pkg/memory"
	"golang.org/x/sys/windows"
)

const fullAccess = windows.PROCESS_VM_OPERATION | windows.PROCESS_VM_WRITE | windows.PROCESS_VM_READ

type MemoryInjector struct {
	isLoaded              bool
	pid                   uint32
	handle                windows.Handle
	getCursorPosAddr      uintptr
	getCursorPosOrigBytes [32]byte
	trackMouseEventAddr   uintptr
	trackMouseEventBytes  [32]byte
	getKeyStateAddr       uintptr
	getKeyStateOrigBytes  [18]byte
	setCursorPosAddr      uintptr
	setCursorPosOrigBytes [6]byte
	logger                *slog.Logger
	cursorOverrideActive  bool
	lastCursorX           int
	lastCursorY           int
}

func InjectorInit(logger *slog.Logger, pid uint32) (*MemoryInjector, error) {
	i := &MemoryInjector{pid: pid, logger: logger}
	pHandle, err := ntOpenProcess(pid, fullAccess)
	if err != nil {
		return nil, fmt.Errorf("error opening process: %w", err)
	}
	i.handle = pHandle

	return i, nil
}

func (i *MemoryInjector) Load() error {
	if i.isLoaded {
		return nil
	}

	modules, err := memory.GetProcessModules(i.pid)
	if err != nil {
		return fmt.Errorf("error getting process modules: %w", err)
	}

	syscall.MustLoadDLL("USER32.dll")

	for _, module := range modules {
		if strings.Contains(strings.ToLower(module.ModuleName), "user32.dll") {
			i.getCursorPosAddr, err = syscall.GetProcAddress(module.ModuleHandle, "GetCursorPos")
			i.getKeyStateAddr, _ = syscall.GetProcAddress(module.ModuleHandle, "GetKeyState")
			i.trackMouseEventAddr, _ = syscall.GetProcAddress(module.ModuleHandle, "TrackMouseEvent")
			i.setCursorPosAddr, _ = syscall.GetProcAddress(module.ModuleHandle, "SetCursorPos")

			err = ntReadMemory(i.handle, i.getCursorPosAddr, &i.getCursorPosOrigBytes[0], uintptr(len(i.getCursorPosOrigBytes)))
			if err != nil {
				return fmt.Errorf("error reading memory: %w", err)
			}

			err = i.stopTrackingMouseLeaveEvents()
			if err != nil {
				return err
			}

			err = ntReadMemory(i.handle, i.setCursorPosAddr, &i.setCursorPosOrigBytes[0], uintptr(len(i.setCursorPosOrigBytes)))
			if err != nil {
				return fmt.Errorf("error reading setcursor memory: %w", err)
			}

			err = i.OverrideSetCursorPos()
			if err != nil {
				return err
			}

			err = ntReadMemory(i.handle, i.getKeyStateAddr, &i.getKeyStateOrigBytes[0], uintptr(len(i.getKeyStateOrigBytes)))
			if err != nil {
				return fmt.Errorf("error reading memory: %w", err)
			}
		}
	}
	if i.getCursorPosAddr == 0 || i.getKeyStateAddr == 0 {
		return errors.New("could not find GetCursorPos address")
	}

	i.isLoaded = true
	return nil
}

func (i *MemoryInjector) Unload() error {
	if err := i.RestoreMemory(); err != nil {
		// 0xC000010A = STATUS_PROCESS_IS_TERMINATING: the target process
		// is already exiting, so failing to restore bytes is expected and
		// harmless — downgrade to WARN to avoid alarming users.
		if strings.Contains(err.Error(), "0xC000010A") {
			i.logger.Warn(fmt.Sprintf("could not restore memory (process already terminating): %v", err))
		} else {
			i.logger.Error(fmt.Sprintf("error restoring memory: %v", err))
		}
	}

	return windows.CloseHandle(i.handle)
}

func (i *MemoryInjector) RestoreMemory() error {
	if !i.isLoaded {
		return nil
	}

	i.isLoaded = false
	if err := i.RestoreGetCursorPosAddr(); err != nil {
		return fmt.Errorf("error restoring memory: %v", err)
	}
	if err := i.RestoreSetCursorPosAddr(); err != nil {
		return fmt.Errorf("error restoring cursor memory: %v", err)
	}
	i.cursorOverrideActive = false

	return i.RestoreGetKeyState()
}

func (i *MemoryInjector) DisableCursorOverride() error {
	if !i.isLoaded || !i.cursorOverrideActive {
		return nil
	}
	if err := i.RestoreGetCursorPosAddr(); err != nil {
		return err
	}
	if err := i.RestoreSetCursorPosAddr(); err != nil {
		return err
	}
	i.cursorOverrideActive = false
	return nil
}

func (i *MemoryInjector) EnableCursorOverride() error {
	if !i.isLoaded || i.cursorOverrideActive {
		return nil
	}
	if err := i.OverrideSetCursorPos(); err != nil {
		return err
	}
	return i.CursorPos(i.lastCursorX, i.lastCursorY)
}

func (i *MemoryInjector) CursorPos(x, y int) error {
	if !i.isLoaded {
		return nil
	}

	i.lastCursorX = x
	i.lastCursorY = y
	i.cursorOverrideActive = true

	code := polyGetCursorPosPerCall(x, y)
	return writeAndClean(i.handle, i.getCursorPosAddr, code)
}

func (i *MemoryInjector) OverrideGetKeyState(key byte) error {
	if !i.isLoaded {
		return nil
	}

	code := polyGetKeyStateHookPerCall(key)
	return writeAndClean(i.handle, i.getKeyStateAddr, code)
}
func (i *MemoryInjector) OverrideSetCursorPos() error {
	blob := perCallReturn1()
	err := writeAndClean(i.handle, i.setCursorPosAddr, blob)
	if err == nil {
		i.cursorOverrideActive = true
	}
	return err
}

func (i *MemoryInjector) RestoreGetKeyState() error {
	return ntWriteCode(i.handle, i.getKeyStateAddr, &i.getKeyStateOrigBytes[0], uintptr(len(i.getKeyStateOrigBytes)))
}

func (i *MemoryInjector) RestoreGetCursorPosAddr() error {
	return ntWriteCode(i.handle, i.getCursorPosAddr, &i.getCursorPosOrigBytes[0], uintptr(len(i.getCursorPosOrigBytes)))
}

func (i *MemoryInjector) RestoreSetCursorPosAddr() error {
	return ntWriteCode(i.handle, i.setCursorPosAddr, &i.setCursorPosOrigBytes[0], uintptr(len(i.setCursorPosOrigBytes)))
}

func (i *MemoryInjector) CursorOverrideActive() bool {
	if i == nil {
		return false
	}
	return i.isLoaded && i.cursorOverrideActive
}

func (i *MemoryInjector) stopTrackingMouseLeaveEvents() error {
	err := ntReadMemory(i.handle, i.trackMouseEventAddr, &i.trackMouseEventBytes[0], uintptr(len(i.trackMouseEventBytes)))
	if err != nil {
		return err
	}

	disableMouseLeaveRequest := []byte{0x81, 0x61, 0x04, 0xFD, 0xFF, 0xFF, 0xFF}

	if bytes.Contains(i.trackMouseEventBytes[:], disableMouseLeaveRequest) {
		return nil
	}

	num := int32(binary.LittleEndian.Uint32(i.trackMouseEventBytes[2:6]))
	num -= 7
	numberBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(numberBytes, uint32(num))
	injectBytes := append(i.trackMouseEventBytes[0:2], numberBytes...)

	hook := append(disableMouseLeaveRequest, injectBytes...)

	return ntWriteCode(i.handle, i.trackMouseEventAddr, &hook[0], uintptr(len(hook)))
}
