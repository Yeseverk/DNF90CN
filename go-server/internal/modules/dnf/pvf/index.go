package pvf

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

// Source 表示可以按路径读取 PVF 文本的内存来源。
type Source interface {
	ReadText(relativePath string) (string, error)
}

// BuildOptions 控制 DNF PVF 内存索引的预加载范围。
type BuildOptions struct {
	All      bool
	Paths    []string
	Lists    []string
	Prefixes []string
}

// Index 保存已解析的 DNF PVF 文档和 `.lst` 引用索引。
type Index struct {
	docs  map[string]*Document
	lists map[string][]ListEntry
	refs  map[string]map[int64]string
	paths []string
}

type fileLister interface {
	Files() []platformpvf.File
}

// Build 从已加载的 PVF source 中构建项目层内存索引。
func Build(ctx context.Context, source Source, options BuildOptions) (*Index, error) {
	if source == nil {
		return nil, ErrSourceRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sourcePaths := sourcePathSet(source)
	paths := collectPaths(source, options, sourcePaths)
	listKeys := listKeySet(options.Lists)
	listPaths := append([]string(nil), options.Lists...)
	index := &Index{
		docs:  make(map[string]*Document, len(paths)),
		lists: make(map[string][]ListEntry, len(options.Lists)),
		refs:  make(map[string]map[int64]string, len(options.Lists)),
		paths: make([]string, 0, len(paths)),
	}
	for pos := 0; pos < len(paths); pos++ {
		docPath := paths[pos]
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text, err := source.ReadText(docPath)
		if err != nil {
			return nil, fmt.Errorf("read dnf pvf %q: %w", docPath, err)
		}
		doc, err := Parse(docPath, text)
		if err != nil {
			return nil, fmt.Errorf("parse dnf pvf %q: %w", docPath, err)
		}
		key := pathKey(docPath)
		index.docs[key] = doc
		index.paths = append(index.paths, doc.Path)
		if _, ok := listKeys[key]; ok {
			for _, entry := range ParseList(doc) {
				resolved := resolveListRef(doc.Path, entry.Path, sourcePaths)
				addPath(&paths, index.docs, resolved)
				if strings.EqualFold(path.Ext(resolved), ".lst") {
					resolvedKey := pathKey(resolved)
					if _, exists := listKeys[resolvedKey]; !exists {
						listKeys[resolvedKey] = struct{}{}
						listPaths = append(listPaths, resolved)
					}
				}
			}
		}
	}
	sort.Strings(index.paths)
	index.buildLists(listPaths, sourcePaths)
	return index, nil
}

// Document 按路径返回已解析文档，不会重新读取 PVF。
func (i *Index) Document(docPath string) (*Document, bool) {
	if i == nil {
		return nil, false
	}
	doc, ok := i.docs[pathKey(docPath)]
	return doc, ok
}

// Paths 返回当前索引中的文档路径副本。
func (i *Index) Paths() []string {
	if i == nil || len(i.paths) == 0 {
		return nil
	}
	out := make([]string, len(i.paths))
	copy(out, i.paths)
	return out
}

// List 返回指定 `.lst` 文档的引用列表副本。
func (i *Index) List(listPath string) []ListEntry {
	if i == nil {
		return nil
	}
	entries := i.lists[pathKey(listPath)]
	out := make([]ListEntry, len(entries))
	copy(out, entries)
	return out
}

// Resolve 从指定 `.lst` 索引中按 id 查找 PVF 路径。
func (i *Index) Resolve(listPath string, id int64) (string, bool) {
	if i == nil {
		return "", false
	}
	refs := i.refs[pathKey(listPath)]
	if refs == nil {
		return "", false
	}
	value, ok := refs[id]
	return value, ok
}

// Snapshot 返回当前内存索引的文档、列表和引用数量。
func (i *Index) Snapshot() Snapshot {
	if i == nil {
		return Snapshot{}
	}
	refs := 0
	for _, entries := range i.lists {
		refs += len(entries)
	}
	return Snapshot{
		Documents: len(i.docs),
		Lists:     len(i.lists),
		Refs:      refs,
	}
}

func (i *Index) buildLists(lists []string, sourcePaths map[string]string) {
	for _, listPath := range lists {
		key := pathKey(listPath)
		doc := i.docs[key]
		if doc == nil {
			continue
		}
		entries := ParseList(doc)
		for idx := range entries {
			entries[idx].Path = resolveListRef(doc.Path, entries[idx].Path, sourcePaths)
		}
		i.lists[key] = entries
		refMap := make(map[int64]string, len(entries))
		for _, entry := range entries {
			if _, exists := refMap[entry.ID]; !exists {
				refMap[entry.ID] = entry.Path
			}
		}
		i.refs[key] = refMap
	}
}

func collectPaths(source Source, options BuildOptions, sourcePaths map[string]string) []string {
	pathSet := make(map[string]struct{})
	paths := make([]string, 0, len(options.Paths)+len(options.Lists))
	addPath := func(value string) {
		clean := cleanPath(value)
		if clean == "" {
			return
		}
		key := pathKey(clean)
		if _, exists := pathSet[key]; exists {
			return
		}
		pathSet[key] = struct{}{}
		if actual, ok := sourcePaths[key]; ok {
			paths = append(paths, actual)
			return
		}
		paths = append(paths, clean)
	}
	for _, value := range options.Paths {
		addPath(value)
	}
	for _, value := range options.Lists {
		addPath(value)
	}
	if lister, ok := source.(fileLister); ok && (options.All || len(options.Prefixes) > 0) {
		for _, file := range lister.Files() {
			if options.All || matchPrefix(file.ArchivePath, options.Prefixes) {
				addPath(file.ArchivePath)
			}
		}
	}
	sort.Strings(paths)
	return paths
}

func sourcePathSet(source Source) map[string]string {
	lister, ok := source.(fileLister)
	if !ok {
		return nil
	}
	files := lister.Files()
	if len(files) == 0 {
		return nil
	}
	out := make(map[string]string, len(files))
	for _, file := range files {
		clean := cleanPath(file.ArchivePath)
		key := pathKey(clean)
		if key == "" {
			continue
		}
		if _, exists := out[key]; !exists {
			out[key] = clean
		}
	}
	return out
}

func resolveListRef(listPath, refPath string, sourcePaths map[string]string) string {
	ref := cleanPath(refPath)
	if ref == "" {
		return ""
	}
	if actual, ok := sourcePaths[pathKey(ref)]; ok {
		return actual
	}
	listDir := cleanPath(path.Dir(cleanPath(listPath)))
	if listDir == "." {
		listDir = ""
	}
	if listDir != "" {
		candidate := cleanPath(path.Join(listDir, ref))
		if actual, ok := sourcePaths[pathKey(candidate)]; ok {
			return actual
		}
		listDirKey := pathKey(listDir)
		refKey := pathKey(ref)
		if listDirKey != "" && strings.HasPrefix(refKey, listDirKey+"/") {
			return ref
		}
		return candidate
	}
	return ref
}

func addPath(paths *[]string, docs map[string]*Document, value string) {
	clean := cleanPath(value)
	if clean == "" {
		return
	}
	key := pathKey(clean)
	if _, exists := docs[key]; exists {
		return
	}
	for _, existing := range *paths {
		if pathKey(existing) == key {
			return
		}
	}
	*paths = append(*paths, clean)
}

func listKeySet(lists []string) map[string]struct{} {
	out := make(map[string]struct{}, len(lists))
	for _, value := range lists {
		key := pathKey(value)
		if key != "" {
			out[key] = struct{}{}
		}
	}
	return out
}
