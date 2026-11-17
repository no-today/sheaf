package theme

import (
	"embed"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

//go:embed assets/GeistMono-Regular.ttf assets/GeistMono-SemiBold.ttf assets/GeistMono-Italic.ttf
var monoFaces embed.FS

// UIFont is the desktop's interface font: labels, buttons, headings and prose read as the system does, so the code
// stands apart from them.
const UIFont = "system-ui"

// BundledCode is the monospace bundled with the app, the same on every machine.
const BundledCode = "Geist Mono"

// MonoFont is for names the eye compares letter by letter in the interface: branches, requirement numbers, hashes,
// paths. Code itself uses CodeFont, which the user may change.
const MonoFont = `"` + BundledCode + `", monospace`

// systemMono are the monospaces macOS keeps out of its font list, loaded from their files when asked for by name.
var systemMono = map[string][]string{
	"SF Mono": {"/System/Library/Fonts/SFNSMono.ttf", "/System/Library/Fonts/SFNSMonoItalic.ttf"},
}

// code is the family the user chose for code, "" for the bundled one. It is app-wide like the app's languages: every
// window draws code with it.
var code atomic.Pointer[string]

// loaded caches which system monospaces have been registered.
var loaded sync.Map

func init() {
	for _, face := range []string{"Regular", "SemiBold", "Italic"} {
		data, err := monoFaces.ReadFile("assets/GeistMono-" + face + ".ttf")
		if err == nil {
			err = ui.RegisterFont(data, BundledCode)
		}
		if err != nil {
			panic("theme: bundled font " + face + ": " + err.Error())
		}
	}
}

// CodeFonts are the families the settings suggest for code; any installed family can be typed instead.
var CodeFonts = []string{BundledCode, "SF Mono", "Menlo", "JetBrains Mono", "Fira Code", "Cascadia Code"}

// SetCodeFont makes family the font of code in every window; "" or a family the system lacks falls back to the bundled
// one, glyph by glyph.
func SetCodeFont(family string) {
	family = strings.TrimSpace(family)
	if files, ok := systemMono[family]; ok {
		if _, done := loaded.LoadOrStore(family, true); !done {
			for _, f := range files {
				if data, err := os.ReadFile(f); err == nil {
					_ = ui.RegisterFont(data, family) // missing or unreadable: the fallback draws it
				}
			}
		}
	}
	code.Store(&family)
}

// CodeFont is the font family list of code: the user's choice, then the bundled monospace.
func CodeFont() string {
	if f := code.Load(); f != nil && *f != "" && *f != BundledCode {
		return `"` + *f + `", ` + MonoFont
	}
	return MonoFont
}
