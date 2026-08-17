package app

import (
	"fmt"

	"longheng.io/server/internal/platform/config"
	"longheng.io/server/internal/platform/pvf"
)

// newPVFArchive adapts the DNF Script.pvf resource into the generic platform
// environment while leaving the framework's core resource model unchanged.
func newPVFArchive(cfg config.ServiceConfig) (*pvf.Archive, error) {
	if !cfg.PVF.Enabled {
		return nil, nil
	}
	archive, err := pvf.LoadArchive(pvf.Options{
		Path:     cfg.PVF.Path,
		MaxBytes: cfg.PVF.MaxBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("load pvf archive: %w", err)
	}
	if cfg.PVF.PreloadChunks {
		if _, err := archive.PreloadAll(); err != nil {
			return nil, fmt.Errorf("preload pvf chunks: %w", err)
		}
	}
	return archive, nil
}
