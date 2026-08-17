//go:build !windows

package main

import "os"

func replaceFileAtomic(source, destination string) error {
	return os.Rename(source, destination)
}
