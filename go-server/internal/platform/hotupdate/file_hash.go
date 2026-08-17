package hotupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type FileHashEntry struct {
	Path        string `json:"path"`
	AbsPath     string `json:"abs_path,omitempty"`
	Bytes       int64  `json:"bytes"`
	ModTimeUnix int64  `json:"mod_time_unix"`
	Checksum    string `json:"checksum"`
}

type FileHashCacheStats struct {
	Entries int `json:"entries"`
	Hits    int `json:"hits"`
	Misses  int `json:"misses"`
}

type FileHashCache struct {
	mu      sync.Mutex
	entries map[string]FileHashEntry
	hits    int
	misses  int
}

func NewFileHashCache(entries ...FileHashEntry) *FileHashCache {
	cache := &FileHashCache{entries: make(map[string]FileHashEntry, len(entries))}
	for _, entry := range entries {
		if entry.AbsPath == "" || entry.Checksum == "" {
			continue
		}
		cache.entries[entry.AbsPath] = entry
	}
	return cache
}

func (c *FileHashCache) Snapshot() []FileHashEntry {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	out := make([]FileHashEntry, 0, len(c.entries))
	for _, entry := range c.entries {
		out = append(out, entry)
	}
	c.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].AbsPath < out[j].AbsPath })
	return out
}

func (c *FileHashCache) Stats() FileHashCacheStats {
	if c == nil {
		return FileHashCacheStats{}
	}
	c.mu.Lock()
	stats := FileHashCacheStats{Entries: len(c.entries), Hits: c.hits, Misses: c.misses}
	c.mu.Unlock()
	return stats
}

func ScanBundleWithCache(ctx context.Context, sourceDir, version string, cache *FileHashCache) (BundleManifest, error) {
	if err := contextErr(ctx); err != nil {
		return BundleManifest{}, err
	}
	files, err := bundleFilesHash(ctx, sourceDir, cache)
	if err != nil {
		return BundleManifest{}, err
	}
	bytes, checksum := bundleSummary(files)
	return BundleManifest{
		Version:   version,
		Files:     cloneBundleFiles(files),
		Bytes:     bytes,
		Checksum:  checksum,
		CreatedAt: time.Now().UTC(),
	}, nil
}

func bundleFilesHash(ctx context.Context, root string, cache *FileHashCache) ([]BundleFile, error) {
	if cache == nil {
		return bundleFiles(ctx, root)
	}
	root = filepath.Clean(root)
	var files []BundleFile
	if err := filepath.WalkDir(root, func(filePath string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := contextErr(ctx); ctxErr != nil {
			return ctxErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("hot update bundle rejects symlink %q", filePath)
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		file, err := cache.bundleFile(ctx, filePath, filepath.ToSlash(rel), info)
		if err != nil {
			return err
		}
		files = append(files, file)
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func (c *FileHashCache) bundleFile(ctx context.Context, filePath, rel string, info os.FileInfo) (BundleFile, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return BundleFile{}, err
	}
	modUnix := info.ModTime().UTC().UnixNano()
	c.mu.Lock()
	if entry, ok := c.entries[absPath]; ok && entry.Bytes == info.Size() && entry.ModTimeUnix == modUnix && entry.Checksum != "" {
		c.hits++
		c.mu.Unlock()
		return BundleFile{Path: rel, Bytes: entry.Bytes, Checksum: entry.Checksum}, nil
	}
	c.misses++
	c.mu.Unlock()

	if err := contextErr(ctx); err != nil {
		return BundleFile{}, err
	}
	data, err := os.ReadFile(filePath) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if err != nil {
		return BundleFile{}, err
	}
	sum := sha256.Sum256(data)
	entry := FileHashEntry{
		Path:        rel,
		AbsPath:     absPath,
		Bytes:       int64(len(data)),
		ModTimeUnix: modUnix,
		Checksum:    hex.EncodeToString(sum[:]),
	}
	c.mu.Lock()
	c.entries[absPath] = entry
	c.mu.Unlock()
	return BundleFile{Path: rel, Bytes: entry.Bytes, Checksum: entry.Checksum}, nil
}
