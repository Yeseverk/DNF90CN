package pvf

import (
	"path"
	"strings"
)

func cleanPath(value string) string {
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
	return strings.ToLower(cleanPath(value))
}

func sectionKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func matchPrefix(filePath string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return false
	}
	key := pathKey(filePath)
	for _, prefix := range prefixes {
		prefixKey := pathKey(prefix)
		if prefixKey == "" {
			continue
		}
		if key == prefixKey || strings.HasPrefix(key, prefixKey+"/") {
			return true
		}
	}
	return false
}
