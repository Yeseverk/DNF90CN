package worldmap

import (
	"fmt"
	"path"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

// mapDocumentLoader resolves an existing map document.  The caller keeps the
// source-specific path lookup out of the import parser so Load and LoadSource
// share the same PVF import semantics.
type mapDocumentLoader func(candidate string) (string, *dnfpvf.Document, error)

func parseMapWithImports(id int64, docPath string, doc *dnfpvf.Document, load mapDocumentLoader) (Map, error) {
	if doc == nil {
		return Map{}, fmt.Errorf("map document %q is nil", docPath)
	}
	if load == nil {
		return Map{}, fmt.Errorf("map document loader is nil")
	}

	cache := make(map[string]Map)
	visiting := make(map[string]struct{})
	var parse func(string, *dnfpvf.Document) (Map, error)
	parse = func(currentPath string, currentDoc *dnfpvf.Document) (Map, error) {
		key := pathKey(currentPath)
		if parsed, ok := cache[key]; ok {
			return cloneMap(parsed), nil
		}
		if _, exists := visiting[key]; exists {
			return Map{}, fmt.Errorf("cyclic map import at %q", currentPath)
		}
		visiting[key] = struct{}{}
		defer delete(visiting, key)

		parsed := ParseMap(0, currentPath, currentDoc)
		for _, reference := range mapImportReferences(currentDoc) {
			importPath, importDoc, err := loadMapImport(currentPath, reference, load)
			if err != nil {
				return Map{}, err
			}
			imported, err := parse(importPath, importDoc)
			if err != nil {
				return Map{}, err
			}
			parsed = mergeImportedMapStaticSpawns(parsed, imported)
		}
		cache[key] = cloneMap(parsed)
		return parsed, nil
	}

	parsed, err := parse(docPath, doc)
	if err != nil {
		return Map{}, err
	}
	parsed.ID = id
	return parsed, nil
}

func mapImportReferences(doc *dnfpvf.Document) []string {
	sections, _ := rawSections(doc)
	var out []string
	for _, section := range sections {
		if sectionKey(section.Name) != "import script" {
			continue
		}
		for _, reference := range texts(section) {
			reference = strings.TrimSpace(reference)
			if reference != "" {
				out = append(out, reference)
			}
		}
	}
	return out
}

func loadMapImport(ownerPath, reference string, load mapDocumentLoader) (string, *dnfpvf.Document, error) {
	var lastErr error
	for _, candidate := range mapImportCandidates(ownerPath, reference) {
		resolvedPath, doc, err := load(candidate)
		if err == nil && doc != nil {
			return resolvedPath, doc, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no matching document")
	}
	return "", nil, fmt.Errorf("resolve map import %q from %q: %w", reference, ownerPath, lastErr)
}

func mapImportCandidates(ownerPath, reference string) []string {
	reference = strings.TrimSpace(strings.ReplaceAll(reference, "\\", "/"))
	for strings.HasPrefix(reference, "./") || strings.HasPrefix(reference, "/") {
		reference = strings.TrimPrefix(reference, "./")
		reference = strings.TrimPrefix(reference, "/")
	}
	if reference == "" {
		return nil
	}

	ownerPath = strings.TrimSpace(strings.ReplaceAll(ownerPath, "\\", "/"))
	var candidates []string
	add := func(candidate string) {
		candidate = strings.TrimPrefix(path.Clean(candidate), "./")
		if candidate == "." || candidate == "" {
			return
		}
		for _, existing := range candidates {
			if pathKey(existing) == pathKey(candidate) {
				return
			}
		}
		candidates = append(candidates, candidate)
	}
	add(reference)
	add(path.Join(path.Dir(ownerPath), reference))

	parts := strings.Split(ownerPath, "/")
	for index, part := range parts {
		if !strings.EqualFold(part, "map") {
			continue
		}
		add(path.Join(strings.Join(parts[:index+1], "/"), reference))
		break
	}
	return candidates
}

// Imported town maps commonly carry the physical NPC and passive-object
// placements shared by several maps.  Keep the root map metadata authoritative
// while exposing those real imported placements to the runtime catalog.
func mergeImportedMapStaticSpawns(base, imported Map) Map {
	imported = cloneMap(imported)
	base.PathgatePositions = append(imported.PathgatePositions, base.PathgatePositions...)
	base.PathgateObjects = append(imported.PathgateObjects, base.PathgateObjects...)
	base.Portals = buildPortals(base.PathgatePositions, base.PathgateObjects)
	base.AnimationObjects = append(imported.AnimationObjects, base.AnimationObjects...)
	base.PassiveObjects = append(imported.PassiveObjects, base.PassiveObjects...)
	base.SpecialPassiveObjects = append(imported.SpecialPassiveObjects, base.SpecialPassiveObjects...)
	base.NPCs = append(imported.NPCs, base.NPCs...)
	base.AICharacters = append(imported.AICharacters, base.AICharacters...)
	base.TownMovableAreas = append(imported.TownMovableAreas, base.TownMovableAreas...)
	base.DungeonMovableAreas = append(imported.DungeonMovableAreas, base.DungeonMovableAreas...)
	return base
}
