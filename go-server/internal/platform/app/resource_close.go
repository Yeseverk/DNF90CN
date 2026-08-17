package app

import "errors"

func (a *Application) closePlatformRes() (error, error) {
	var busErr error
	if a.bus != nil {
		busErr = a.bus.Close()
	}
	if a.audit != nil {
		busErr = errors.Join(busErr, a.audit.Close())
	}
	if a.logicLog != nil {
		busErr = errors.Join(busErr, a.logicLog.Close())
	}
	if a.biLog != nil {
		busErr = errors.Join(busErr, a.biLog.Close())
	}
	if a.eventLog != nil {
		busErr = errors.Join(busErr, a.eventLog.Close())
	}
	if a.hotUpdate != nil && a.hotUpdate.Control() != nil {
		busErr = errors.Join(busErr, a.hotUpdate.Control().Close())
	}
	if a.stateSync != nil {
		busErr = errors.Join(busErr, a.stateSync.Close())
	}
	if a.storageObjects != nil {
		busErr = errors.Join(busErr, a.storageObjects.Close())
	}
	if a.accountCenter != nil {
		busErr = errors.Join(busErr, a.accountCenter.Close())
	}
	if a.lockManager != nil {
		busErr = errors.Join(busErr, a.lockManager.Close())
	}
	if closer, ok := a.presence.(interface{ Close() error }); ok {
		busErr = errors.Join(busErr, closer.Close())
	}
	if a.cache != nil {
		busErr = errors.Join(busErr, a.cache.Close())
	}
	var regErr error
	if a.registry != nil {
		regErr = a.registry.Close()
	}
	return busErr, regErr
}
