// Package plugins manages on-demand download, cryptographic verification,
// private staging, and removal of all optional sub-tool plugins.
package plugins

import (
	"runtime"
	"slices"

	"github.com/thedavidweng/any2jb/internal/manifest"
)

// Category groups related sub-tool plugins in the GUI and CLI.
type Category string

const (
	CategorySession   Category = "session"
	CategoryHomebrew  Category = "homebrew"
	CategoryCivSwitch Category = "civswitch"
	CategoryArtemis   Category = "artemis"
	CategoryUtilities Category = "utilities"
)

// Categories lists all plugin categories in display order.
var Categories = []Category{
	CategorySession,
	CategoryHomebrew,
	CategoryCivSwitch,
	CategoryArtemis,
	CategoryUtilities,
}

// Spec describes one installable sub-tool plugin backed by a canonical
// manifest component.
type Spec struct {
	ID           string
	Category     Category
	TitleKey     string
	DescKey      string
	UpstreamName string
	UpstreamURL  string
	License      string
	Dependencies []string
	Platforms    []string // Empty means all supported platforms (darwin-arm64, linux-amd64, linux-arm64).
}

// SupportedOnHost reports whether this plugin can be installed on the current OS/arch.
func (s Spec) SupportedOnHost() bool {
	return s.SupportedOn(runtime.GOOS + "-" + runtime.GOARCH)
}

// SupportedOn reports whether this plugin supports platform (e.g. "darwin-arm64").
func (s Spec) SupportedOn(platform string) bool {
	if len(s.Platforms) == 0 {
		return true
	}
	return slices.Contains(s.Platforms, platform)
}

