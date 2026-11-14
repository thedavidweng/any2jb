package artemis

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/thedavidweng/any2jb/internal/manifest"
)

// Pinned catalog artifacts inside the savekit plugin.
const (
	catalogFile = "vendor/ns2-savekit/research/catalog.json"
	titlesFile  = "vendor/ns2-savekit/research/current/config/titles.json"
)

// Seedsow Lullaby title IDs, in the lower-case form the pinned names map and the
// filesystem host's target configuration record. The demo and the full-game
// launcher saves share one Artemis profile, so both stay protected from a host
// save overwrite.
const (
	SeedsowDemoTitleID = "010042d022aea000"
	SeedsowFullTitleID = "0100ee5021c9e000"
)

// InventoryTool is one pinned tool in the SaveKit catalog.
type InventoryTool struct {
	Category     string `json:"category"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	Execution    string `json:"execution"`
	RequiresRun  bool   `json:"requiresRun"`
	Requirements string `json:"requirements"`
}

// ArtemisGame is one launcher title: the pinned name joined with the Artemis
// launcher profile that serves it.
type ArtemisGame struct {
	TitleID string
	Name    string
	Engine  string
	Status  string
	Demo    bool
}

// artemisProfiles maps a pinned title name prefix to its launcher profile. Only
// the Seedsow profile has a validated filesystem bridge; the others are HTTP and
// Lua only.
var artemisProfiles = []struct{ prefix, engine, status string }{
	{"Seedsow Lullaby", "seedsow", "fs-verified"},
	{"Nora to Oujo to Noraneko Heart 2", "nora", "http-only"},
	{"Wagamama High Spec", "hamidashi", "http-only"},
}

// Catalog is the verified Artemis inventory: the pinned tool list plus the
// launcher games those titles describe.
type Catalog struct {
	Revision       string
	LegacyRevision string
	Tools          []InventoryTool
	Games          []ArtemisGame
}

// LoadCatalog verifies the savekit plugin and returns only content whose digest
// the manifest records.
func LoadCatalog(toolRoot string, m *manifest.Manifest) (*Catalog, error) {
	root, err := PluginRoot(toolRoot, m, SaveKitPlugin)
	if err != nil {
		return nil, err
	}
	inventory, err := readRecorded(root, m, SaveKitPlugin, catalogFile)
	if err != nil {
		return nil, err
	}
	var document struct {
		Revision       string          `json:"revision"`
		LegacyRevision string          `json:"legacyRevision"`
		Tools          []InventoryTool `json:"tools"`
	}
	if err := json.Unmarshal(inventory, &document); err != nil {
		return nil, fmt.Errorf("%w: the pinned savekit catalog is not valid JSON: %v", ErrIntegrity, err)
	}
	if document.Revision == "" || len(document.Tools) == 0 {
		return nil, fmt.Errorf("%w: the pinned savekit catalog is incomplete", ErrIntegrity)
	}
	titles, err := readRecorded(root, m, SaveKitPlugin, titlesFile)
	if err != nil {
		return nil, err
	}
	var names struct {
		Names map[string]string `json:"names"`
	}
	if err := json.Unmarshal(titles, &names); err != nil {
		return nil, fmt.Errorf("%w: the pinned title names are not valid JSON: %v", ErrIntegrity, err)
	}
	if len(names.Names) == 0 {
		return nil, fmt.Errorf("%w: the pinned title names are empty", ErrIntegrity)
	}
	// Both Seedsow launcher saves must be named: they are the program IDs the
	// filesystem host may never overwrite.
	for _, id := range []string{SeedsowDemoTitleID, SeedsowFullTitleID} {
		if names.Names[id] == "" {
			return nil, fmt.Errorf("%w: the pinned title names lost %s", ErrIntegrity, id)
		}
	}
	catalog := &Catalog{Revision: document.Revision, LegacyRevision: document.LegacyRevision, Tools: document.Tools}
	ids := make([]string, 0, len(names.Names))
	for id := range names.Names {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		name := names.Names[id]
		for _, profile := range artemisProfiles {
			if !strings.HasPrefix(name, profile.prefix) {
				continue
			}
			catalog.Games = append(catalog.Games, ArtemisGame{
				TitleID: id, Name: name, Engine: profile.engine, Status: profile.status,
				Demo: strings.Contains(name, "(demo)") || strings.Contains(name, "(trial)"),
			})
			break
		}
	}
	if len(catalog.Games) == 0 {
		return nil, fmt.Errorf("%w: the pinned title names list no Artemis launcher title", ErrIntegrity)
	}
	return catalog, nil
}
