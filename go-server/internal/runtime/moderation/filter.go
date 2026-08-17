package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

var (
	ErrInvalidWordRule = errors.New("moderation word rule is invalid")
	ErrInvalidRequest  = errors.New("moderation request is invalid")
)

type Action string

const (
	ActionMask   Action = "mask"
	ActionReject Action = "reject"
)

type WordRule struct {
	ID               string            `json:"id"`
	Word             string            `json:"word"`
	Scope            string            `json:"scope,omitempty"`
	Action           Action            `json:"action,omitempty"`
	Replacement      string            `json:"replacement,omitempty"`
	Severity         int               `json:"severity,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	IgnoreSeparators bool              `json:"ignore_separators,omitempty"`
	WholeWord        bool              `json:"whole_word,omitempty"`
	Meta             map[string]string `json:"meta,omitempty"`
}

type WordSnapshot struct {
	Version   string     `json:"version,omitempty"`
	UpdatedAt time.Time  `json:"updated_at,omitempty"`
	Rules     []WordRule `json:"rules,omitempty"`
}

type FilterSnapshot struct {
	Version   string     `json:"version,omitempty"`
	UpdatedAt time.Time  `json:"updated_at,omitempty"`
	Rules     []WordRule `json:"rules,omitempty"`
}

type Match struct {
	RuleID      string   `json:"rule_id"`
	Word        string   `json:"word"`
	Scope       string   `json:"scope,omitempty"`
	Action      Action   `json:"action"`
	Replacement string   `json:"replacement,omitempty"`
	Severity    int      `json:"severity,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Start       int      `json:"start"`
	End         int      `json:"end"`
}

type TextResult struct {
	Original  string  `json:"original"`
	Sanitized string  `json:"sanitized"`
	Rejected  bool    `json:"rejected"`
	Matches   []Match `json:"matches,omitempty"`
}

type Filter struct {
	mu        sync.RWMutex
	version   string
	updatedAt time.Time
	rules     []compiledRule
}

type compiledRule struct {
	rule       WordRule
	normalized []rune
}

type normalizedRune struct {
	value         rune
	originalIndex int
}

func NewFilter(rules ...WordRule) (*Filter, error) {
	filter := &Filter{}
	if len(rules) > 0 {
		if err := filter.ReplaceRules(context.Background(), WordSnapshot{Rules: rules}); err != nil {
			return nil, err
		}
	}
	return filter, nil
}

func (f *Filter) ReplaceRules(ctx context.Context, snapshot WordSnapshot) error {
	if err := ctxErr(ctx); err != nil {
		return err
	}
	compiled := make([]compiledRule, 0, len(snapshot.Rules))
	seen := make(map[string]struct{}, len(snapshot.Rules))
	for _, rule := range snapshot.Rules {
		item, err := normalizeWordRule(rule)
		if err != nil {
			return err
		}
		if _, exists := seen[item.ID]; exists {
			return fmt.Errorf("%w: duplicate id %q", ErrInvalidWordRule, item.ID)
		}
		seen[item.ID] = struct{}{}
		normalized := normalizeRuleWord(item.Word, item.IgnoreSeparators)
		if len(normalized) == 0 {
			return fmt.Errorf("%w: word %q has no comparable characters", ErrInvalidWordRule, item.ID)
		}
		compiled = append(compiled, compiledRule{rule: item, normalized: normalized})
	}
	sort.Slice(compiled, func(i, j int) bool {
		if compiled[i].rule.Scope != compiled[j].rule.Scope {
			return compiled[i].rule.Scope < compiled[j].rule.Scope
		}
		if len(compiled[i].normalized) != len(compiled[j].normalized) {
			return len(compiled[i].normalized) > len(compiled[j].normalized)
		}
		return compiled[i].rule.ID < compiled[j].rule.ID
	})

	f.mu.Lock()
	f.version = strings.TrimSpace(snapshot.Version)
	f.updatedAt = snapshot.UpdatedAt.UTC()
	if f.updatedAt.IsZero() && len(compiled) > 0 {
		f.updatedAt = time.Now().UTC()
	}
	f.rules = compiled
	f.mu.Unlock()
	return nil
}

func (f *Filter) ApplyJSON(ctx context.Context, data []byte) error {
	if len(strings.TrimSpace(string(data))) == 0 {
		return f.ReplaceRules(ctx, WordSnapshot{})
	}
	var snapshot WordSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return fmt.Errorf("decode moderation word snapshot: %w", err)
	}
	return f.ReplaceRules(ctx, snapshot)
}

func (f *Filter) Evaluate(scope, text string) TextResult {
	result := TextResult{Original: text, Sanitized: text}
	if f == nil || text == "" {
		return result
	}
	scope = normalizeToken(scope)
	runes := []rune(text)
	if len(runes) == 0 {
		return result
	}

	f.mu.RLock()
	rules := append([]compiledRule(nil), f.rules...)
	f.mu.RUnlock()
	if len(rules) == 0 {
		return result
	}

	matches := make([]Match, 0)
	normalizedText := map[bool][]normalizedRune{
		false: normalizeText(runes, false),
		true:  normalizeText(runes, true),
	}
	for _, rule := range rules {
		if !ruleApplies(rule.rule.Scope, scope) {
			continue
		}
		matches = append(matches, matchRule(rule, runes, normalizedText[rule.rule.IgnoreSeparators])...)
	}
	if len(matches) == 0 {
		return result
	}
	sortMatches(matches)

	mask := make([]rune, len(runes))
	for _, match := range matches {
		if match.Action == ActionReject {
			result.Rejected = true
		}
		if match.Action != ActionMask {
			continue
		}
		replacement := replacementRune(match.Replacement)
		for i := match.Start; i <= match.End && i < len(mask); i++ {
			if !unicode.IsSpace(runes[i]) {
				mask[i] = replacement
			}
		}
	}
	for i := range runes {
		if mask[i] != 0 {
			runes[i] = mask[i]
		}
	}
	result.Sanitized = string(runes)
	result.Matches = matches
	return result
}

