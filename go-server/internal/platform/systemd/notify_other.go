//go:build !linux

package systemd

import "context"

func Notify(string) error {
	return nil
}

func Ready() error {
	return nil
}

func Stopping() error {
	return nil
}

func Watchdog() error {
	return nil
}

func StartWatchdog(context.Context, func(error)) func() {
	return func() {}
}
