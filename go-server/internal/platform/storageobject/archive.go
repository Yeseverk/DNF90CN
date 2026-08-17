package storageobject

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"longheng.io/server/internal/platform/filex"
)

var (
	ErrInvalidArchiveKey = errors.New("storage archive key is invalid")
	ErrArchiveCorrupt    = errors.New("storage archive checksum mismatch")
)

type ArchiveRecord struct {
	Key       string            `json:"key"`
	Size      int64             `json:"size"`
	SHA256    string            `json:"sha256"`
	Meta      map[string]string `json:"meta,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type ArchiveStore interface {
	Put(context.Context, string, []byte, map[string]string) (ArchiveRecord, error)
	Get(context.Context, string) ([]byte, ArchiveRecord, bool, error)
	Delete(context.Context, string) error
}

type LocalArchiveStore struct {
	root string
	now  func() time.Time
}

func NewLocalArchiveStore(root string, now func() time.Time) (*LocalArchiveStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, ErrInvalidArchiveKey
	}
	if now == nil {
		now = time.Now
	}
	return &LocalArchiveStore{root: root, now: now}, nil
}

func (s *LocalArchiveStore) Put(ctx context.Context, key string, data []byte, meta map[string]string) (ArchiveRecord, error) {
	if err := ctxErr(ctx); err != nil {
		return ArchiveRecord{}, err
	}
	path, manifestPath, normalized, err := s.paths(key)
	if err != nil {
		return ArchiveRecord{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return ArchiveRecord{}, err
	}
	data = append([]byte(nil), data...)
	record := ArchiveRecord{
		Key:       normalized,
		Size:      int64(len(data)),
		SHA256:    sha256Hex(data),
		Meta:      cloneArchiveMeta(meta),
		UpdatedAt: s.now().UTC(),
	}
	manifest, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return ArchiveRecord{}, err
	}
	previousData, hadData, err := readArchiveFile(path)
	if err != nil {
		return ArchiveRecord{}, err
	}
	previousManifest, hadManifest, err := readArchiveFile(manifestPath)
	if err != nil {
		return ArchiveRecord{}, err
	}
	if err := writeFileAtomic(path, data, 0o600); err != nil {
		return ArchiveRecord{}, err
	}
	if err := writeFileAtomic(manifestPath, manifest, 0o600); err != nil {
		restoreErr := restoreArchiveFiles(path, previousData, hadData, manifestPath, previousManifest, hadManifest)
		return ArchiveRecord{}, errors.Join(err, restoreErr)
	}
	return record, nil
}

func (s *LocalArchiveStore) Get(ctx context.Context, key string) ([]byte, ArchiveRecord, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, ArchiveRecord{}, false, err
	}
	path, manifestPath, _, err := s.paths(key)
	if err != nil {
		return nil, ArchiveRecord{}, false, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if errors.Is(err, os.ErrNotExist) {
		return nil, ArchiveRecord{}, false, nil
	}
	if err != nil {
		return nil, ArchiveRecord{}, false, err
	}
	var record ArchiveRecord
	manifest, err := os.ReadFile(manifestPath) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if errors.Is(err, os.ErrNotExist) {
		record = ArchiveRecord{Key: filepath.ToSlash(strings.TrimSpace(key)), Size: int64(len(data)), SHA256: sha256Hex(data)}
		return data, record, true, nil
	}
	if err != nil {
		return nil, ArchiveRecord{}, false, err
	}
	if err := json.Unmarshal(manifest, &record); err != nil {
		return nil, ArchiveRecord{}, false, err
	}
	if record.SHA256 != "" && record.SHA256 != sha256Hex(data) {
		return nil, ArchiveRecord{}, false, ErrArchiveCorrupt
	}
	return data, record, true, nil
}

func (s *LocalArchiveStore) Delete(ctx context.Context, key string) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	path, manifestPath, _, err := s.paths(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(manifestPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *LocalArchiveStore) paths(key string) (string, string, string, error) {
	if s == nil || strings.TrimSpace(s.root) == "" {
		return "", "", "", ErrInvalidArchiveKey
	}
	normalized, err := normalizeArchiveKey(key)
	if err != nil {
		return "", "", "", err
	}
	path := filepath.Join(append([]string{s.root}, strings.Split(normalized, "/")...)...)
	manifestPath := path + ".manifest.json"
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", "", "", err
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", "", "", err
	}
	if !strings.HasPrefix(absPath, root+string(os.PathSeparator)) && absPath != root {
		return "", "", "", ErrInvalidArchiveKey
	}
	return path, manifestPath, normalized, nil
}

var archiveKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func normalizeArchiveKey(key string) (string, error) {
	key = filepath.ToSlash(strings.TrimSpace(key))
	key = strings.Trim(key, "/")
	if key == "" || strings.Contains(key, "..") {
		return "", ErrInvalidArchiveKey
	}
	parts := strings.Split(key, "/")
	for _, part := range parts {
		if part == "" || !archiveKeyPattern.MatchString(part) {
			return "", ErrInvalidArchiveKey
		}
	}
	return strings.Join(parts, "/"), nil
}

var writeFileAtomic = filex.AtomicWriteFile

func readArchiveFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304：路径来自已校验的 archive key。
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return append([]byte(nil), data...), true, nil
}

func restoreArchiveFiles(path string, data []byte, hadData bool, manifestPath string, manifest []byte, hadManifest bool) error {
	return errors.Join(
		restoreArchiveFile(path, data, hadData),
		restoreArchiveFile(manifestPath, manifest, hadManifest),
	)
}

func restoreArchiveFile(path string, data []byte, existed bool) error {
	if existed {
		return writeFileAtomic(path, data, 0o600)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneArchiveMeta(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]string, len(meta))
	for key, value := range meta {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	return out
}
