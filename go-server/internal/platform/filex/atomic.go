package filex

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	if path == "" {
		return fmt.Errorf("atomic write path is required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	closed := false
	defer func() {
		if !closed {
			_ = tmp.Close()
		}
		_ = os.Remove(tmpName)
	}()
	written, err := tmp.Write(data)
	if err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if written != len(data) {
		return fmt.Errorf("write temp file: %w", io.ErrShortWrite)
	}
	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	closed = true
	if err := os.Rename(tmpName, path); err == nil {
		return syncDir(dir)
	}
	if err := replaceWithBackup(tmpName, path); err != nil {
		return err
	}
	return syncDir(dir)
}

func replaceWithBackup(tmpName, path string) error {
	backup := tmpName + ".bak"
	hadExisting := false
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			return fmt.Errorf("replace file %s: destination is a directory", path)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat existing file: %w", err)
	}
	if err := os.Rename(path, backup); err == nil {
		hadExisting = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("backup existing file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		if hadExisting {
			_ = os.Rename(backup, path)
		}
		return fmt.Errorf("replace file: %w", err)
	}
	if hadExisting {
		_ = os.Remove(backup)
	}
	return nil
}

func syncDir(dir string) error {
	file, err := os.Open(dir) //nolint:gosec // G304：目录来自调用点限定的输出路径。
	if err != nil {
		if runtime.GOOS == "windows" {
			return nil
		}
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer func() { _ = file.Close() }()
	if err := file.Sync(); err != nil && runtime.GOOS != "windows" {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}
