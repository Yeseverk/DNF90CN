package config

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
)

func requireString(errs *[]error, field, value string) {
	if strings.TrimSpace(value) == "" {
		*errs = append(*errs, fmt.Errorf("%s is required", field))
	}
}

func isAsyncSaveMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "async", "writebehind", "write_behind":
		return true
	default:
		return false
	}
}

func requireSafeName(errs *[]error, field, value string) {
	requireString(errs, field, value)
	if strings.TrimSpace(value) == "" {
		return
	}
	for _, r := range value {
		if isSafeNameRune(r) {
			continue
		}
		*errs = append(*errs, fmt.Errorf("%s %q can only contain letters, digits, dot, dash, or underscore", field, value))
		return
	}
	if strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		*errs = append(*errs, fmt.Errorf("%s %q cannot start with dot, end with dot, or contain consecutive dots", field, value))
	}
}

func requireSQLIdentifier(errs *[]error, field, value string) {
	requireString(errs, field, value)
	if strings.TrimSpace(value) == "" {
		return
	}
	for idx, r := range value {
		if idx == 0 {
			if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				continue
			}
			*errs = append(*errs, fmt.Errorf("%s %q must start with a letter or underscore", field, value))
			return
		}
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		*errs = append(*errs, fmt.Errorf("%s %q can only contain letters, digits, or underscore", field, value))
		return
	}
}

func isSafeNameRune(r rune) bool {
	return r == '.' || r == '-' || r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

func requireNonEmptySlice(errs *[]error, field string, values []string) {
	if len(values) == 0 {
		*errs = append(*errs, fmt.Errorf("%s requires at least one value", field))
		return
	}
	for idx, value := range values {
		if strings.TrimSpace(value) == "" {
			*errs = append(*errs, fmt.Errorf("%s[%d] is required", field, idx))
		}
	}
}

func requireOneOf(errs *[]error, field, value string, allowed ...string) {
	if !slices.Contains(allowed, value) {
		*errs = append(*errs, fmt.Errorf("%s %q must be one of %s", field, value, strings.Join(allowed, ", ")))
	}
}

func requirePositiveInt(errs *[]error, field string, value int) {
	if value <= 0 {
		*errs = append(*errs, fmt.Errorf("%s must be positive", field))
	}
}

func requirePositiveInt32(errs *[]error, field string, value int32) {
	if value <= 0 {
		*errs = append(*errs, fmt.Errorf("%s must be positive", field))
	}
}

func requirePositiveInt64(errs *[]error, field string, value int64) {
	if value <= 0 {
		*errs = append(*errs, fmt.Errorf("%s must be positive", field))
	}
}

func requireNonNegInt(errs *[]error, field string, value int) {
	if value < 0 {
		*errs = append(*errs, fmt.Errorf("%s must be non-negative", field))
	}
}

func requireNonNegInt64(errs *[]error, field string, value int64) {
	if value < 0 {
		*errs = append(*errs, fmt.Errorf("%s must be non-negative", field))
	}
}

func requireListenAddress(errs *[]error, field, value string) {
	requireString(errs, field, value)
	if strings.TrimSpace(value) == "" {
		return
	}
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s %q must be host:port or :port: %w", field, value, err))
		return
	}
	if strings.TrimSpace(port) == "" {
		*errs = append(*errs, fmt.Errorf("%s %q must include a port", field, value))
		return
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		*errs = append(*errs, fmt.Errorf("%s %q has invalid port %q: %w", field, value, port, err))
	}
}

func validRateRules(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, ",")
		if len(fields) != 3 {
			return fmt.Errorf("rate_limit.rules %q must use path,window_seconds,max_requests", part)
		}
		if strings.TrimSpace(fields[0]) == "" {
			return fmt.Errorf("rate_limit.rules %q has empty path", part)
		}
		window, err := strconv.Atoi(strings.TrimSpace(fields[1]))
		if err != nil || window <= 0 {
			return fmt.Errorf("rate_limit.rules %q has invalid window_seconds", part)
		}
		max, err := strconv.Atoi(strings.TrimSpace(fields[2]))
		if err != nil || max <= 0 {
			return fmt.Errorf("rate_limit.rules %q has invalid max_requests", part)
		}
	}
	return nil
}
