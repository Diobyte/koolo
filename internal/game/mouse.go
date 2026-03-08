package game

import (
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

// MovePointer moves the mouse to the requested position, x and y should be the final position based on
// pixels shown in the screen. Top-left corner is 0,0
func (hid *HID) MovePointer(x, y int) {
	if !hid.gr.IsWindowValid() {
		return
	}
	hid.gr.updateWindowPositionData()
	x = hid.gr.WindowLeftX + x
	y = hid.gr.WindowTopY + y

	hid.gi.CursorPos(x, y)
	lParam := calculateLparam(x, y)
	win.PostMessage(hid.gr.HWND, win.WM_NCHITTEST, 0, lParam)
	win.PostMessage(hid.gr.HWND, win.WM_SETCURSOR, 0x000105A8, 0x2010001)
	win.PostMessage(hid.gr.HWND, win.WM_MOUSEMOVE, 0, lParam)
}

// Click does a single mouse click with a small random jitter (±2px)
// to break pixel-perfect repetition heuristics.
func (hid *HID) Click(btn MouseButton, x, y int) {
	if !hid.gr.IsWindowValid() {
		return
	}
	// Apply ±2px jitter to the click coordinates
	x += rand.Intn(5) - 2
	y += rand.Intn(5) - 2

	hid.MovePointer(x, y)
	x = hid.gr.WindowLeftX + x
	y = hid.gr.WindowTopY + y

	lParam := calculateLparam(x, y)
	buttonDown := uint32(win.WM_LBUTTONDOWN)
	buttonUp := uint32(win.WM_LBUTTONUP)
	if btn == RightButton {
		buttonDown = win.WM_RBUTTONDOWN
		buttonUp = win.WM_RBUTTONUP
	}

	if !hid.gr.IsWindowValid() {
		return
	}
	win.PostMessage(hid.gr.HWND, buttonDown, 1, lParam)
	sleepTime := biasedLowRand(keyPressMinTime, keyPressMaxTime)
	time.Sleep(time.Duration(sleepTime) * time.Millisecond)
	if !hid.gr.IsWindowValid() {
		return
	}
	win.PostMessage(hid.gr.HWND, buttonUp, 1, lParam)
}

func (hid *HID) ClickWithModifier(btn MouseButton, x, y int, modifier ModifierKey) {
	hid.gi.OverrideGetKeyState(byte(modifier))
	hid.Click(btn, x, y)
	hid.gi.RestoreGetKeyState()
}

func calculateLparam(x, y int) uintptr {
	return uintptr(y<<16 | x)
}
