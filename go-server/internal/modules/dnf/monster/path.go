package monster

import (
	"path"
	"strings"
)

func pathKey(value string) string {
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
	return strings.ToLower(clean)
}
