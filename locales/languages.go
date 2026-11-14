package locales

// LanguageOption describes an in-app UI language choice. Adding a new language
// requires only appending an entry here and adding locales/<Tag>.json before
// running `go tool mygo generate .`.
type LanguageOption struct {
	Tag     string
	Autonym string
}

// Switch2Languages lists all 18 official Nintendo Switch 2 system languages.
// An empty preference ("") follows the host OS preferred language.
var Switch2Languages = []LanguageOption{
	{Tag: "en", Autonym: "English (US)"},
	{Tag: "en-GB", Autonym: "English (UK)"},
	{Tag: "fr", Autonym: "Français"},
	{Tag: "fr-CA", Autonym: "Français (Canada)"},
	{Tag: "de", Autonym: "Deutsch"},
	{Tag: "es", Autonym: "Español"},
	{Tag: "es-419", Autonym: "Español (Latinoamérica)"},
	{Tag: "it", Autonym: "Italiano"},
	{Tag: "nl", Autonym: "Nederlands"},
	{Tag: "pl", Autonym: "Polski"},
	{Tag: "pt-PT", Autonym: "Português (Portugal)"},
	{Tag: "pt-BR", Autonym: "Português (Brasil)"},
	{Tag: "ru", Autonym: "Русский"},
	{Tag: "ja", Autonym: "日本語"},
	{Tag: "ko", Autonym: "한국어"},
	{Tag: "th", Autonym: "ไทย"},
	{Tag: "zh-CN", Autonym: "简体中文"},
	{Tag: "zh-Hant", Autonym: "繁體中文"},
}

// SupportedLanguageTags returns the canonical BCP 47 tags in Switch2Languages.
func SupportedLanguageTags() []string {
	tags := make([]string, len(Switch2Languages))
	for i, lang := range Switch2Languages {
		tags[i] = lang.Tag
	}
	return tags
}

// NormalizeLanguagePreference maps "" and "system" to "" (follow OS language)
// and validates explicit tags against Switch2Languages (case-insensitively).
func NormalizeLanguagePreference(pref string) (string, bool) {
	if pref == "" || pref == "system" {
		return "", true
	}
	for _, lang := range Switch2Languages {
		if equalFoldASCII(pref, lang.Tag) {
			return lang.Tag, true
		}
	}
	return "", false
}

// LanguageAutonym returns the native display name for tag, or "" when tag is
// empty or unknown.
func LanguageAutonym(tag string) string {
	for _, lang := range Switch2Languages {
		if lang.Tag == tag {
			return lang.Autonym
		}
	}
	return ""
}

func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
