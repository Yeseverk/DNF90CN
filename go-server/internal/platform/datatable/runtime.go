package datatable

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

type Manifest struct {
	Root     string    `json:"root,omitempty"`
	Version  string    `json:"version,omitempty"`
	Tables   int       `json:"tables"`
	Rows     int       `json:"rows"`
	Bytes    int64     `json:"bytes"`
	Checksum string    `json:"checksum,omitempty"`
	LoadedAt time.Time `json:"loaded_at,omitempty"`
}

type ChangeType string

const (
	ChangeAdded     ChangeType = "added"
	ChangeChanged   ChangeType = "changed"
	ChangeRemoved   ChangeType = "removed"
	ChangeUnchanged ChangeType = "unchanged"
)

type TableChange struct {
	Type   ChangeType `json:"type"`
	Name   string     `json:"name"`
	Before *Table     `json:"before,omitempty"`
	After  *Table     `json:"after,omitempty"`
}

type Diff struct {
	Added     []TableChange `json:"added,omitempty"`
	Changed   []TableChange `json:"changed,omitempty"`
	Removed   []TableChange `json:"removed,omitempty"`
	Unchanged []TableChange `json:"unchanged,omitempty"`
}

func (d Diff) HasChanges() bool {
	return len(d.Added) > 0 || len(d.Changed) > 0 || len(d.Removed) > 0
}

type LoadResult struct {
	Manifest Manifest  `json:"manifest"`
	Diff     Diff      `json:"diff"`
	LoadedAt time.Time `json:"loaded_at,omitempty"`
}

type ReloadOptions struct {
	Validators []Validator
}

func (o ReloadOptions) validate(ctx context.Context, dataset Dataset) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, validator := range o.Validators {
		if validator == nil {
			continue
		}
		if err := validator.ValidateDataTables(ctx, dataset); err != nil {
			return err
		}
	}
	return nil
}

type Validator interface {
	ValidateDataTables(context.Context, Dataset) error
}

type ValidatorFunc func(context.Context, Dataset) error

func (f ValidatorFunc) ValidateDataTables(ctx context.Context, dataset Dataset) error {
	if f == nil {
		return nil
	}
	return f(ctx, dataset)
}

type Dataset struct {
	root     string
	version  string
	tables   map[string]Table
	raw      map[string][]byte
	manifest Manifest
}

func (d Dataset) Root() string {
	return d.root
}

func (d Dataset) Version() string {
	return d.version
}

func (d Dataset) Manifest() Manifest {
	return d.manifest
}

func (d Dataset) Snapshot() []Table {
	return sortedTables(d.tables)
}

func (d Dataset) Get(name string) (Table, bool) {
	name = strings.TrimSpace(name)
	table, ok := d.tables[name]
	return table, ok
}

func (d Dataset) Decode(name string, out any) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrTableRequired
	}
	if out == nil {
		return fmt.Errorf("decode target for data table %q is nil", name)
	}
	data, ok := d.raw[name]
	if !ok {
		return fmt.Errorf("%w: %s", ErrTableNotFound, name)
	}
	if err := json.Unmarshal(append([]byte(nil), data...), out); err != nil {
		return fmt.Errorf("decode data table %q: %w", name, err)
	}
	return nil
}

func (d Dataset) Raw(name string) ([]byte, bool) {
	name = strings.TrimSpace(name)
	data, ok := d.raw[name]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), data...), true
}

func buildManifest(root, version string, tables map[string]Table, loadedAt time.Time) Manifest {
	snapshot := sortedTables(tables)
	manifest := Manifest{
		Root:     root,
		Version:  version,
		Tables:   len(snapshot),
		LoadedAt: loadedAt.UTC(),
	}
	hash := sha256.New()
	for _, table := range snapshot {
		manifest.Rows += table.Rows
		manifest.Bytes += table.Bytes
		_, _ = io.WriteString(hash, table.Name)
		hash.Write([]byte{0})
		_, _ = io.WriteString(hash, table.Path)
		hash.Write([]byte{0})
		_, _ = io.WriteString(hash, table.Checksum)
		hash.Write([]byte{0})
		_, _ = io.WriteString(hash, fmt.Sprintf("%d:%d", table.Rows, table.Bytes))
		hash.Write([]byte{0})
	}
	manifest.Checksum = hex.EncodeToString(hash.Sum(nil))
	return manifest
}

func diffTables(before, after map[string]Table) Diff {
	names := make(map[string]struct{}, len(before)+len(after))
	for name := range before {
		names[name] = struct{}{}
	}
	for name := range after {
		names[name] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)

	var diff Diff
	for _, name := range ordered {
		oldTable, hadOld := before[name]
		newTable, hasNew := after[name]
		switch {
		case !hadOld && hasNew:
			diff.Added = append(diff.Added, TableChange{Type: ChangeAdded, Name: name, After: tableRef(newTable)})
		case hadOld && !hasNew:
			diff.Removed = append(diff.Removed, TableChange{Type: ChangeRemoved, Name: name, Before: tableRef(oldTable)})
		case sameTableContent(oldTable, newTable):
			diff.Unchanged = append(diff.Unchanged, TableChange{Type: ChangeUnchanged, Name: name, Before: tableRef(oldTable), After: tableRef(newTable)})
		default:
			diff.Changed = append(diff.Changed, TableChange{Type: ChangeChanged, Name: name, Before: tableRef(oldTable), After: tableRef(newTable)})
		}
	}
	return diff
}

func sameTableContent(left, right Table) bool {
	return left.Name == right.Name &&
		left.Path == right.Path &&
		left.Checksum == right.Checksum &&
		left.Rows == right.Rows &&
		left.Bytes == right.Bytes
}

func sortedTables(tables map[string]Table) []Table {
	out := make([]Table, 0, len(tables))
	for _, table := range tables {
		out = append(out, table)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func cloneTableMap(in map[string]Table) map[string]Table {
	out := make(map[string]Table, len(in))
	for name, table := range in {
		out[name] = table
	}
	return out
}

func cloneRawMap(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for name, data := range in {
		out[name] = append([]byte(nil), data...)
	}
	return out
}

func cloneLoadResult(in LoadResult) LoadResult {
	return LoadResult{
		Manifest: in.Manifest,
		Diff:     cloneDiff(in.Diff),
		LoadedAt: in.LoadedAt,
	}
}

func cloneDiff(in Diff) Diff {
	return Diff{
		Added:     cloneTableChanges(in.Added),
		Changed:   cloneTableChanges(in.Changed),
		Removed:   cloneTableChanges(in.Removed),
		Unchanged: cloneTableChanges(in.Unchanged),
	}
}

func cloneTableChanges(in []TableChange) []TableChange {
	if len(in) == 0 {
		return nil
	}
	out := make([]TableChange, len(in))
	for i, change := range in {
		out[i] = TableChange{
			Type:   change.Type,
			Name:   change.Name,
			Before: cloneTableRef(change.Before),
			After:  cloneTableRef(change.After),
		}
	}
	return out
}

func tableRef(table Table) *Table {
	return &Table{
		Name:     table.Name,
		Path:     table.Path,
		Version:  table.Version,
		Checksum: table.Checksum,
		Rows:     table.Rows,
		Bytes:    table.Bytes,
		LoadedAt: table.LoadedAt,
	}
}

func cloneTableRef(table *Table) *Table {
	if table == nil {
		return nil
	}
	return tableRef(*table)
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