var catalog = []Spec{
	// Session & Save Tools
	{
		ID:           "ouncecake",
		Category:     CategorySession,
		TitleKey:     "pluginOunceCakeTitle",
		DescKey:      "pluginOunceCakeDesc",
		UpstreamName: "Cellenseres/OunceCake",
		UpstreamURL:  "https://github.com/Cellenseres/OunceCake",
		License:      "GPL-3.0-only",
	},
	{
		ID:           "savekit",
		Category:     CategorySession,
		TitleKey:     "pluginSaveKitTitle",
		DescKey:      "pluginSaveKitDesc",
		UpstreamName: "dougchansan/ns2-savekit",
		UpstreamURL:  "https://github.com/dougchansan/ns2-savekit",
		License:      "MIT",
	},
	{
		ID:           "modloader",
		Category:     CategorySession,
		TitleKey:     "pluginModloaderTitle",
		DescKey:      "pluginModloaderDesc",
		UpstreamName: "SebaaMG/ns2jb-modloader",
		UpstreamURL:  "https://github.com/SebaaMG/ns2jb-modloader",
		License:      "MIT",
		Dependencies: []string{"ouncecake"},
	},

	// Homebrew Launchers
	{
		ID:           "mintbrew-native",
		Category:     CategoryHomebrew,
		TitleKey:     "pluginMintBrewNativeTitle",
		DescKey:      "pluginMintBrewNativeDesc",
		UpstreamName: "ravensclawx/HBLauncher",
		UpstreamURL:  "https://github.com/ravensclawx/HBLauncher",
		License:      "MIT",
		Dependencies: []string{"mintbrew", "ouncecake"},
	},
	{
		ID:           "mintbrew",
		Category:     CategoryHomebrew,
		TitleKey:     "pluginMintBrewTitle",
		DescKey:      "pluginMintBrewDesc",
		UpstreamName: "NiftyNova/GPUBrewLauncher",
		UpstreamURL:  "https://github.com/NiftyNova/GPUBrewLauncher",
		License:      "MIT",
		Dependencies: []string{"homebrew-runtime"},
	},
	{
		ID:           "homebrew-runtime",
		Category:     CategoryHomebrew,
		TitleKey:     "pluginHomebrewRuntimeTitle",
		DescKey:      "pluginHomebrewRuntimeDesc",
		UpstreamName: "python-build-standalone + SPIRV-Tools + glslang",
		UpstreamURL:  "https://github.com/astral-sh/python-build-standalone",
		License:      "PSF-2.0 / HPND / Apache-2.0 / BSD",
		Platforms:    []string{"darwin-arm64"},
	},

	// CivSwitch Tools & Apps
	{
		ID:           "civswitch-usb",
		Category:     CategoryCivSwitch,
		TitleKey:     "pluginCivSwitchUSBTitle",
		DescKey:      "pluginCivSwitchUSBDesc",
		UpstreamName: "Gezine/CivSwitch",
		UpstreamURL:  "https://github.com/Gezine/CivSwitch",
		License:      "MIT",
	},
	{
		ID:           "civswitch-trigger",
		Category:     CategoryCivSwitch,
		TitleKey:     "pluginCivSwitchTriggerTitle",
		DescKey:      "pluginCivSwitchTriggerDesc",
		UpstreamName: "Gezine/CivSwitch",
		UpstreamURL:  "https://github.com/Gezine/CivSwitch",
		License:      "MIT",
		Dependencies: []string{"civswitch-usb"},
	},
	{
		ID:           "civswitch-fs",
		Category:     CategoryCivSwitch,
		TitleKey:     "pluginCivSwitchFSTitle",
		DescKey:      "pluginCivSwitchFSDesc",
		UpstreamName: "mza1010/civswitch-kit",
		UpstreamURL:  "https://gitee.com/mza1010/civswitch-kit",
		License:      "GPL-2.0",
	},
	{
		ID:           "civhomebrew",
		Category:     CategoryCivSwitch,
		TitleKey:     "pluginCivHomebrewTitle",
		DescKey:      "pluginCivHomebrewDesc",
		UpstreamName: "XITRIX/CivHomebrew",
		UpstreamURL:  "https://github.com/XITRIX/CivHomebrew",
		License:      "MIT",
	},
	{
		ID:           "moonlight-civswitch",
		Category:     CategoryCivSwitch,
		TitleKey:     "pluginMoonlightCivSwitchTitle",
		DescKey:      "pluginMoonlightCivSwitchDesc",
		UpstreamName: "XITRIX/Moonlight-CivSwitch",
		UpstreamURL:  "https://github.com/XITRIX/Moonlight-CivSwitch",
		License:      "GPL-3.0",
		Dependencies: []string{"civhomebrew"},
	},
	{
		ID:           "sw2nes",
		Category:     CategoryCivSwitch,
		TitleKey:     "pluginSW2NESTitle",
		DescKey:      "pluginSW2NESDesc",
		UpstreamName: "Rodpad/SW2NES",
		UpstreamURL:  "https://github.com/Rodpad/SW2NES",
		License:      "GPL-2.0-or-later",
		Dependencies: []string{"civhomebrew"},
	},
	{
		ID:           "jellyfin-civswitch",
		Category:     CategoryCivSwitch,
		TitleKey:     "pluginJellyfinCivSwitchTitle",
		DescKey:      "pluginJellyfinCivSwitchDesc",
		UpstreamName: "THZoria/jellyfin",
		UpstreamURL:  "https://github.com/THZoria/jellyfin",
		License:      "MIT",
		Dependencies: []string{"civhomebrew"},
	},

	// Artemis / Seedsow & Save Launchers
	{
		ID:           "artemis-fs",
		Category:     CategoryArtemis,
		TitleKey:     "pluginArtemisFSTitle",
		DescKey:      "pluginArtemisFSDesc",
		UpstreamName: "cpypwn/mac2jb (artemis-fs)",
		UpstreamURL:  "https://github.com/cpypwn/mac2jb",
		License:      "MIT",
	},
	{
		ID:           "launcher-seedsow-demo",
		Category:     CategoryArtemis,
		TitleKey:     "pluginLauncherSeedsowDemoTitle",
		DescKey:      "pluginLauncherSeedsowDemoDesc",
		UpstreamName: "alula/switch-artemis-launcher",
		UpstreamURL:  "https://github.com/alula/switch-artemis-launcher",
		License:      "MIT",
		Dependencies: []string{"savekit"},
	},
	{
		ID:           "launcher-seedsow-full",
		Category:     CategoryArtemis,
		TitleKey:     "pluginLauncherSeedsowFullTitle",
		DescKey:      "pluginLauncherSeedsowFullDesc",
		UpstreamName: "soniciso1/switch-artemis-launcher",
		UpstreamURL:  "https://github.com/soniciso1/switch-artemis-launcher",
		License:      "MIT",
		Dependencies: []string{"savekit"},
	},
	{
		ID:           "launcher-nora2-demo",
		Category:     CategoryArtemis,
		TitleKey:     "pluginLauncherNora2DemoTitle",
		DescKey:      "pluginLauncherNora2DemoDesc",
		UpstreamName: "alula/switch-artemis-launcher",
		UpstreamURL:  "https://github.com/alula/switch-artemis-launcher",
		License:      "MIT",
		Dependencies: []string{"savekit"},
	},
	{
		ID:           "launcher-nora1-demo",
		Category:     CategoryArtemis,
		TitleKey:     "pluginLauncherNora1DemoTitle",
		DescKey:      "pluginLauncherNora1DemoDesc",
		UpstreamName: "alula/switch-artemis-launcher",
		UpstreamURL:  "https://github.com/alula/switch-artemis-launcher",
		License:      "MIT",
		Dependencies: []string{"savekit"},
	},
	{
		ID:           "launcher-hamidashi-demo",
		Category:     CategoryArtemis,
		TitleKey:     "pluginLauncherHamidashiDemoTitle",
		DescKey:      "pluginLauncherHamidashiDemoDesc",
		UpstreamName: "alula/switch-artemis-launcher",
		UpstreamURL:  "https://github.com/alula/switch-artemis-launcher",
		License:      "MIT",
		Dependencies: []string{"savekit"},
	},
	{
		ID:           "launcher-lua-editor",
		Category:     CategoryArtemis,
		TitleKey:     "pluginLauncherLuaEditorTitle",
		DescKey:      "pluginLauncherLuaEditorDesc",
		UpstreamName: "DialingSpoon527/seedsow-lua-editor",
		UpstreamURL:  "https://github.com/DialingSpoon527/seedsow-lua-editor",
		License:      "MIT",
		Dependencies: []string{"savekit"},
	},
	{
		ID:           "launcher-lua-editor-pro",
		Category:     CategoryArtemis,
		TitleKey:     "pluginLauncherLuaEditorProTitle",
		DescKey:      "pluginLauncherLuaEditorProDesc",
		UpstreamName: "purple442/seedsow-lua-editor-pro",
		UpstreamURL:  "https://github.com/purple442/seedsow-lua-editor-pro",
		License:      "MIT",
		Dependencies: []string{"savekit"},
	},
	{
		ID:           "launcher-balatro",
		Category:     CategoryArtemis,
		TitleKey:     "pluginLauncherBalatroTitle",
		DescKey:      "pluginLauncherBalatroDesc",
		UpstreamName: "Luminger/balatro-switch-rce",
		UpstreamURL:  "https://github.com/Luminger/balatro-switch-rce",
		License:      "MIT",
		Dependencies: []string{"savekit"},
	},
	{
		ID:           "launcher-balatro-showcase",
		Category:     CategoryArtemis,
		TitleKey:     "pluginLauncherBalatroShowcaseTitle",
		DescKey:      "pluginLauncherBalatroShowcaseDesc",
		UpstreamName: "Luminger/balatro-switch-rce",
		UpstreamURL:  "https://github.com/Luminger/balatro-switch-rce",
		License:      "MIT",
		Dependencies: []string{"savekit"},
	},
	{
		ID:           "launcher-gravity-circuit",
		Category:     CategoryArtemis,
		TitleKey:     "pluginLauncherGravityCircuitTitle",
		DescKey:      "pluginLauncherGravityCircuitDesc",
		UpstreamName: "figurophobia/GravCirc-Switch-rce",
		UpstreamURL:  "https://github.com/figurophobia/GravCirc-Switch-rce",
		License:      "MIT",
		Dependencies: []string{"savekit"},
	},

	// Utilities & Diagnostics
	{
		ID:           "nxformat",
		Category:     CategoryUtilities,
		TitleKey:     "pluginNXFormatTitle",
		DescKey:      "pluginNXFormatDesc",
		UpstreamName: "fab918/nxformat",
		UpstreamURL:  "https://github.com/fab918/nxformat",
		License:      "MIT",
	},
	{
		ID:           "nsp-box",
		Category:     CategoryUtilities,
		TitleKey:     "pluginNSPBoxTitle",
		DescKey:      "pluginNSPBoxDesc",
		UpstreamName: "smarkyie/NSP-Box",
		UpstreamURL:  "https://github.com/smarkyie/NSP-Box",
		License:      "MIT",
		Dependencies: []string{"homebrew-runtime"},
	},
	{
		ID:           "nup",
		Category:     CategoryUtilities,
		TitleKey:     "pluginNUPTitle",
		DescKey:      "pluginNUPDesc",
		UpstreamName: "a55uka/nup",
		UpstreamURL:  "https://github.com/a55uka/nup",
		License:      "Unlicense",
		Dependencies: []string{"homebrew-runtime"},
	},
	{
		ID:           "dnswitch",
		Category:     CategoryUtilities,
		TitleKey:     "pluginDNSwitchTitle",
		DescKey:      "pluginDNSwitchDesc",
		UpstreamName: "cpypwn/mac2jb (DNSwitch)",
		UpstreamURL:  "https://github.com/cpypwn/mac2jb",
		License:      "MIT",
	},
	{
		ID:           "browser-probes",
		Category:     CategoryUtilities,
		TitleKey:     "pluginBrowserProbesTitle",
		DescKey:      "pluginBrowserProbesDesc",
		UpstreamName: "cpypwn/mac2jb (browser-probes)",
		UpstreamURL:  "https://github.com/cpypwn/mac2jb",
		License:      "MIT",
	},
}

