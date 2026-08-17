//go:build !windows

package main

import "context"

func acquireControllerLock(context.Context, string) (func(), error) {
	return func() {}, nil
}
