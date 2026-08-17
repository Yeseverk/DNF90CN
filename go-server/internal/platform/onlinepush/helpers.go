package onlinepush

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func nowUTC(now func() time.Time) time.Time {
	if now == nil {
		return time.Now().UTC()
	}
	return now().UTC()
}

func hashID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(strings.TrimSpace(part)))
		h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	return prefix + hex.EncodeToString(sum[:8])
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneReceipt(in Receipt) Receipt {
	in.Errors = append([]string(nil), in.Errors...)
	return in
}

func cloneOffline(in OfflineMessage) OfflineMessage {
	in.Body = append([]byte(nil), in.Body...)
	in.Metadata = cloneStringMap(in.Metadata)
	return in
}

func uniqueStrings(values ...[]string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, batch := range values {
		for _, value := range batch {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}
