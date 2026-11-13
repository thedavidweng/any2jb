package locales

import (
	"errors"
	"maps"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/i18n"
)

type segment struct {
	key  string
	raw  string
	args map[string]any
}

// Text is an immutable catalog-backed message. English() and Error() render
// deterministic source-locale text for snapshots, logs, and diagnostic
// bundles; Local() renders in the active UI or CLI language without substring
// scanning.
type Text struct {
	segments []segment
}

func Key(key string) Text {
	if key == "" {
		return Text{}
	}
	return Text{segments: []segment{{key: key}}}
}

func Format(key string, args map[string]any) Text {
	if key == "" {
		return Text{}
	}
	return Text{segments: []segment{{key: key, args: maps.Clone(args)}}}
}

func Raw(text string) Text {
	if text == "" {
		return Text{}
	}
	return Text{segments: []segment{{raw: text}}}
}

func Join(parts ...Text) Text {
	var merged []segment
	for _, part := range parts {
		merged = append(merged, part.segments...)
	}
	if len(merged) == 0 {
		return Text{}
	}
	return Text{segments: merged}
}

func (t Text) IsZero() bool { return len(t.segments) == 0 }

func (t Text) InLocale(locale string) string {
	var b strings.Builder
	for _, seg := range t.segments {
		rendered := seg.raw
		if seg.key != "" {
			var resolved map[string]any
			if len(seg.args) > 0 {
				resolved = make(map[string]any, len(seg.args))
				for k, v := range seg.args {
					switch val := v.(type) {
					case interface{ InLocale(string) string }:
						resolved[k] = val.InLocale(locale)
					case error:
						resolved[k] = ErrorInLocale(val, locale)
					default:
						resolved[k] = v
					}
				}
			}
			rendered = Catalog.Format(locale, seg.key, resolved)
		}
		if rendered == "" {
			continue
		}
		if b.Len() > 0 && !endsWithFullwidthPunct(b.String()) {
			b.WriteByte(' ')
		}
		b.WriteString(rendered)
	}
	return b.String()
}

func endsWithFullwidthPunct(s string) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	switch r {
	case '。', '！', '？', '；':
		return true
	default:
		return false
	}
}

func (t Text) English() string { return t.InLocale("en") }
func (t Text) Local() string   { return t.InLocale(i18n.Locale()) }
func (t Text) String() string  { return t.English() }
func (t Text) Error() string   { return t.English() }

type wrappedError struct {
	key   string
	inner error
}

// WrapError wraps inner under a catalog template with placeholder {detail}.
func WrapError(key string, inner error) error {
	if inner == nil {
		return nil
	}
	return wrappedError{key: key, inner: inner}
}

func (w wrappedError) Error() string { return w.InLocale("en") }
func (w wrappedError) Local() string { return w.InLocale(i18n.Locale()) }
func (w wrappedError) Unwrap() error { return w.inner }

func (w wrappedError) InLocale(locale string) string {
	return Catalog.Format(locale, w.key, map[string]any{"detail": ErrorInLocale(w.inner, locale)})
}

func ErrorInLocale(err error, locale string) string {
	if err == nil {
		return ""
	}
	var localized interface{ InLocale(string) string }
	if errors.As(err, &localized) {
		return localized.InLocale(locale)
	}
	var simple interface{ Local() string }
	if locale != "en" && errors.As(err, &simple) {
		return simple.Local()
	}
	return err.Error()
}

// LocalizeError renders a catalog-backed error in the active UI or CLI
// language while leaving plain Go errors untouched.
func LocalizeError(err error) string {
	return ErrorInLocale(err, i18n.Locale())
}

// DistributionKeys maps user-facing manifest profile and component IDs onto
// their canonical catalog keys.
var DistributionKeys = map[string]string{
	"pepsipu":                   "dynDistPepsipu",
	"kurumuz":                   "dynDistKurumuz",
	"cellenseres":               "dynDistCellenseres",
	"BermTheSequel":             "dynDistBerm",
	"izmysh":                    "dynDistIzmyshLegacy",
	"astral":                    "dynDistAstral",
	"browser-research":          "dynDistBrowserResearch",
	"ouncecake":                 "dynDistBundledOunceCake",
	"sd-browser":                "dynDistBundledSDBrowser",
	"savekit":                   "dynDistBundledSaveKit",
	"modloader":                 "dynDistBundledModloader",
	"launcher-seedsow-demo":     "dynDistBundledLauncherSave",
	"launcher-seedsow-full":     "dynDistBundledLauncherSave",
	"launcher-nora2-demo":       "dynDistBundledLauncherSave",
	"launcher-nora1-demo":       "dynDistBundledLauncherSave",
	"launcher-hamidashi-demo":   "dynDistBundledLauncherSave",
	"launcher-lua-editor":       "dynDistBundledLauncherSave",
	"launcher-lua-editor-pro":   "dynDistBundledLauncherSave",
	"launcher-balatro":          "dynDistBundledLauncherSave",
	"launcher-balatro-showcase": "dynDistBundledLauncherSave",
	"launcher-gravity-circuit":  "dynDistBundledLauncherSave",
	"mintbrew":                  "dynDistOptMintbrew",
	"mintbrew-native":           "dynDistOptMintbrewNative",
	"izlauncher":                "dynDistOptIZLauncher",
	"izlauncher-blitbun":        "dynDistOptIZBlitbun",
	"izlauncher-shell":          "dynDistOptIZShell",
	"izlauncher-vacootube":      "dynDistOptIZVacootube",
	"homebrew-runtime":          "dynDistOptHomebrewRuntime",
	"civswitch-payload":         "dynDistBundledCivPayload",
	"usbgadget-binary":          "dynDistUsbgadgetBinary",
	"usbgadget-info":            "dynDistUsbgadgetInfo",
	"usbgadget-checksums":       "dynDistUsbgadgetChecksums",
	"driver-installer":          "dynDistDriverInstaller",
	"placeholder-icon":          "dynDistMITTemplate",
	"mygo-notice":               "dynDistMygoNotice",
	"node-darwin-arm64":         "dynDistNode",
	"node-linux-amd64":          "dynDistNode",
	"node-linux-arm64":          "dynDistNode",
	"node-notice":               "dynDistNode",
	"engine-bridge":             "dynDistOriginal",
	"tool-host":                 "dynDistOriginalTools",
}

// DistributionReason resolves a recorded manifest reason to its catalog entry
// when reason matches the canonical source text; test-fixture overrides pass
// through verbatim.
func DistributionReason(id, reason string) Text {
	if key, ok := DistributionKeys[id]; ok && Catalog.Format("en", key, nil) == reason {
		return Key(key)
	}
	return Raw(reason)
}
