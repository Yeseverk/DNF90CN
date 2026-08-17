package dnfbridge

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"

	dnfmonster "longheng.io/server/internal/modules/dnf/monster"
	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

var (
	errDungeonMonsterSourceRequired = errors.New("dnf dungeon monster PVF source is required")
	errDungeonMonsterListEmpty      = errors.New("dnf dungeon monster list is empty")
)

type pvfDungeonMonsterCatalog struct {
	mu                sync.Mutex
	source            dnfpvf.Source
	paths             map[int64]string
	cache             map[int64]dnfmonster.Monster
	dropCatalog       *pvfDungeonDropCatalog
	dropCatalogErr    error
	dropCatalogLoaded bool
}

func newPVFDungeonMonsterCatalog(source dnfpvf.Source) (*pvfDungeonMonsterCatalog, error) {
	if source == nil {
		return nil, errDungeonMonsterSourceRequired
	}
	text, err := source.ReadText(dnfmonster.DefaultList)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dnfmonster.DefaultList, err)
	}
	document, err := dnfpvf.Parse(dnfmonster.DefaultList, text)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", dnfmonster.DefaultList, err)
	}
	entries := dnfpvf.ParseList(document)
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: %s", errDungeonMonsterListEmpty, dnfmonster.DefaultList)
	}
	catalog := &pvfDungeonMonsterCatalog{
		source: source,
		paths:  make(map[int64]string, len(entries)),
		cache:  make(map[int64]dnfmonster.Monster),
	}
	for _, entry := range entries {
		if _, exists := catalog.paths[entry.ID]; !exists {
			catalog.paths[entry.ID] = cleanDungeonMonsterPath(entry.Path)
		}
	}
	return catalog, nil
}

func (c *pvfDungeonMonsterCatalog) Count() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.paths)
}

func (c *pvfDungeonMonsterCatalog) Find(id int64) (dnfmonster.Monster, bool, error) {
	if c == nil || c.source == nil {
		return dnfmonster.Monster{}, false, errDungeonMonsterCatalogUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.cache[id]; ok {
		return cloneDungeonMonsterDefinition(cached), true, nil
	}
	listedPath, ok := c.paths[id]
	if !ok {
		return dnfmonster.Monster{}, false, nil
	}
	docPath, text, err := readDungeonMonsterText(c.source, listedPath)
	if err != nil {
		return dnfmonster.Monster{}, false, fmt.Errorf("read monster id=%d path=%s: %w", id, listedPath, err)
	}
	document, err := worldmap.ParseDocument(docPath, text)
	if err != nil {
		return dnfmonster.Monster{}, false, fmt.Errorf("parse monster id=%d path=%s: %w", id, docPath, err)
	}
	definition := parseDungeonMonsterDefinition(id, docPath, document)
	c.cache[id] = definition
	return cloneDungeonMonsterDefinition(definition), true, nil
}

func readDungeonMonsterText(source dnfpvf.Source, listedPath string) (string, string, error) {
	candidates := []string{cleanDungeonMonsterPath(listedPath)}
	if !strings.HasPrefix(strings.ToLower(candidates[0]), "monster/") {
		candidates = append(candidates, path.Join("monster", candidates[0]))
	}
	var lastErr error
	for _, candidate := range candidates {
		text, err := source.ReadText(candidate)
		if err == nil {
			return candidate, text, nil
		}
		lastErr = err
	}
	return "", "", lastErr
}

func cleanDungeonMonsterPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	return strings.TrimPrefix(value, "/")
}

func parseDungeonMonsterDefinition(id int64, docPath string, document *dnfpvf.Document) dnfmonster.Monster {
	definition := dnfmonster.Monster{
		ID:      id,
		Path:    docPath,
		Name:    dungeonMonsterText(document, "name", "display name"),
		Kind:    dungeonMonsterText(document, "monster type", "type", "kind"),
		Rank:    dungeonMonsterText(document, "rank", "monster rank", "grade"),
		Level:   dungeonMonsterInt(document, "level", "monster level"),
		HP:      dungeonMonsterInt(document, "hp", "hit point", "max hp"),
		Attack:  dungeonMonsterInt(document, "attack", "physical attack"),
		Defense: dungeonMonsterInt(document, "defense", "physical defense"),
		Move:    dungeonMonsterNumber(document, "move speed", "movement speed"),
		Speed:   dungeonMonsterNumber(document, "attack speed"),
		Exp:     dungeonMonsterInt(document, "exp", "experience"),
		AI:      dungeonMonsterText(document, "ai", "ai pattern"),
		Icon:    dungeonMonsterText(document, "icon", "icon path"),
		Scalars: dungeonMonsterScalars(document),
	}
	if definition.Name == "" {
		definition.Name = definition.Path
	}
	if document != nil {
		definition.Sections = make([]string, 0, len(document.Sections))
		for _, section := range document.Sections {
			definition.Sections = append(definition.Sections, section.Name)
		}
	}
	return definition
}

func dungeonMonsterText(document *dnfpvf.Document, names ...string) string {
	for _, name := range names {
		if value, ok := document.Text(name); ok {
			return value
		}
	}
	return ""
}

func dungeonMonsterInt(document *dnfpvf.Document, names ...string) int64 {
	for _, name := range names {
		if value, ok := document.Int(name); ok {
			return value
		}
	}
	return 0
}

func dungeonMonsterNumber(document *dnfpvf.Document, names ...string) float64 {
	for _, name := range names {
		if value, ok := document.Number(name); ok {
			return value
		}
	}
	return 0
}

func dungeonMonsterScalars(document *dnfpvf.Document) map[string]float64 {
	names := map[string][]string{
		"hp":            {"hp", "hit point", "max hp"},
		"attack":        {"attack", "physical attack"},
		"magic_attack":  {"magical attack", "magic attack"},
		"defense":       {"defense", "physical defense"},
		"magic_defense": {"magical defense", "magic defense"},
		"move_speed":    {"move speed", "movement speed"},
		"attack_speed":  {"attack speed"},
		"hit_recovery":  {"hit recovery"},
		"exp":           {"exp", "experience"},
	}
	out := make(map[string]float64)
	for key, aliases := range names {
		for _, alias := range aliases {
			if value, ok := document.Number(alias); ok {
				out[key] = value
				break
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneDungeonMonsterDefinition(definition dnfmonster.Monster) dnfmonster.Monster {
	definition.Scalars = cloneFloatMap(definition.Scalars)
	definition.Sections = append([]string(nil), definition.Sections...)
	return definition
}
