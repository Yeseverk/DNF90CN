package datatable

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrDirectoryRequired = errors.New("data table directory is required")
	ErrRegistryRequired  = errors.New("data table registry is required")
	ErrTableRequired     = errors.New("data table name is required")
	ErrTableNotFound     = errors.New("data table not found")
)

type Table struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Version  string    `json:"version"`
	Checksum string    `json:"checksum"`
	Rows     int       `json:"rows"`
	Bytes    int64     `json:"bytes"`
	LoadedAt time.Time `json:"loaded_at"`
}

type Registry struct {
	root    string
	version string

	mu       sync.RWMutex
	tables   map[string]Table
	raw      map[string][]byte
	manifest Manifest
	watchers map[chan LoadResult]struct{}
}

func NewRegistry(root, version string) *Registry {
	root = strings.TrimSpace(root)
	version = strings.TrimSpace(version)
	return &Registry{
		root:     root,
		version:  version,
		tables:   make(map[string]Table),
		raw:      make(map[string][]byte),
		manifest: Manifest{Root: root, Version: version},
		watchers: make(map[chan LoadResult]struct{}),
	}
}

func (r *Registry) Load(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	root := strings.TrimSpace(r.root)
	version := r.version
	r.mu.RUnlock()
	_, err := r.ReloadDir(ctx, root, version, ReloadOptions{})
	return err
}

func (r *Registry) LoadDir(ctx context.Context, root, version string) error {
	_, err := r.ReloadDir(ctx, root, version, ReloadOptions{})
	return err
}

func (r *Registry) Reload(ctx context.Context, options ReloadOptions) (LoadResult, error) {
	if r == nil {
		return LoadResult{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	root := strings.TrimSpace(r.root)
	version := r.version
	r.mu.RUnlock()
	return r.ReloadDir(ctx, root, version, options)
}

func (r *Registry) ReloadDir(ctx context.Context, root, version string, options ReloadOptions) (LoadResult, error) {
	if r == nil {
		return LoadResult{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dataset, err := loadDirectory(ctx, root, version)
	if err != nil {
		return LoadResult{}, err
	}
	if err := options.validate(ctx, dataset); err != nil {
		return LoadResult{}, err
	}

	r.mu.Lock()
	diff := diffTables(r.tables, dataset.tables)
	result := LoadResult{
		Manifest: dataset.manifest,
		Diff:     diff,
		LoadedAt: dataset.manifest.LoadedAt,
	}
	r.root = dataset.root
	r.version = dataset.version
	r.tables = cloneTableMap(dataset.tables)
	r.raw = cloneRawMap(dataset.raw)
	r.manifest = dataset.manifest
	for ch := range r.watchers {
		select {
		case ch <- cloneLoadResult(result):
		default:
		}
	}
	r.mu.Unlock()
	return cloneLoadResult(result), nil
}

func (r *Registry) Configure(root, version string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.root = strings.TrimSpace(root)
	r.version = strings.TrimSpace(version)
	if len(r.tables) == 0 {
		r.manifest = Manifest{Root: r.root, Version: r.version}
	}
	r.mu.Unlock()
}

func (r *Registry) Clear() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.tables = make(map[string]Table)
	r.raw = make(map[string][]byte)
	r.manifest = Manifest{Root: r.root, Version: r.version}
	r.mu.Unlock()
}

func (r *Registry) Snapshot() []Table {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	out := make([]Table, 0, len(r.tables))
	for _, table := range r.tables {
		out = append(out, table)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *Registry) Manifest() Manifest {
	if r == nil {
		return Manifest{}
	}
	r.mu.RLock()
	manifest := r.manifest
	r.mu.RUnlock()
	return manifest
}

func (r *Registry) Decode(name string, out any) error {
	if r == nil {
		return ErrTableNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrTableRequired
	}
	if out == nil {
		return fmt.Errorf("decode target for data table %q is nil", name)
	}
	r.mu.RLock()
	data, ok := r.raw[name]
	data = append([]byte(nil), data...)
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrTableNotFound, name)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode data table %q: %w", name, err)
	}
	return nil
}

func (r *Registry) Get(name string) (Table, bool) {
	if r == nil {
		return Table{}, false
	}
	name = strings.TrimSpace(name)
	r.mu.RLock()
	table, ok := r.tables[name]
	r.mu.RUnlock()
	return table, ok
}

func (r *Registry) Root() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	root := r.root
	r.mu.RUnlock()
	return root
}

func (r *Registry) Version() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	version := r.version
	r.mu.RUnlock()
	return version
}

