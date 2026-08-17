package db

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// WriteFileAtomically 通过同目录临时文件、fsync 和原子替换持久化写入单个文件。
func WriteFileAtomically(ctx context.Context, path string, data []byte, perm fs.FileMode) error {
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("atomic file path is required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	keepTmp := true
	defer func() {
		if keepTmp {
			_ = os.Remove(tmpName)
		}
	}()
	if err := writeAndSyncTempFile(tmp, data, perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := replaceFile(tmpName, path); err != nil {
		return err
	}
	keepTmp = false
	return syncDir(dir)
}

func writeAndSyncTempFile(file *os.File, data []byte, perm fs.FileMode) error {
	written, err := file.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	if err := file.Chmod(perm); err != nil {
		return err
	}
	return file.Sync()
}

func replaceFile(tmpName, path string) error {
	if err := os.Rename(tmpName, path); err == nil {
		return nil
	} else if _, statErr := os.Stat(path); statErr != nil {
		return err
	}

	backupPath := tmpName + ".bak"
	if err := os.Rename(path, backupPath); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		restoreErr := os.Rename(backupPath, path)
		return errors.Join(err, restoreErr)
	}
	if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func syncDir(dir string) error {
	file, err := os.Open(dir) //nolint:gosec // G304：目录来自调用点限定的持久化路径。
	if err != nil {
		if runtime.GOOS == "windows" {
			return nil
		}
		return err
	}
	defer func() { _ = file.Close() }()
	if err := file.Sync(); err != nil && runtime.GOOS != "windows" {
		return err
	}
	return nil
}
