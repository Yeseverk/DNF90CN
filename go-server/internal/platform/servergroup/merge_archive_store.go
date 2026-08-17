package servergroup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrMergeArchiveNotFound 表示找不到指定合服工作流归档。
var ErrMergeArchiveNotFound = errors.New("server group merge workflow archive not found")

// MergeArchiveStore 保存合服工作流归档，项目侧可以替换成 MySQL、对象存储或工单系统。
type MergeArchiveStore interface {
	SaveMergeArchive(context.Context, MergeArchive) error
	GetMergeArchive(context.Context, string) (MergeArchive, bool, error)
	ListMergeArchives(context.Context) ([]MergeArchive, error)
}

// MemoryMergeArchiveStore 是测试和单进程工具使用的内存归档存储。
type MemoryMergeArchiveStore struct {
	mu       sync.RWMutex
	archives map[string]MergeArchive
}

// NewMemoryMergeArchiveStore 创建内存归档存储，并可预置一组归档。
func NewMemoryMergeArchiveStore(archives ...MergeArchive) (*MemoryMergeArchiveStore, error) {
	store := &MemoryMergeArchiveStore{archives: make(map[string]MergeArchive, len(archives))}
	for _, archive := range archives {
		normalized, err := normArchiveStore(archive)
		if err != nil {
			return nil, err
		}
		store.archives[normalized.ArchiveID] = normalized
	}
	return store, nil
}

// SaveMergeArchive 保存或覆盖内存中的合服归档。
func (s *MemoryMergeArchiveStore) SaveMergeArchive(ctx context.Context, archive MergeArchive) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreEmpty
	}
	normalized, err := normArchiveStore(archive)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.archives == nil {
		s.archives = make(map[string]MergeArchive)
	}
	s.archives[normalized.ArchiveID] = normalized
	return nil
}

// GetMergeArchive 从内存归档存储读取指定归档。
func (s *MemoryMergeArchiveStore) GetMergeArchive(ctx context.Context, archiveID string) (MergeArchive, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return MergeArchive{}, false, err
	}
	if s == nil {
		return MergeArchive{}, false, ErrStoreEmpty
	}
	archiveID = normalizeID(archiveID)
	if archiveID == "" {
		return MergeArchive{}, false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	archive, ok := s.archives[archiveID]
	return cloneMergeArchive(archive), ok, nil
}

// ListMergeArchives 返回内存中的全部合服归档。
func (s *MemoryMergeArchiveStore) ListMergeArchives(ctx context.Context) ([]MergeArchive, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrStoreEmpty
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	archives := make([]MergeArchive, 0, len(s.archives))
	for _, archive := range s.archives {
		archives = append(archives, cloneMergeArchive(archive))
	}
	sort.Slice(archives, func(i, j int) bool { return archives[i].ArchiveID < archives[j].ArchiveID })
	return archives, nil
}

// FileMergeArchiveStore 把每份归档保存为独立 JSON 文件，适合本地演练和离线交付。
type FileMergeArchiveStore struct {
	dir string
	mu  sync.Mutex
}

// NewFileMergeArchiveStore 创建文件归档存储。
func NewFileMergeArchiveStore(dir string) (*FileMergeArchiveStore, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, fmt.Errorf("%w: archive store dir is required", ErrInvalidMigration)
	}
	return &FileMergeArchiveStore{dir: dir}, nil
}

// SaveMergeArchive 将合服归档保存为 JSON 文件。
func (s *FileMergeArchiveStore) SaveMergeArchive(ctx context.Context, archive MergeArchive) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	if s == nil {
		return ErrStoreEmpty
	}
	normalized, err := normArchiveStore(archive)
	if err != nil {
		return err
	}
	path, err := s.pathForArchive(normalized.ArchiveID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeFileReplace(path, data)
}

