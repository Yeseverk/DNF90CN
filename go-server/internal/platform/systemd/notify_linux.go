//go:build linux

package systemd

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

func Notify(state string) error {
	addr := os.Getenv("NOTIFY_SOCKET")
	if strings.TrimSpace(addr) == "" || state == "" {
		return nil
	}
	if strings.HasPrefix(addr, "@") {
		addr = "\x00" + addr[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(state))
	return err
}

func Ready() error {
	return Notify("READY=1")
}

func Stopping() error {
	return Notify("STOPPING=1")
}

func Watchdog() error {
	return Notify("WATCHDOG=1")
}

func StartWatchdog(ctx context.Context, onError func(error)) func() {
	interval := watchdogInterval()
	if interval <= 0 {
		return func() {}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		notifyWatchdog(onError)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				notifyWatchdog(onError)
			}
		}
	}()
	return cancel
}

func watchdogInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("WATCHDOG_USEC"))
	if raw == "" {
		return 0
	}
	usec, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || usec <= 0 {
		return 0
	}
	interval := time.Duration(usec) * time.Microsecond / 2
	if interval <= 0 {
		return time.Second
	}
	return interval
}

func notifyWatchdog(onError func(error)) {
	if err := Watchdog(); err != nil && onError != nil {
		onError(err)
	}
}
