package config

import "strings"

const defaultPVFMaxBytes int64 = 512 * 1024 * 1024

// PVFSection is the DNF project extension that keeps Script.pvf loading in
// the generic platform bootstrap without coupling the framework to DNF logic.
type PVFSection struct {
	Enabled       bool   `toml:"enabled"`
	Path          string `toml:"path"`
	MaxBytes      int64  `toml:"max_bytes"`
	PreloadChunks bool   `toml:"preload_chunks"`
}

func (c *ServiceConfig) normalizePVF() {
	if c.PVF.MaxBytes == 0 {
		c.PVF.MaxBytes = defaultPVFMaxBytes
	}
}

func (c *ServiceConfig) trimPVF() {
	c.PVF.Path = strings.TrimSpace(c.PVF.Path)
}

func (c ServiceConfig) validatePVF(errs *[]error) {
	if c.PVF.Enabled {
		requireString(errs, "pvf.path", c.PVF.Path)
	}
	requirePositiveInt64(errs, "pvf.max_bytes", c.PVF.MaxBytes)
}
