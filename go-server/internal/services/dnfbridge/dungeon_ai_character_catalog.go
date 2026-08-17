package dnfbridge

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
	"longheng.io/server/internal/modules/dnf/worldmap"
)

const defaultDungeonAICharacterList = "AICharacter/AICharacter.lst"

var (
	errDungeonAICharacterSourceRequired = errors.New("dnf dungeon AI character PVF source is required")
	errDungeonAICharacterListEmpty      = errors.New("dnf dungeon AI character list is empty")
	errDungeonAICharacterMinimumInfo    = errors.New("dnf dungeon AI character minimum info is malformed")
	errDungeonAICharacterUnavailable    = errors.New("dnf dungeon AI character catalog is unavailable")
)

type pvfDungeonAICharacterSection struct {
	Name   string
	Tokens []dnfpvf.Token
}

type pvfDungeonAICharacterDefinition struct {
	ID          int64
	Path        string
	Name        string
	Level       byte
	MinimumInfo []dnfpvf.Token
	Sections    []pvfDungeonAICharacterSection
}

type pvfDungeonAICharacterCatalog struct {
	mu     sync.Mutex
	source dnfpvf.Source
	paths  map[int64]string
	cache  map[int64]pvfDungeonAICharacterDefinition
}

func newPVFDungeonAICharacterCatalog(source dnfpvf.Source) (*pvfDungeonAICharacterCatalog, error) {
	if source == nil {
		return nil, errDungeonAICharacterSourceRequired
	}
	text, err := source.ReadText(defaultDungeonAICharacterList)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", defaultDungeonAICharacterList, err)
	}
	document, err := dnfpvf.Parse(defaultDungeonAICharacterList, text)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", defaultDungeonAICharacterList, err)
	}
	entries := dnfpvf.ParseList(document)
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: %s", errDungeonAICharacterListEmpty, defaultDungeonAICharacterList)
	}
	catalog := &pvfDungeonAICharacterCatalog{
		source: source,
		paths:  make(map[int64]string, len(entries)),
		cache:  make(map[int64]pvfDungeonAICharacterDefinition),
	}
	for _, entry := range entries {
		if _, exists := catalog.paths[entry.ID]; !exists {
			catalog.paths[entry.ID] = cleanDungeonAICharacterPath(entry.Path)
		}
	}
	return catalog, nil
}

func (c *pvfDungeonAICharacterCatalog) Count() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.paths)
}

func (c *pvfDungeonAICharacterCatalog) Find(id int64) (pvfDungeonAICharacterDefinition, bool, error) {
	if c == nil || c.source == nil {
		return pvfDungeonAICharacterDefinition{}, false, errDungeonAICharacterUnavailable
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.cache[id]; ok {
		return cloneDungeonAICharacterDefinition(cached), true, nil
	}
	listedPath, ok := c.paths[id]
	if !ok {
		return pvfDungeonAICharacterDefinition{}, false, nil
	}
	docPath, text, err := readDungeonAICharacterText(c.source, listedPath)
	if err != nil {
		return pvfDungeonAICharacterDefinition{}, false, fmt.Errorf("read AI character id=%d path=%s: %w", id, listedPath, err)
	}
	document, err := worldmap.ParseDocument(docPath, text)
	if err != nil {
		return pvfDungeonAICharacterDefinition{}, false, fmt.Errorf("parse AI character id=%d path=%s: %w", id, docPath, err)
	}
	definition, err := parseDungeonAICharacterDefinition(id, docPath, document)
	if err != nil {
		return pvfDungeonAICharacterDefinition{}, false, err
	}
	c.cache[id] = definition
	return cloneDungeonAICharacterDefinition(definition), true, nil
}

func readDungeonAICharacterText(source dnfpvf.Source, listedPath string) (string, string, error) {
	candidates := []string{cleanDungeonAICharacterPath(listedPath)}
	if !strings.HasPrefix(strings.ToLower(candidates[0]), "aicharacter/") {
		candidates = append(candidates, path.Join("AICharacter", candidates[0]))
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

func cleanDungeonAICharacterPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	return strings.TrimPrefix(value, "/")
}

func parseDungeonAICharacterDefinition(
	id int64,
	docPath string,
	document *dnfpvf.Document,
) (pvfDungeonAICharacterDefinition, error) {
	definition := pvfDungeonAICharacterDefinition{ID: id, Path: docPath}
	if document == nil {
		return definition, fmt.Errorf("%w: id=%d path=%s document=nil", errDungeonAICharacterMinimumInfo, id, docPath)
	}
	minimumInfo, ok := document.Section("minimum info")
	if !ok {
		return definition, fmt.Errorf("%w: id=%d path=%s section=missing", errDungeonAICharacterMinimumInfo, id, docPath)
	}
	definition.MinimumInfo = append([]dnfpvf.Token(nil), minimumInfo...)
	integers := make([]int64, 0, len(minimumInfo))
	for _, token := range minimumInfo {
		switch token.Kind {
		case dnfpvf.TokenString, dnfpvf.TokenIdent:
			if definition.Name == "" {
				definition.Name = token.Value
			}
		case dnfpvf.TokenInt:
			integers = append(integers, token.Int)
		}
	}
	if len(integers) < 5 || integers[4] <= 0 || integers[4] > 0xff {
		return definition, fmt.Errorf("%w: id=%d path=%s integer_count=%d level=%d",
			errDungeonAICharacterMinimumInfo,
			id,
			docPath,
			len(integers),
			minimumInfoLevel(integers),
		)
	}
	definition.Level = byte(integers[4])
	definition.Sections = cloneDungeonAICharacterSections(document)
	return definition, nil
}

func minimumInfoLevel(integers []int64) int64 {
	if len(integers) < 5 {
		return 0
	}
	return integers[4]
}

func cloneDungeonAICharacterSections(document *dnfpvf.Document) []pvfDungeonAICharacterSection {
	if document == nil || len(document.Sections) == 0 {
		return nil
	}
	sections := make([]pvfDungeonAICharacterSection, 0, len(document.Sections))
	for _, section := range document.Sections {
		if section.Start < 0 || section.Start > section.End || section.End > len(document.Tokens) {
			continue
		}
		sections = append(sections, pvfDungeonAICharacterSection{
			Name:   section.Name,
			Tokens: append([]dnfpvf.Token(nil), document.Tokens[section.Start:section.End]...),
		})
	}
	return sections
}

func cloneDungeonAICharacterDefinition(value pvfDungeonAICharacterDefinition) pvfDungeonAICharacterDefinition {
	value.MinimumInfo = append([]dnfpvf.Token(nil), value.MinimumInfo...)
	if len(value.Sections) != 0 {
		sections := make([]pvfDungeonAICharacterSection, len(value.Sections))
		for index, section := range value.Sections {
			sections[index] = pvfDungeonAICharacterSection{
				Name:   section.Name,
				Tokens: append([]dnfpvf.Token(nil), section.Tokens...),
			}
		}
		value.Sections = sections
	}
	return value
}
