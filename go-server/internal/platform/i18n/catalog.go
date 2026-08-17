package i18n

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var (
	ErrDirectoryRequired = errors.New("i18n directory is required")
	ErrLanguageRequired  = errors.New("i18n language is required")
	ErrKeyRequired       = errors.New("i18n key is required")
)

type Snapshot struct {
	DefaultLanguage string             `json:"default_language"`
	Version         string             `json:"version"`
	Languages       []LanguageSnapshot `json:"languages"`
}

type LanguageSnapshot struct {
	Language string `json:"language"`
	Messages int    `json:"messages"`
}

type Catalog struct {
	defaultLanguage string
	version         string

	mu       sync.RWMutex
	messages map[string]map[string]string
}

func NewCatalog(defaultLanguage, version string) *Catalog {
	defaultLanguage = normalizeLanguage(defaultLanguage)
	if defaultLanguage == "" {
		defaultLanguage = "en-us"
	}
	return &Catalog{
		defaultLanguage: defaultLanguage,
		version:         strings.TrimSpace(version),
		messages:        make(map[string]map[string]string),
	}
}

func (c *Catalog) LoadDir(ctx context.Context, root string) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c.mu.RLock()
	defaultLanguage := c.defaultLanguage
	version := c.version
	c.mu.RUnlock()
	return c.LoadDirWithConfig(ctx, root, defaultLanguage, version)
}

func (c *Catalog) LoadDirWithConfig(ctx context.Context, root, defaultLanguage, version string) error {
	if c == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return ErrDirectoryRequired
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("stat i18n directory %q: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("i18n directory %q is not a directory", root)
	}

	next := make(map[string]map[string]string)
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if entry.IsDir() || !isCatalogFile(entry.Name()) {
			return nil
		}
		language := normalizeLanguage(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		if language == "" {
			return ErrLanguageRequired
		}
		if _, exists := next[language]; exists {
			return fmt.Errorf("duplicate i18n catalog for language %q", language)
		}
		messages, err := loadCatalogFile(path)
		if err != nil {
			return err
		}
		next[language] = messages
		return nil
	}); err != nil {
		return err
	}
	defaultLanguage = normalizeLanguage(defaultLanguage)
	if defaultLanguage == "" {
		defaultLanguage = "en-us"
	}
	if _, ok := matchSupported(defaultLanguage, supportedLanguages(next), defaultLanguage); !ok {
		return fmt.Errorf("default language %q is not loaded", defaultLanguage)
	}

	c.mu.Lock()
	c.defaultLanguage = defaultLanguage
	c.version = strings.TrimSpace(version)
	c.messages = next
	c.mu.Unlock()
	return nil
}

func (c *Catalog) Configure(defaultLanguage, version string) {
	if c == nil {
		return
	}
	defaultLanguage = normalizeLanguage(defaultLanguage)
	if defaultLanguage == "" {
		defaultLanguage = "en-us"
	}
	c.mu.Lock()
	c.defaultLanguage = defaultLanguage
	c.version = strings.TrimSpace(version)
	c.mu.Unlock()
}

func (c *Catalog) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.messages = make(map[string]map[string]string)
	c.mu.Unlock()
}

func (c *Catalog) Translate(language, key string, fields map[string]string) (string, bool) {
	if c == nil {
		return key, false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false
	}
	language = c.resolveLanguage(language)
	c.mu.RLock()
	message, ok := c.messages[language][key]
	if !ok && language != c.defaultLanguage {
		message, ok = c.messages[c.defaultLanguage][key]
	}
	c.mu.RUnlock()
	if !ok {
		return key, false
	}
	return render(message, fields), true
}

func (c *Catalog) MustTranslate(language, key string, fields map[string]string) string {
	message, _ := c.Translate(language, key, fields)
	return message
}

func (c *Catalog) HasLanguage(language string) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	_, ok := matchSupported(language, supportedLanguages(c.messages), c.defaultLanguage)
	c.mu.RUnlock()
	return ok
}

func (c *Catalog) Negotiate(acceptLanguage string) string {
	if c == nil {
		return ""
	}
	c.mu.RLock()
	supported := make(map[string]struct{}, len(c.messages))
	for language := range c.messages {
		supported[language] = struct{}{}
	}
	defaultLanguage := c.defaultLanguage
	c.mu.RUnlock()

	for _, candidate := range parseAcceptLanguage(acceptLanguage) {
		if matched, ok := matchSupported(candidate, supported, defaultLanguage); ok {
			return matched
		}
	}
	if _, ok := supported[defaultLanguage]; ok {
		return defaultLanguage
	}
	return defaultLanguage
}