// All returns a copy of the ordered plugin catalog.
func All() []Spec {
	out := make([]Spec, len(catalog))
	copy(out, catalog)
	return out
}

// Lookup finds a plugin spec by ID.
func Lookup(id string) (Spec, bool) {
	for _, s := range catalog {
		if s.ID == id {
			return s, true
		}
	}
	return Spec{}, false
}

// ByCategory returns all plugins belonging to cat.
func ByCategory(cat Category) []Spec {
	var out []Spec
	for _, s := range catalog {
		if s.Category == cat {
			out = append(out, s)
		}
	}
	return out
}

// ExpandTargets expands user-facing selectors ("all", a category name, or
// individual plugin IDs) into a deduplicated dependency-ordered list of
// host-supported component IDs.
func ExpandTargets(platform string, selectors ...string) ([]string, error) {
	var requested []string
	for _, sel := range selectors {
		switch sel {
		case "all":
			for _, s := range catalog {
				if s.SupportedOn(platform) {
					requested = append(requested, s.ID)
				}
			}
		case string(CategorySession), string(CategoryHomebrew), string(CategoryCivSwitch), string(CategoryArtemis), string(CategoryUtilities):
			for _, s := range ByCategory(Category(sel)) {
				if s.SupportedOn(platform) {
					requested = append(requested, s.ID)
				}
			}
		default:
			spec, ok := Lookup(sel)
			if !ok {
				return nil, &UnknownPluginError{ID: sel}
			}
			requested = append(requested, spec.ID)
		}
	}
	var ordered []string
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(id string, direct bool) error
	visit = func(id string, direct bool) error {
		if visited[id] {
			return nil
		}
		spec, ok := Lookup(id)
		if !ok {
			return &UnknownPluginError{ID: id}
		}
		if !spec.SupportedOn(platform) {
			if direct {
				return &UnsupportedPlatformError{ID: id, Platform: platform}
			}
			// Optional platform-specific dependency (such as homebrew-runtime on Linux) is skipped.
			return nil
		}
		if visiting[id] {
			return nil
		}
		visiting[id] = true
		for _, dep := range spec.Dependencies {
			if err := visit(dep, false); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		ordered = append(ordered, id)
		return nil
	}
	for _, id := range requested {
		if err := visit(id, true); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

// UnknownPluginError reports an unrecognized plugin selector.
type UnknownPluginError struct{ ID string }

func (e *UnknownPluginError) Error() string { return "unknown plugin " + e.ID }

// UnsupportedPlatformError reports a plugin unavailable on the active OS/arch.
type UnsupportedPlatformError struct{ ID, Platform string }

func (e *UnsupportedPlatformError) Error() string {
	return "plugin " + e.ID + " is not supported on " + e.Platform
}

// VerifyCatalogMatchesManifest checks that every optional manifest tool is in
// the plugin catalog and vice versa.
func VerifyCatalogMatchesManifest(m *manifest.Manifest) bool {
	if m == nil {
		return false
	}
	want := m.OptionalToolIDs()
	if len(want) != len(catalog) {
		return false
	}
	for _, s := range catalog {
		if !slices.Contains(want, s.ID) {
			return false
		}
	}
	return true
}
