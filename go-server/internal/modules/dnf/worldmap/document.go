package worldmap

import (
	"errors"
	"fmt"
	"strings"

	dnfpvf "longheng.io/server/internal/modules/dnf/pvf"
)

var ErrMultilineStringNotClosed = errors.New("dnf worldmap multiline string is not closed")

const multilineNewlineMarker = "\x1eLONGHENG_DNF_PVF_NEWLINE\x1e"

func ParseDocument(docPath, text string) (*dnfpvf.Document, error) {
	doc, err := dnfpvf.Parse(docPath, text)
	if err == nil {
		return doc, nil
	}
	normalized, changed, normalizeErr := normalizeMultilineBackticks(text)
	if normalizeErr != nil {
		return nil, normalizeErr
	}
	if !changed {
		return nil, err
	}
	doc, retryErr := dnfpvf.Parse(docPath, normalized)
	if retryErr != nil {
		return nil, fmt.Errorf("parse normalized dnf pvf %q: %w", docPath, retryErr)
	}
	restoreMultilineNewlines(doc)
	return doc, nil
}

func normalizeMultilineBackticks(text string) (string, bool, error) {
	var out strings.Builder
	out.Grow(len(text))
	var quote byte
	inBacktick := false
	inComment := false
	escaped := false
	pendingNewlines := 0
	changed := false

	for pos := 0; pos < len(text); pos++ {
		value := text[pos]
		if inComment {
			out.WriteByte(value)
			if value == '\n' {
				inComment = false
			}
			continue
		}
		if inBacktick {
			switch value {
			case '`':
				out.WriteByte(value)
				for pendingNewlines > 0 {
					out.WriteByte('\n')
					pendingNewlines--
				}
				inBacktick = false
			case '\r':
				if pos+1 < len(text) && text[pos+1] == '\n' {
					continue
				}
				out.WriteString(multilineNewlineMarker)
				pendingNewlines++
				changed = true
			case '\n':
				out.WriteString(multilineNewlineMarker)
				pendingNewlines++
				changed = true
			default:
				out.WriteByte(value)
			}
			continue
		}
		if quote != 0 {
			out.WriteByte(value)
			if escaped {
				escaped = false
				continue
			}
			if value == '\\' {
				escaped = true
				continue
			}
			if value == quote {
				quote = 0
			}
			continue
		}
		if value == '#' || (value == '/' && pos+1 < len(text) && text[pos+1] == '/') {
			inComment = true
			out.WriteByte(value)
			continue
		}
		switch value {
		case '`':
			inBacktick = true
			out.WriteByte(value)
		case '\'', '"':
			quote = value
			out.WriteByte(value)
		default:
			out.WriteByte(value)
		}
	}
	if inBacktick {
		return "", changed, ErrMultilineStringNotClosed
	}
	return out.String(), changed, nil
}

func restoreMultilineNewlines(doc *dnfpvf.Document) {
	if doc == nil {
		return
	}
	for index := range doc.Tokens {
		token := &doc.Tokens[index]
		if token.Kind != dnfpvf.TokenString {
			continue
		}
		token.Value = strings.ReplaceAll(token.Value, multilineNewlineMarker, "\n")
		token.Raw = strings.ReplaceAll(token.Raw, multilineNewlineMarker, "\n")
	}
}