func (f *Filter) Snapshot() FilterSnapshot {
	if f == nil {
		return FilterSnapshot{}
	}
	f.mu.RLock()
	rules := make([]WordRule, 0, len(f.rules))
	for _, rule := range f.rules {
		rules = append(rules, cloneWordRule(rule.rule))
	}
	version := f.version
	updatedAt := f.updatedAt
	f.mu.RUnlock()
	return FilterSnapshot{Version: version, UpdatedAt: updatedAt, Rules: rules}
}

func normalizeWordRule(rule WordRule) (WordRule, error) {
	rule.ID = normalizeToken(rule.ID)
	rule.Word = strings.TrimSpace(rule.Word)
	rule.Scope = normalizeToken(rule.Scope)
	rule.Replacement = strings.TrimSpace(rule.Replacement)
	rule.Tags = normalizeList(rule.Tags)
	rule.Meta = cloneStringMap(rule.Meta)
	if rule.ID == "" {
		rule.ID = normalizeToken(rule.Word)
	}
	if rule.ID == "" || rule.Word == "" {
		return WordRule{}, fmt.Errorf("%w: id and word are required", ErrInvalidWordRule)
	}
	if rule.Action == "" {
		rule.Action = ActionMask
	}
	if rule.Action != ActionMask && rule.Action != ActionReject {
		return WordRule{}, fmt.Errorf("%w: invalid action %q", ErrInvalidWordRule, rule.Action)
	}
	if rule.Replacement == "" {
		rule.Replacement = "*"
	}
	if rule.Severity < 0 {
		return WordRule{}, fmt.Errorf("%w: severity must be >= 0", ErrInvalidWordRule)
	}
	return rule, nil
}

func matchRule(rule compiledRule, text []rune, normalized []normalizedRune) []Match {
	if len(normalized) == 0 || len(normalized) < len(rule.normalized) {
		return nil
	}
	out := make([]Match, 0)
	for i := 0; i <= len(normalized)-len(rule.normalized); i++ {
		ok := true
		for j := range rule.normalized {
			if normalized[i+j].value != rule.normalized[j] {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		start := normalized[i].originalIndex
		end := normalized[i+len(rule.normalized)-1].originalIndex
		if rule.rule.WholeWord && !hasWordBoundary(text, start, end) {
			continue
		}
		out = append(out, Match{
			RuleID:      rule.rule.ID,
			Word:        rule.rule.Word,
			Scope:       rule.rule.Scope,
			Action:      rule.rule.Action,
			Replacement: rule.rule.Replacement,
			Severity:    rule.rule.Severity,
			Tags:        append([]string(nil), rule.rule.Tags...),
			Start:       start,
			End:         end,
		})
	}
	return out
}

func replacementRune(value string) rune {
	for _, r := range value {
		return r
	}
	return '*'
}

func normalizeRuleWord(word string, ignoreSeparators bool) []rune {
	out := make([]rune, 0, len(word))
	for _, r := range word {
		if ignoreSeparators && isSeparator(r) {
			continue
		}
		out = append(out, unicode.ToLower(r))
	}
	return out
}

func normalizeText(text []rune, ignoreSeparators bool) []normalizedRune {
	out := make([]normalizedRune, 0, len(text))
	for idx, r := range text {
		if ignoreSeparators && isSeparator(r) {
			continue
		}
		out = append(out, normalizedRune{value: unicode.ToLower(r), originalIndex: idx})
	}
	return out
}

func isSeparator(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func hasWordBoundary(text []rune, start, end int) bool {
	if start > 0 && isWordRune(text[start-1]) {
		return false
	}
	if end+1 < len(text) && isWordRune(text[end+1]) {
		return false
	}
	return true
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func sortMatches(matches []Match) {
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Start != matches[j].Start {
			return matches[i].Start < matches[j].Start
		}
		leftLen := matches[i].End - matches[i].Start
		rightLen := matches[j].End - matches[j].Start
		if leftLen != rightLen {
			return leftLen > rightLen
		}
		if matches[i].Severity != matches[j].Severity {
			return matches[i].Severity > matches[j].Severity
		}
		return matches[i].RuleID < matches[j].RuleID
	})
}

func ruleApplies(ruleScope, requestScope string) bool {
	ruleScope = normalizeToken(ruleScope)
	requestScope = normalizeToken(requestScope)
	return ruleScope == "" || ruleScope == "global" || ruleScope == requestScope
}

func normalizeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeToken(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func cloneWordRule(rule WordRule) WordRule {
	rule.Tags = append([]string(nil), rule.Tags...)
	rule.Meta = cloneStringMap(rule.Meta)
	return rule
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
