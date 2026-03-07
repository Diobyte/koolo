package game

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/koolo/internal/utils/winproc"
	"github.com/lxn/win"
)

const (
	keyPressMinTime = 30 // ms
	keyPressMaxTime = 65 // ms
)

// PressKey receives an ASCII code and sends a key press event to the game window
func (hid *HID) PressKey(key byte) {
	win.PostMessage(hid.gr.HWND, win.WM_KEYDOWN, uintptr(key), hid.calculatelParam(key, true))
	sleepTime := biasedLowRand(keyPressMinTime, keyPressMaxTime)
	time.Sleep(time.Duration(sleepTime) * time.Millisecond)
	win.PostMessage(hid.gr.HWND, win.WM_KEYUP, uintptr(key), hid.calculatelParam(key, false))
}

func (hid *HID) KeySequence(keysToPress ...byte) {
	for _, key := range keysToPress {
		hid.PressKey(key)
		gap := rand.Intn(51) + 80 // 80–130ms random gap
		time.Sleep(time.Duration(gap) * time.Millisecond)
	}
}

// PressKeyWithModifier works the same as PressKey but with a modifier key (shift, ctrl, alt)
func (hid *HID) PressKeyWithModifier(key byte, modifier ModifierKey) {
	if err := hid.gi.OverrideGetKeyState(byte(modifier)); err != nil {
		hid.gi.logger.Error(fmt.Sprintf("override key state failed: %v", err))
	}
	hid.PressKey(key)
	if err := hid.gi.RestoreGetKeyState(); err != nil {
		hid.gi.logger.Error(fmt.Sprintf("restore key state failed: %v", err))
	}
}

func (hid *HID) PressKeyBinding(kb data.KeyBinding) {
	keys := getKeysForKB(kb)
	if keys[1] == 0 || keys[1] == 255 {
		hid.PressKey(keys[0])
		return
	}

	hid.PressKeyWithModifier(keys[0], ModifierKey(keys[1]))
}

// KeyDown sends a key down event to the game window
func (hid *HID) KeyDown(kb data.KeyBinding) {
	keys := getKeysForKB(kb)
	win.PostMessage(hid.gr.HWND, win.WM_KEYDOWN, uintptr(keys[0]), hid.calculatelParam(keys[0], true))
}

// KeyUp sends a key up event to the game window
func (hid *HID) KeyUp(kb data.KeyBinding) {
	keys := getKeysForKB(kb)
	win.PostMessage(hid.gr.HWND, win.WM_KEYUP, uintptr(keys[0]), hid.calculatelParam(keys[0], false))
}

func getKeysForKB(kb data.KeyBinding) [2]byte {
	if kb.Key1[0] == 0 || kb.Key1[0] == 255 {
		return [2]byte{kb.Key2[0], kb.Key2[1]}
	}

	return [2]byte{kb.Key1[0], kb.Key1[1]}
}

func (hid *HID) GetASCIICode(key string) byte {
	char, found := specialChars[strings.ToLower(key)]
	if found {
		return char
	}

	return strings.ToUpper(key)[0]
}

var specialChars = map[string]byte{
	"esc":       win.VK_ESCAPE,
	"enter":     win.VK_RETURN,
	"f1":        win.VK_F1,
	"f2":        win.VK_F2,
	"f3":        win.VK_F3,
	"f4":        win.VK_F4,
	"f5":        win.VK_F5,
	"f6":        win.VK_F6,
	"f7":        win.VK_F7,
	"f8":        win.VK_F8,
	"f9":        win.VK_F9,
	"f10":       win.VK_F10,
	"f11":       win.VK_F11,
	"f12":       win.VK_F12,
	"lctrl":     win.VK_LCONTROL,
	"home":      win.VK_HOME,
	"down":      win.VK_DOWN,
	"up":        win.VK_UP,
	"left":      win.VK_LEFT,
	"right":     win.VK_RIGHT,
	"tab":       win.VK_TAB,
	"space":     win.VK_SPACE,
	"alt":       win.VK_MENU,
	"lalt":      win.VK_LMENU,
	"ralt":      win.VK_RMENU,
	"shift":     win.VK_LSHIFT,
	"backspace": win.VK_BACK,
	"lwin":      win.VK_LWIN,
	"rwin":      win.VK_RWIN,
	"end":       win.VK_END,
	"-":         win.VK_OEM_MINUS,
}

// biasedLowRand returns a random int in [lo, hi) biased toward the low end.
// It uses the minimum of two uniform samples to skew the distribution.
func biasedLowRand(lo, hi int) int {
	span := hi - lo
	a := rand.Intn(span)
	b := rand.Intn(span)
	if a < b {
		return lo + a
	}
	return lo + b
}

func (hid *HID) calculatelParam(keyCode byte, down bool) uintptr {
	ret, _, _ := winproc.MapVirtualKey.Call(uintptr(keyCode), 0)
	scanCode := int(ret)
	repeatCount := 1
	extendedKeyFlag := 0
	contextCode := 0
	previousKeyState := 0
	transitionState := 0
	if !down {
		transitionState = 1
	}

	lParam := uintptr((repeatCount & 0xFFFF) | (scanCode << 16) | (extendedKeyFlag << 24) | (contextCode << 29) | (previousKeyState << 30) | (transitionState << 31))
	return lParam
}
