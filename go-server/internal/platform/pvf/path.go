package pvf

import (
	"path"
	"strings"
)

func normalizePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	for strings.HasPrefix(value, "./") || strings.HasPrefix(value, "/") {
		value = strings.TrimPrefix(value, "./")
		value = strings.TrimPrefix(value, "/")
	}
	if value == "" {
		return ""
	}
	clean := strings.TrimSuffix(path.Clean(value), "/")
	if clean == "." {
		return ""
	}
	return clean
}

func pathKey(value string) string {
	return strings.ToLower(normalizePath(value))
}

func joinArchivePath(dir, name string) string {
	if strings.TrimSpace(dir) == "" {
		return normalizePath(name)
	}
	if strings.TrimSpace(name) == "" {
		return normalizePath(dir)
	}
	return normalizePath(dir + "/" + name)
}