// GetMergeArchive 从文件归档存储读取指定归档。
func (s *FileMergeArchiveStore) GetMergeArchive(ctx context.Context, archiveID string) (MergeArchive, bool, error) {
	if err := ctxErr(ctx); err != nil {
		return MergeArchive{}, false, err
	}
	if s == nil {
		return MergeArchive{}, false, ErrStoreEmpty
	}
	path, err := s.pathForArchive(archiveID)
	if err != nil {
		return MergeArchive{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(path) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if errors.Is(err, os.ErrNotExist) {
		return MergeArchive{}, false, nil
	}
	if err != nil {
		return MergeArchive{}, false, err
	}
	archive, err := decodeMergeArchive(data)
	if err != nil {
		return MergeArchive{}, false, err
	}
	return archive, true, nil
}

// ListMergeArchives 返回文件目录中的全部合服归档。
func (s *FileMergeArchiveStore) ListMergeArchives(ctx context.Context) ([]MergeArchive, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, ErrStoreEmpty
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	archives := make([]MergeArchive, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name())) //nolint:gosec // G703：路径来自框架配置或 CLI 显式输出目标，调用点负责限定输入范围。
		if err != nil {
			return nil, err
		}
		archive, err := decodeMergeArchive(data)
		if err != nil {
			return nil, err
		}
		archives = append(archives, archive)
	}
	sort.Slice(archives, func(i, j int) bool { return archives[i].ArchiveID < archives[j].ArchiveID })
	return archives, nil
}

func (s *FileMergeArchiveStore) pathForArchive(archiveID string) (string, error) {
	if s == nil {
		return "", ErrStoreEmpty
	}
	fileName, err := archiveFileName(archiveID)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.dir, fileName), nil
}

func archiveFileName(archiveID string) (string, error) {
	archiveID = normalizeID(archiveID)
	if archiveID == "" {
		return "", fmt.Errorf("%w: archive id is required", ErrInvalidMigration)
	}
	if archiveID == "." || archiveID == ".." || strings.ContainsAny(archiveID, `/\`) {
		return "", fmt.Errorf("%w: archive id %q must be a file name", ErrInvalidMigration, archiveID)
	}
	return archiveID + ".json", nil
}

func decodeMergeArchive(data []byte) (MergeArchive, error) {
	var archive MergeArchive
	if err := json.Unmarshal(data, &archive); err != nil {
		return MergeArchive{}, err
	}
	return normArchiveStore(archive)
}

func normArchiveStore(archive MergeArchive) (MergeArchive, error) {
	archive = cloneMergeArchive(archive)
	archive.ArchiveID = normalizeID(archive.ArchiveID)
	archive.Workflow = firstNonEmpty(archive.Workflow, MergeWorkflowName)
	archive.Stage = normalizeID(archive.Stage)
	if archive.Stage == "" {
		archive.Stage = MergeStageDryRun
	}
	if archive.ArchiveID == "" {
		return MergeArchive{}, fmt.Errorf("%w: archive id is required", ErrInvalidMigration)
	}
	if archive.Workflow != MergeWorkflowName {
		return MergeArchive{}, fmt.Errorf("%w: archive workflow %s is invalid", ErrInvalidMigration, archive.Workflow)
	}
	if !validMergeStage(archive.Stage) {
		return MergeArchive{}, fmt.Errorf("%w: archive stage %s is invalid", ErrInvalidMigration, archive.Stage)
	}
	archive.GeneratedAt = normalizeTime(archive.GeneratedAt)
	if archive.GeneratedAt.IsZero() {
		archive.GeneratedAt = time.Now().UTC()
	}
	archive.WorkflowID = firstNonEmpty(archive.WorkflowID)
	archive.ApprovalID = firstNonEmpty(archive.ApprovalID)
	archive.IdempotencyKey = firstNonEmpty(archive.IdempotencyKey)
	archive.OperatorID = firstNonEmpty(archive.OperatorID)
	archive.Reason = firstNonEmpty(archive.Reason)
	archive.Meta = normalizeStringMap(archive.Meta)
	return archive, nil
}
