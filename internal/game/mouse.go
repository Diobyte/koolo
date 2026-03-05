package game

import (
	"fmt"
	"math"
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

// movePointerDirect moves the mouse cursor instantly to (x, y) in client coordinates.
// This is the low-level primitive used by both direct and animated movement.
func (hid *HID) movePointerDirect(x, y int) {
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

	hid.lastClientX.Store(int32(x))
	hid.lastClientY.Store(int32(y))
}

// MovePointer moves the mouse to the requested position, x and y should be the final position based on
// pixels shown in the screen. Top-left corner is 0,0
func (hid *HID) MovePointer(x, y int) {
	hid.movePointerDirect(x, y)
}

// movePointerAnimated moves the cursor from the current position to (x, y)
// along a SigmaDrift trajectory, stepping through intermediate points with
// appropriate timing to simulate human biomechanical mouse movement.
func (hid *HID) movePointerAnimated(x, y int) {
	startX := float64(hid.lastClientX.Load())
	startY := float64(hid.lastClientY.Load())
	endX := float64(x)
	endY := float64(y)

	dist := math.Hypot(endX-startX, endY-startY)
	if dist < 3.0 {
		hid.movePointerDirect(x, y)
		return
	}

	path := GenerateTrajectory(startX, startY, endX, endY, hid.sigmaDriftCfg)

	// Clamp trajectory points to valid window coordinates.
	maxX := hid.gr.GameAreaSizeX - 1
	maxY := hid.gr.GameAreaSizeY - 1

	for i, pt := range path {
		ptX := int(math.Round(pt.X))
		ptY := int(math.Round(pt.Y))
		if ptX < 0 {
			ptX = 0
		} else if ptX > maxX {
			ptX = maxX
		}
		if ptY < 0 {
			ptY = 0
		} else if ptY > maxY {
			ptY = maxY
		}
		hid.movePointerDirect(ptX, ptY)

		// Sleep until the next sample time.
		if i+1 < len(path) {
			dt := path[i+1].T - pt.T
			if dt > 0 {
				time.Sleep(time.Duration(dt * float64(time.Millisecond)))
			}
		}
	}

	// Ensure we land exactly on the target.
	hid.movePointerDirect(x, y)
}

// Click performs a single mouse click at (x, y). When SigmaDrift is enabled the
// cursor follows a human-like trajectory to the target before clicking, and a
// small random pixel offset (±3 px) is applied so clicks never land on the
// exact same coordinate twice.
func (hid *HID) Click(btn MouseButton, x, y int) {
	if hid.sigmaDriftEnabled {
		// Jitter the click target by a few pixels to avoid pixel-perfect repetition.
		jx := x + rand.Intn(7) - 3 // -3 … +3
		jy := y + rand.Intn(7) - 3
		if jx < 0 {
			jx = 0
		}
		if jy < 0 {
			jy = 0
		}
		hid.movePointerAnimated(jx, jy)
		x, y = jx, jy
	} else {
		hid.MovePointer(x, y)
	}

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
	return uintptr(uint16(y))<<16 | uintptr(uint16(x))
}