func (c *Catalog) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	c.mu.RLock()
	languages := make([]LanguageSnapshot, 0, len(c.messages))
	for language, messages := range c.messages {
		languages = append(languages, LanguageSnapshot{Language: language, Messages: len(messages)})
	}
	snapshot := Snapshot{
		DefaultLanguage: c.defaultLanguage,
		Version:         c.version,
		Languages:       languages,
	}
	c.mu.RUnlock()
	sort.Slice(snapshot.Languages, func(i, j int) bool {
		return snapshot.Languages[i].Language < snapshot.Languages[j].Language
	})
	return snapshot
}

func (c *Catalog) resolveLanguage(language string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if matched, ok := matchSupported(language, supportedLanguages(c.messages), c.defaultLanguage); ok {
		return matched
	}
	return c.defaultLanguage
}

func loadCatalogFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if err != nil {
		return nil, fmt.Errorf("read i18n catalog %q: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	messages := make(map[string]string)
	if err := decoder.Decode(&messages); err != nil {
		return nil, fmt.Errorf("parse i18n catalog %q: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("parse i18n catalog %q: catalog contains more than one JSON value", path)
		}
		return nil, fmt.Errorf("parse i18n catalog %q: %w", path, err)
	}
	out := make(map[string]string, len(messages))
	for key, value := range messages {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("%w in %q", ErrKeyRequired, path)
		}
		out[key] = value
	}
	return out, nil
}

func render(message string, fields map[string]string) string {
	for key, value := range fields {
		token := "{{" + strings.TrimSpace(key) + "}}"
		message = strings.ReplaceAll(message, token, value)
	}
	return message
}

func parseAcceptLanguage(raw string) []string {
	type candidate struct {
		language string
		q        float64
		index    int
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	candidates := make([]candidate, 0, len(parts))
	for idx, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		q := 1.0
		fields := strings.Split(part, ";")
		language := normalizeLanguage(fields[0])
		if language == "" || language == "*" {
			continue
		}
		for _, field := range fields[1:] {
			field = strings.TrimSpace(field)
			if !strings.HasPrefix(field, "q=") {
				continue
			}
			parsed, err := strconv.ParseFloat(strings.TrimPrefix(field, "q="), 64)
			if err == nil && parsed >= 0 {
				q = parsed
			}
		}
		candidates = append(candidates, candidate{language: language, q: q, index: idx})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].q != candidates[j].q {
			return candidates[i].q > candidates[j].q
		}
		return candidates[i].index < candidates[j].index
	})
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.q <= 0 {
			continue
		}
		out = append(out, candidate.language)
	}
	return out
}

func normalizeLanguage(language string) string {
	language = strings.TrimSpace(language)
	language = strings.ReplaceAll(language, "_", "-")
	return strings.ToLower(language)
}

func baseLanguage(language string) string {
	language = normalizeLanguage(language)
	if idx := strings.IndexByte(language, '-'); idx > 0 {
		return language[:idx]
	}
	return ""
}

func supportedLanguages(messages map[string]map[string]string) map[string]struct{} {
	supported := make(map[string]struct{}, len(messages))
	for language := range messages {
		supported[language] = struct{}{}
	}
	return supported
}

func matchSupported(language string, supported map[string]struct{}, preferred string) (string, bool) {
	language = normalizeLanguage(language)
	preferred = normalizeLanguage(preferred)
	if language == "" {
		if _, ok := supported[preferred]; ok {
			return preferred, true
		}
		return "", false
	}
	if _, ok := supported[language]; ok {
		return language, true
	}
	base := baseLanguage(language)
	if base == "" {
		base = language
	}
	if _, ok := supported[base]; ok {
		return base, true
	}
	if languageMatches(preferred, base) {
		if _, ok := supported[preferred]; ok {
			return preferred, true
		}
	}
	candidates := make([]string, 0, len(supported))
	for candidate := range supported {
		if languageMatches(candidate, base) {
			candidates = append(candidates, candidate)
		}
	}
	sort.Strings(candidates)
	if len(candidates) == 0 {
		return "", false
	}
	return candidates[0], true
}

func languageMatches(language, base string) bool {
	language = normalizeLanguage(language)
	base = normalizeLanguage(base)
	return language == base || strings.HasPrefix(language, base+"-")
}

func isCatalogFile(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == ".DS_Store" || strings.HasPrefix(name, "._") {
		return false
	}
	return strings.EqualFold(filepath.Ext(name), ".json")
}
