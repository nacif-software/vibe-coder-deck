package deck

import (
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Icon is drawn on a key's cap in the page and the pad view: like the real
// keycaps, which take any print. A key's Icon is one of:
//   - "" for none;
//   - "icon:<name>" for a built-in drawing (Icons);
//   - a short text or emoji, up to 4 characters ("🚀", "OK");
//   - an uploaded picture as a data URL (PNG, JPEG, GIF, WebP or SVG, base64,
//     up to 256 KB), shown as an image so an SVG's scripts never run.
type IconInfo struct {
	Name string `json:"name"`
	// SVG is the inside of a 24×24 stroke drawing (stroke="currentColor").
	SVG string `json:"svg"`
}

var icons = []IconInfo{
	{"skull", `<path d="M12 3a8 8 0 0 0-8 8c0 2.6 1.2 4.4 3 5.5V20h10v-3.5c1.8-1.1 3-2.9 3-5.5a8 8 0 0 0-8-8z"/><circle cx="9" cy="11.5" r="1.6" fill="currentColor"/><circle cx="15" cy="11.5" r="1.6" fill="currentColor"/><path d="M10.5 17v3M13.5 17v3"/>`},
	{"play", `<path d="M7 4l13 8-13 8z"/>`},
	{"pause", `<path d="M8 5v14M16 5v14"/>`},
	{"play-pause", `<path d="M4 5l9 7-9 7z"/><path d="M16 5v14M20 5v14"/>`},
	{"next", `<path d="M5 5l10 7-10 7z"/><path d="M19 5v14"/>`},
	{"prev", `<path d="M19 5L9 12l10 7z"/><path d="M5 5v14"/>`},
	{"volume", `<path d="M4 9h4l5-4v14l-5-4H4z"/><path d="M17 9a4 4 0 0 1 0 6M19.5 6.5a8 8 0 0 1 0 11"/>`},
	{"mute", `<path d="M4 9h4l5-4v14l-5-4H4z"/><path d="M17 9l5 6M22 9l-5 6"/>`},
	{"mic", `<rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3"/>`},
	{"camera", `<path d="M4 7h4l2-3h4l2 3h4v12H4z"/><circle cx="12" cy="13" r="3.5"/>`},
	{"screenshot", `<path d="M4 8V4h4M16 4h4v4M20 16v4h-4M8 20H4v-4"/><rect x="8" y="8" width="8" height="8" rx="1"/>`},
	{"terminal", `<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 9l3 3-3 3M13 15h4"/>`},
	{"code", `<path d="M8 7l-5 5 5 5M16 7l5 5-5 5M14 4l-4 16"/>`},
	{"git", `<circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="8" r="2.5"/><path d="M6 8.5v7M18 10.5c0 4-6 3-11 6"/>`},
	{"bug", `<rect x="7" y="8" width="10" height="12" rx="5"/><path d="M12 8v12M9 5l1.5 3M15 5l-1.5 3M3 12h4M17 12h4M4 18l3-2M20 18l-3-2M4 6l3 2M20 6l-3 2"/>`},
	{"rocket", `<path d="M12 3c3 2 5 6 5 10l-2 3H9l-2-3c0-4 2-8 5-10z"/><circle cx="12" cy="10" r="1.8"/><path d="M9 16l-2 4 3-1M15 16l2 4-3-1"/>`},
	{"bot", `<rect x="5" y="8" width="14" height="11" rx="3"/><path d="M12 4v4M9 13h.01M15 13h.01M9 16h6M2 12v3M22 12v3"/>`},
	{"sparkles", `<path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/><path d="M19 15l.8 2.2L22 18l-2.2.8L19 21l-.8-2.2L16 18l2.2-.8z"/>`},
	{"bolt", `<path d="M13 2L4 14h7l-1 8 9-12h-7z"/>`},
	{"fire", `<path d="M12 22c4 0 7-3 7-7 0-4-3-6-4-10-2 2-3 4-3 6-1-1-2-2-2-4-2 2-5 5-5 8 0 4 3 7 7 7z"/>`},
	{"star", `<path d="M12 3l2.8 5.8 6.2.9-4.5 4.4 1 6.2L12 17.4l-5.5 2.9 1-6.2L3 9.7l6.2-.9z"/>`},
	{"heart", `<path d="M12 20s-8-5-8-11a4.5 4.5 0 0 1 8-2.8A4.5 4.5 0 0 1 20 9c0 6-8 11-8 11z"/>`},
	{"check", `<path d="M4 12l5 5L20 6"/>`},
	{"x", `<path d="M6 6l12 12M18 6L6 18"/>`},
	{"coffee", `<path d="M4 9h12v6a5 5 0 0 1-5 5H9a5 5 0 0 1-5-5z"/><path d="M16 11h2a2 2 0 0 1 0 4h-2M8 3v3M12 3v3"/>`},
	{"music", `<path d="M9 18V5l11-2v13"/><circle cx="6.5" cy="18" r="2.5"/><circle cx="17.5" cy="16" r="2.5"/>`},
	{"chat", `<path d="M4 5h16v11H9l-5 4z"/>`},
	{"mail", `<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M3 7l9 6 9-6"/>`},
	{"globe", `<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18"/>`},
	{"search", `<circle cx="11" cy="11" r="6"/><path d="M20 20l-4.5-4.5"/>`},
	{"lock", `<rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>`},
	{"settings", `<circle cx="12" cy="12" r="3"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9l2.1 2.1M17 17l2.1 2.1M4.9 19.1L7 17M17 7l2.1-2.1"/>`},
	{"folder", `<path d="M3 6h6l2 2h10v11H3z"/>`},
	{"home", `<path d="M3 11l9-7 9 7v9h-6v-6H9v6H3z"/>`},
	{"undo", `<path d="M9 7L4 12l5 5"/><path d="M4 12h11a5 5 0 0 1 0 10h-2"/>`},
	{"copy", `<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3"/>`},
	{"send", `<path d="M3 11l18-8-8 18-2-8z"/>`},
	{"moon", `<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>`},
}

var iconNames = map[string]bool{}

func init() {
	for _, ic := range icons {
		iconNames[ic.Name] = true
	}
}

// Icons lists the built-in drawings, in the order the picker shows them.
func Icons() []IconInfo {
	out := make([]IconInfo, len(icons))
	copy(out, icons)
	return out
}

const maxIconBytes = 256 << 10

var iconImageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp", "image/svg+xml"}

func validateIcon(icon string) error {
	switch {
	case icon == "":
		return nil
	case strings.HasPrefix(icon, "icon:"):
		if !iconNames[strings.TrimPrefix(icon, "icon:")] {
			return fmt.Errorf("icon: %q is not a built-in icon", icon)
		}
		return nil
	case strings.HasPrefix(icon, "data:"):
		meta, payload, ok := strings.Cut(strings.TrimPrefix(icon, "data:"), ",")
		mime, enc, _ := strings.Cut(meta, ";")
		if !ok || enc != "base64" || !contains(iconImageTypes, mime) {
			return fmt.Errorf("icon: an uploaded picture must be a base64 PNG, JPEG, GIF, WebP or SVG")
		}
		if base64.StdEncoding.DecodedLen(len(payload)) > maxIconBytes {
			return fmt.Errorf("icon: the picture is over %d KB", maxIconBytes>>10)
		}
		if _, err := base64.StdEncoding.DecodeString(payload); err != nil {
			return fmt.Errorf("icon: the picture is not valid base64")
		}
		return nil
	default:
		if utf8.RuneCountInString(icon) > 4 || strings.ContainsAny(icon, "<>&\"'") {
			return fmt.Errorf("icon: text icons are up to 4 characters (an emoji or a short word)")
		}
		return nil
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
