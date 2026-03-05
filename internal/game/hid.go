package game

import "sync/atomic"

type HID struct {
	gr *MemoryReader
	gi *MemoryInjector

	// SigmaDrift human-like mouse trajectory.
	sigmaDriftEnabled bool
	sigmaDriftCfg     SigmaDriftConfig
	lastClientX       atomic.Int32
	lastClientY       atomic.Int32
}

func NewHID(gr *MemoryReader, gi *MemoryInjector) *HID {
	return &HID{
		gr: gr,
		gi: gi,
	}
}

// SetSigmaDrift enables or disables human-like mouse trajectory generation.
// When enabled, Click operations animate the cursor along a biomechanical
// trajectory instead of teleporting it instantly to the target.
func (hid *HID) SetSigmaDrift(enabled bool, cfg SigmaDriftConfig) {
	hid.sigmaDriftEnabled = enabled
	hid.sigmaDriftCfg = cfg
}