func (r *Registry) Watch(ctx context.Context, buffer int) (<-chan LoadResult, error) {
	if ctxErr := contextErr(ctx); ctxErr != nil {
		return nil, ctxErr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if r == nil {
		return nil, ErrRegistryRequired
	}
	if buffer < 0 {
		buffer = 0
	}
	ch := make(chan LoadResult, buffer)
	r.mu.Lock()
	if r.watchers == nil {
		r.watchers = make(map[chan LoadResult]struct{})
	}
	r.watchers[ch] = struct{}{}
	r.mu.Unlock()
	go func() {
		<-ctx.Done()
		r.mu.Lock()
		if _, ok := r.watchers[ch]; ok {
			delete(r.watchers, ch)
			close(ch)
		}
		r.mu.Unlock()
	}()
	return ch, nil
}

func loadDirectory(ctx context.Context, root, version string) (Dataset, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	root = strings.TrimSpace(root)
	version = strings.TrimSpace(version)
	if root == "" {
		return Dataset{}, ErrDirectoryRequired
	}
	info, err := os.Stat(root)
	if err != nil {
		return Dataset{}, fmt.Errorf("stat data table directory %q: %w", root, err)
	}
	if !info.IsDir() {
		return Dataset{}, fmt.Errorf("data table directory %q is not a directory", root)
	}

	loadedAt := time.Now().UTC()
	tables := make(map[string]Table)
	raw := make(map[string][]byte)
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if entry.IsDir() {
			return nil
		}
		if !isJSONTable(entry.Name()) {
			return nil
		}
		table, data, err := loadOne(root, version, path, loadedAt)
		if err != nil {
			return err
		}
		if existing, ok := tables[table.Name]; ok {
			return fmt.Errorf("duplicate data table name %q from %q and %q", table.Name, existing.Path, table.Path)
		}
		tables[table.Name] = table
		raw[table.Name] = data
		return nil
	}); err != nil {
		return Dataset{}, err
	}
	manifest := buildManifest(root, version, tables, loadedAt)
	return Dataset{
		root:     root,
		version:  version,
		tables:   tables,
		raw:      raw,
		manifest: manifest,
	}, nil
}

func loadOne(root, version, path string, loadedAt time.Time) (Table, []byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if err != nil {
		return Table{}, nil, fmt.Errorf("read data table %q: %w", path, err)
	}
	rows, err := countRows(data)
	if err != nil {
		return Table{}, nil, fmt.Errorf("parse data table %q: %w", path, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return Table{}, nil, fmt.Errorf("resolve data table path %q: %w", path, err)
	}
	rel = filepath.ToSlash(rel)
	name := strings.TrimSuffix(rel, filepath.Ext(rel))
	sum := sha256.Sum256(data)
	return Table{
		Name:     name,
		Path:     rel,
		Version:  version,
		Checksum: hex.EncodeToString(sum[:]),
		Rows:     rows,
		Bytes:    int64(len(data)),
		LoadedAt: loadedAt,
	}, append([]byte(nil), data...), nil
}

func countRows(data []byte) (int, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return 0, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return 0, fmt.Errorf("data table contains more than one JSON value")
		}
		return 0, err
	}
	switch typed := value.(type) {
	case []any:
		return len(typed), nil
	case map[string]any:
		if items, ok := typed["items"].([]any); ok {
			return len(items), nil
		}
		return 1, nil
	default:
		return 1, nil
	}
}

func isJSONTable(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == ".DS_Store" || strings.HasPrefix(name, "._") {
		return false
	}
	return strings.EqualFold(filepath.Ext(name), ".json")
}
