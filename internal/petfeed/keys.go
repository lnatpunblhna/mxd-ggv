package petfeed

import (
	"fmt"
	"strings"
	"unicode"
)

// virtualKey 是 Windows 虚拟键码。
type virtualKey uint16

const (
	vkBack      virtualKey = 0x08
	vkTab       virtualKey = 0x09
	vkReturn    virtualKey = 0x0D
	vkEscape    virtualKey = 0x1B
	vkSpace     virtualKey = 0x20
	vkPrior     virtualKey = 0x21
	vkNext      virtualKey = 0x22
	vkEnd       virtualKey = 0x23
	vkHome      virtualKey = 0x24
	vkLeft      virtualKey = 0x25
	vkUp        virtualKey = 0x26
	vkRight     virtualKey = 0x27
	vkDown      virtualKey = 0x28
	vkInsert    virtualKey = 0x2D
	vkDelete    virtualKey = 0x2E
	vkMultiply  virtualKey = 0x6A
	vkAdd       virtualKey = 0x6B
	vkSubtract  virtualKey = 0x6D
	vkDecimal   virtualKey = 0x6E
	vkDivide    virtualKey = 0x6F
	vkOEM1      virtualKey = 0xBA
	vkOEMPlus   virtualKey = 0xBB
	vkOEMComma  virtualKey = 0xBC
	vkOEMMinus  virtualKey = 0xBD
	vkOEMPeriod virtualKey = 0xBE
	vkOEM2      virtualKey = 0xBF
	vkOEM3      virtualKey = 0xC0
	vkOEM4      virtualKey = 0xDB
	vkOEM5      virtualKey = 0xDC
	vkOEM6      virtualKey = 0xDD
	vkOEM7      virtualKey = 0xDE
)

var namedKeys = map[string]virtualKey{
	"insert": vkInsert, "ins": vkInsert,
	"delete": vkDelete, "del": vkDelete,
	"home":   vkHome,
	"end":    vkEnd,
	"pageup": vkPrior, "pgup": vkPrior, "prior": vkPrior,
	"pagedown": vkNext, "pgdn": vkNext, "pgdown": vkNext, "next": vkNext,
	"space": vkSpace, "spacebar": vkSpace,
	"tab":   vkTab,
	"enter": vkReturn, "return": vkReturn,
	"escape": vkEscape, "esc": vkEscape,
	"backspace": vkBack, "back": vkBack,
	"up": vkUp, "arrowup": vkUp,
	"down": vkDown, "arrowdown": vkDown,
	"left": vkLeft, "arrowleft": vkLeft,
	"right": vkRight, "arrowright": vkRight,
	"minus": vkOEMMinus, "oemminus": vkOEMMinus,
	"equal": vkOEMPlus, "equals": vkOEMPlus, "oemplus": vkOEMPlus,
	"comma": vkOEMComma, "oemcomma": vkOEMComma,
	"period": vkOEMPeriod, "dot": vkOEMPeriod, "oemperiod": vkOEMPeriod,
	"slash": vkOEM2, "oem2": vkOEM2,
	"backslash": vkOEM5, "oem5": vkOEM5,
	"semicolon": vkOEM1, "oem1": vkOEM1,
	"quote": vkOEM7, "oem7": vkOEM7,
	"grave": vkOEM3, "backquote": vkOEM3, "oem3": vkOEM3,
	"bracketleft": vkOEM4, "leftbracket": vkOEM4, "oem4": vkOEM4,
	"bracketright": vkOEM6, "rightbracket": vkOEM6, "oem6": vkOEM6,
	"numpadadd": vkAdd, "add": vkAdd, "numpadplus": vkAdd,
	"numpadsubtract": vkSubtract, "numpadminus": vkSubtract,
	"numpadmultiply": vkMultiply, "numpadstar": vkMultiply,
	"numpaddivide": vkDivide, "numpadslash": vkDivide,
	"numpaddecimal": vkDecimal, "numpaddot": vkDecimal,
	"numpadenter": vkReturn,
}

// ParseKey 把前端捕获的键名解析为虚拟键码。
// 接受 KeyboardEvent.code（Insert、Digit1、KeyA、F9）以及常见别名。
func ParseKey(name string) (virtualKey, error) {
	raw := strings.TrimSpace(name)
	if raw == "" {
		return 0, fmt.Errorf("请填写喂食快捷键")
	}
	key := normalizeKeyName(raw)

	if vk, ok := namedKeys[key]; ok {
		return vk, nil
	}

	if vk, ok := parseLetterOrDigit(key); ok {
		return vk, nil
	}

	if n, ok := parseFunctionKey(key); ok {
		return virtualKey(0x70 + n - 1), nil
	}

	if n, ok := parsePrefixedDigit("numpad", key); ok {
		return virtualKey(0x60 + n), nil
	}

	return 0, fmt.Errorf("无法识别快捷键 %q", name)
}

func normalizeKeyName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func parseLetterOrDigit(key string) (virtualKey, bool) {
	if strings.HasPrefix(key, "digit") && len(key) == 6 {
		d := key[5]
		if d >= '0' && d <= '9' {
			return virtualKey(0x30 + d - '0'), true
		}
	}
	if strings.HasPrefix(key, "key") && len(key) == 4 {
		c := key[3]
		if c >= 'a' && c <= 'z' {
			return virtualKey(0x41 + c - 'a'), true
		}
	}
	if len(key) == 1 {
		c := key[0]
		if c >= '0' && c <= '9' {
			return virtualKey(0x30 + c - '0'), true
		}
		if c >= 'a' && c <= 'z' {
			return virtualKey(0x41 + c - 'a'), true
		}
	}
	return 0, false
}

func parseFunctionKey(key string) (int, bool) {
	if !strings.HasPrefix(key, "f") || len(key) < 2 || len(key) > 3 {
		return 0, false
	}
	n := 0
	for i := 1; i < len(key); i++ {
		c := key[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > 12 {
		return 0, false
	}
	return n, true
}

func isExtendedKey(vk virtualKey) bool {
	switch vk {
	case vkInsert, vkDelete, vkHome, vkEnd, vkPrior, vkNext, vkLeft, vkRight, vkUp, vkDown, vkDivide:
		return true
	default:
		return false
	}
}

func parsePrefixedDigit(prefix, key string) (int, bool) {
	if !strings.HasPrefix(key, prefix) {
		return 0, false
	}
	rest := key[len(prefix):]
	if len(rest) != 1 || rest[0] < '0' || rest[0] > '9' {
		return 0, false
	}
	return int(rest[0] - '0'), true
}
