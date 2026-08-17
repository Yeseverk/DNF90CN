package main

import (
	"bytes"
	"fmt"
	"os"
)

func installRuntimeBuildVersion(paths projectPaths) error {
	data, err := os.ReadFile(paths.runtimeVersionSource)
	if err != nil {
		return fmt.Errorf("read runtime build version: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return fmt.Errorf("runtime build version is empty: %s", paths.runtimeVersionSource)
	}
	if err := writeFile(paths.runtimeVersion, data, 0o644); err != nil {
		return fmt.Errorf("install runtime build version: %w", err)
	}
	return nil
}

func runtimeBuildVersionCurrent(paths projectPaths) (bool, error) {
	source, err := os.ReadFile(paths.runtimeVersionSource)
	if err != nil {
		return false, fmt.Errorf("read source runtime build version: %w", err)
	}
	if len(bytes.TrimSpace(source)) == 0 {
		return false, fmt.Errorf("runtime build version is empty: %s", paths.runtimeVersionSource)
	}
	installed, err := os.ReadFile(paths.runtimeVersion)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read installed runtime build version: %w", err)
	}
	return bytes.Equal(source, installed), nil
}
