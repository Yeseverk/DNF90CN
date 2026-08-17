package app

import (
	"longheng.io/server/internal/platform/config"
)

func (a *Application) configSnapshot() config.ServiceConfig {
	if a == nil {
		return config.ServiceConfig{}
	}
	a.cfgMu.RLock()
	cfg := a.cfg
	a.cfgMu.RUnlock()
	return cfg
}

func (a *Application) setConfig(cfg config.ServiceConfig) {
	if a == nil {
		return
	}
	a.cfgMu.Lock()
	a.cfg = cfg
	a.cfgMu.Unlock()
}
