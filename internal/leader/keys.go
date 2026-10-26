package leader

// Event types, the values of CGEventType.
const (
	EventKeyDown      = 10
	EventKeyUp        = 11
	EventFlagsChanged = 12
)

// Device-dependent modifier bits of CGEventFlags (NX_DEVICE*KEYMASK in
// IOLLEvent.h), which tell the left and right keys apart.
const (
	deviceLCtl   = 0x00000001
	deviceLShift = 0x00000002
	deviceRShift = 0x00000004
	deviceLCmd   = 0x00000008
	deviceRCmd   = 0x00000010
	deviceLAlt   = 0x00000020
	deviceRAlt   = 0x00000040
	deviceRCtl   = 0x00002000
	// flagSecondaryFn is kCGEventFlagMaskSecondaryFn.
	flagSecondaryFn = 0x00800000
)

// trigger is a key that holds a leader.
type trigger struct {
	keycode int64
	// mask is the flag bit that is set while a modifier trigger is down;
	// zero for an ordinary key, which reports keyDown and keyUp.
	mask uint64
}

func (t trigger) isModifier() bool { return t.mask != 0 }

// functionKeys are the keycodes of F1-F20, which may be both triggers
// and entry keys.
var functionKeys = map[string]int64{
	"f1": 122, "f2": 120, "f3": 99, "f4": 118, "f5": 96, "f6": 97,
	"f7": 98, "f8": 100, "f9": 101, "f10": 109, "f11": 103, "f12": 111,
	"f13": 105, "f14": 107, "f15": 113, "f16": 106, "f17": 64,
	"f18": 79, "f19": 80, "f20": 90,
}

func init() {
	for name, keycode := range functionKeys {
		triggers[name] = trigger{keycode: keycode}
		entryKeys[name] = keycode
	}
}

// triggers are the keys a leader file may be named after. Caps Lock is
// not supported. Function keys are added from functionKeys.
var triggers = map[string]trigger{
	"cmd_l":   {55, deviceLCmd},
	"cmd_r":   {54, deviceRCmd},
	"opt_l":   {58, deviceLAlt},
	"opt_r":   {61, deviceRAlt},
	"ctrl_l":  {59, deviceLCtl},
	"ctrl_r":  {62, deviceRCtl},
	"shift_l": {56, deviceLShift},
	"shift_r": {60, deviceRShift},
	"fn":      {63, flagSecondaryFn},
}

// entryKeys maps the key names an entry may use to virtual keycodes of
// the ANSI layout. Keycodes are physical positions, so on other layouts a
// name stands for the key at its ANSI position. Function keys are added
// from functionKeys.
var entryKeys = map[string]int64{
	"a": 0, "s": 1, "d": 2, "f": 3, "h": 4, "g": 5, "z": 6, "x": 7,
	"c": 8, "v": 9, "b": 11, "q": 12, "w": 13, "e": 14, "r": 15,
	"y": 16, "t": 17, "1": 18, "2": 19, "3": 20, "4": 21, "6": 22,
	"5": 23, "=": 24, "9": 25, "7": 26, "-": 27, "8": 28, "0": 29,
	"]": 30, "o": 31, "u": 32, "[": 33, "i": 34, "p": 35, "l": 37,
	"j": 38, "'": 39, "k": 40, ";": 41, `\`: 42, ",": 43, "/": 44,
	"n": 45, "m": 46, ".": 47, "`": 50,
	"space": 49, "tab": 48, "return": 36,
}
