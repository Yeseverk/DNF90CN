package hotupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type LocalSource struct {
	VerifyOptions BundleVerifyOptions
}

func (s LocalSource) Fetch(ctx context.Context, intent Intent, workspace string) (Package, error) {
	if err := contextErr(ctx); err != nil {
		return Package{}, err
	}
	intent, err := normalizeIntent(intent)
	if err != nil {
		return Package{}, err
	}
	// LocalSource 把版本目录复制到独立 workspace，避免加载过程直接读写原始发布目录。
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		workspace = filepath.Join(os.TempDir(), "longheng-hotupdate")
	}
	sourcePath, err := localPath(intent.SourceURI)
	if err != nil {
		return Package{}, err
	}
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return Package{}, fmt.Errorf("stat hot update source %q: %w", sourcePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Package{}, fmt.Errorf("hot update source %q must not be a symlink", sourcePath)
	}
	destination, err := hotUpdateDestination(workspace, intent.Version)
	if err != nil {
		return Package{}, err
	}
	if !info.IsDir() && strings.EqualFold(filepath.Ext(sourcePath), ".zip") {
		verifyOptions := s.VerifyOptions
		if intent.Checksum != "" {
			verifyOptions.Checksum = intent.Checksum
		}
		pkg, err := ExtractBundleWithOptions(ctx, sourcePath, destination, verifyOptions)
		if err != nil {
			return Package{}, err
		}
		pkg.SourceURI = intent.SourceURI
		return pkg, nil
	}
	if !info.IsDir() {
		return Package{}, fmt.Errorf("hot update source %q is not a directory", sourcePath)
	}
	if err := preflightSourceDir(ctx, sourcePath); err != nil {
		return Package{}, err
	}
	if err := os.RemoveAll(destination); err != nil {
		return Package{}, fmt.Errorf("clear hot update workspace %q: %w", destination, err)
	}
	if err := copyDir(ctx, sourcePath, destination); err != nil {
		return Package{}, err
	}
	checksum, bytes, err := checksumDir(ctx, destination)
	if err != nil {
		return Package{}, err
	}
	if intent.Checksum != "" && !strings.EqualFold(intent.Checksum, checksum) {
		return Package{}, fmt.Errorf("hot update checksum mismatch: got %s want %s", checksum, intent.Checksum)
	}
	return Package{
		Version:   intent.Version,
		SourceURI: intent.SourceURI,
		Directory: destination,
		Checksum:  checksum,
		Bytes:     bytes,
	}, nil
}

func localPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrSourceRequired
	}
	if filepath.VolumeName(raw) != "" || filepath.IsAbs(raw) {
		return raw, nil
	}
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Scheme == "file" {
		if parsed.Path == "" {
			return "", ErrSourceRequired
		}
		return filepath.FromSlash(parsed.Path), nil
	}
	if err == nil && parsed.Scheme != "" && parsed.Scheme != "file" {
		return "", fmt.Errorf("unsupported hot update source scheme %q", parsed.Scheme)
	}
	return raw, nil
}

func preflightSourceDir(ctx context.Context, source string) error {
	// 源目录预检拒绝软链，避免热更包借软链读取或复制工作区外文件。
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := contextErr(ctx); ctxErr != nil {
			return ctxErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("hot update package rejects symlink %q", path)
		}
		return nil
	})
}

func copyDir(ctx context.Context, source, destination string) error {
	// copyDir 在预检后执行真实复制，仍重复拒绝软链，防止预检和复制之间的 TOCTOU 风险扩大。
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := contextErr(ctx); ctxErr != nil {
			return ctxErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("hot update package rejects symlink %q", path)
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(source, destination string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return err
	}
	in, err := os.Open(source) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func checksumDir(ctx context.Context, root string) (string, int64, error) {
	var files []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := contextErr(ctx); ctxErr != nil {
			return ctxErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("hot update checksum rejects symlink %q", path)
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		return "", 0, err
	}
	sort.Strings(files)
	hash := sha256.New()
	var total int64
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return "", 0, err
		}
		data, err := os.ReadFile(path) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
		if err != nil {
			return "", 0, err
		}
		total += int64(len(data))
		_, _ = hash.Write([]byte(filepath.ToSlash(rel)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), total, nil
}

func hotUpdateDestination(workspace, version string) (string, error) {
	// version 只能生成 workspace 下的目标目录，防止恶意版本号写出热更工作区。
	root, err := filepath.Abs(strings.TrimSpace(workspace))
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(root); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("hot update workspace %q must not be a symlink", root)
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("stat hot update workspace %q: %w", root, err)
	}
	base := safePathName(version)
	destination, err := filepath.Abs(filepath.Join(root, base))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, destination)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("hot update destination %q escapes workspace %q", destination, root)
	}
	return destination, nil
}

func safePathName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")
	value = strings.Trim(replacer.Replace(value), " .")
	if value == "" {
		return "unknown"
	}
	return value
}
