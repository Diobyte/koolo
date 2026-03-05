package game

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/lxn/win"
)

const (
	RightButton MouseButton = win.MK_RBUTTON
	LeftButton  MouseButton = win.MK_LBUTTON

	ShiftKey ModifierKey = win.VK_SHIFT
	CtrlKey  ModifierKey = win.VK_CONTROL
)

type MouseButton uint
type ModifierKey byte

const pointerReleaseDelay = 150 * time.Millisecond

// MovePointer moves the mouse to the requested position, x and y should be the final position based on
// pixels shown in the screen. Top-left corner is 0,0
func (hid *HID) MovePointer(x, y int) {
	hid.gr.updateWindowPositionData()
	screenX := hid.gr.WindowLeftX + x
	screenY := hid.gr.WindowTopY + y

	if err := hid.gi.CursorPos(screenX, screenY); err != nil {
		hid.gi.logger.Error(fmt.Sprintf("cursor pos inject failed: %v", err))
	}
	screenLParam := calculateLparam(screenX, screenY)
	clientLParam := calculateLparam(x, y)
	win.SendMessage(hid.gr.HWND, win.WM_NCHITTEST, 0, screenLParam)
	win.SendMessage(hid.gr.HWND, win.WM_SETCURSOR, 0x000105A8, 0x2010001)
	win.PostMessage(hid.gr.HWND, win.WM_MOUSEMOVE, 0, clientLParam)
}

// Click just does a single mouse click at current pointer position
func (hid *HID) Click(btn MouseButton, x, y int) {
	hid.MovePointer(x, y)

	lParam := calculateLparam(x, y)
	buttonDown := uint32(win.WM_LBUTTONDOWN)
	buttonUp := uint32(win.WM_LBUTTONUP)
	wParam := uintptr(win.MK_LBUTTON)
	if btn == RightButton {
		buttonDown = win.WM_RBUTTONDOWN
		buttonUp = win.WM_RBUTTONUP
		wParam = uintptr(win.MK_RBUTTON)
	}

	win.SendMessage(hid.gr.HWND, buttonDown, wParam, lParam)
	sleepTime := rand.Intn(keyPressMaxTime-keyPressMinTime) + keyPressMinTime
	time.Sleep(time.Duration(sleepTime) * time.Millisecond)
	win.SendMessage(hid.gr.HWND, buttonUp, wParam, lParam)
}

func (hid *HID) ClickWithModifier(btn MouseButton, x, y int, modifier ModifierKey) {
	if err := hid.gi.OverrideGetKeyState(byte(modifier)); err != nil {
		hid.gi.logger.Error(fmt.Sprintf("override key state failed: %v", err))
	}
	hid.Click(btn, x, y)
	if err := hid.gi.RestoreGetKeyState(); err != nil {
		hid.gi.logger.Error(fmt.Sprintf("restore key state failed: %v", err))
	}
}

func calculateLparam(x, y int) uintptr {
	return uintptr(y<<16 | x)
}
