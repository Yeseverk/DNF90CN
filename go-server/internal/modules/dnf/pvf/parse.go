package pvf

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Parse 把单个 PVF 文本文档解析成通用 token 和 section。
func Parse(docPath, text string) (*Document, error) {
	docPath = cleanPath(docPath)
	if docPath == "" {
		return nil, ErrPathRequired
	}
	parser := docParser{
		doc: Document{Path: docPath},
	}
	lines := strings.Split(text, "\n")
	for idx, line := range lines {
		if err := parser.parseLine(idx+1, line); err != nil {
			return nil, err
		}
	}
	if parser.pending != nil {
		return nil, fmt.Errorf("dnf pvf string at %d:%d is not closed", parser.pending.line, parser.pending.column)
	}
	parser.closeSection()
	return &parser.doc, nil
}

type docParser struct {
	doc     Document
	pending *pendingString
}

type pendingString struct {
	quote  byte
	line   int
	column int
	raw    strings.Builder
	value  strings.Builder
}

func (p *docParser) parseLine(lineNo int, line string) error {
	if p.pending != nil {
		return p.continueString(lineNo, line)
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		if end := strings.IndexByte(trimmed, ']'); end > 0 {
			column := strings.Index(line, "[") + 1
			name := strings.TrimSpace(trimmed[1:end])
			p.addToken(Token{
				Kind:   TokenSection,
				Raw:    trimmed[:end+1],
				Value:  name,
				Line:   lineNo,
				Column: column,
			})
			return p.scanValues(lineNo, column+end+1, trimmed[end+1:])
		}
	}
	column := len(line) - len(strings.TrimLeftFunc(line, unicode.IsSpace)) + 1
	return p.scanValues(lineNo, column, trimmed)
}

func (p *docParser) addToken(token Token) {
	if token.Kind == TokenSection {
		p.closeSection()
		p.doc.Sections = append(p.doc.Sections, Section{
			Name:  token.Value,
			Start: len(p.doc.Tokens) + 1,
		})
	}
	p.doc.Tokens = append(p.doc.Tokens, token)
}

func (p *docParser) closeSection() {
	if len(p.doc.Sections) == 0 {
		return
	}
	idx := len(p.doc.Sections) - 1
	if p.doc.Sections[idx].End == 0 {
		p.doc.Sections[idx].End = len(p.doc.Tokens)
	}
}

func (p *docParser) scanValues(lineNo, baseColumn int, line string) error {
	for pos := 0; pos < len(line); {
		if isSpace(line[pos]) {
			pos++
			continue
		}
		if pos+1 < len(line) && line[pos] == '/' && line[pos+1] == '/' {
			return nil
		}
		if line[pos] == '#' {
			return nil
		}
		column := baseColumn + pos
		switch line[pos] {
		case '`', '\'', '"':
			token, next, complete := p.beginString(lineNo, column, line, pos)
			if !complete {
				return nil
			}
			p.addToken(token)
			pos = next
		case '{', '}', '=', ',', ':':
			p.addToken(Token{Kind: TokenSymbol, Raw: line[pos : pos+1], Value: line[pos : pos+1], Line: lineNo, Column: column})
			pos++
		default:
			raw, next := scanBare(line, pos)
			if raw == "" {
				pos++
				continue
			}
			p.addToken(parseBare(lineNo, column, raw))
			pos = next
		}
	}
	return nil
}

func (p *docParser) beginString(lineNo, column int, line string, start int) (Token, int, bool) {
	pending := &pendingString{quote: line[start], line: lineNo, column: column}
	pending.raw.WriteByte(line[start])
	next, complete := appendStringSegment(pending, line, start+1)
	if !complete {
		p.pending = pending
		return Token{}, 0, false
	}
	return pending.token(), next, true
}

func (p *docParser) continueString(lineNo int, line string) error {
	p.pending.raw.WriteByte('\n')
	p.pending.value.WriteByte('\n')
	next, complete := appendStringSegment(p.pending, line, 0)
	if !complete {
		return nil
	}
	token := p.pending.token()
	p.pending = nil
	p.addToken(token)
	return p.scanValues(lineNo, next+1, line[next:])
}

func appendStringSegment(pending *pendingString, line string, start int) (int, bool) {
	for pos := start; pos < len(line); pos++ {
		value := line[pos]
		pending.raw.WriteByte(value)
		if value == pending.quote {
			return pos + 1, true
		}
		if pending.quote != '`' && value == '\\' && pos+1 < len(line) {
			pos++
			pending.raw.WriteByte(line[pos])
			pending.value.WriteByte(line[pos])
			continue
		}
		pending.value.WriteByte(value)
	}
	return len(line), false
}

func (p *pendingString) token() Token {
	return Token{
		Kind:   TokenString,
		Raw:    p.raw.String(),
		Value:  p.value.String(),
		Line:   p.line,
		Column: p.column,
	}
}

func scanBare(line string, start int) (string, int) {
	pos := start
	for pos < len(line) {
		if isSpace(line[pos]) || strings.ContainsRune("{},:=", rune(line[pos])) {
			break
		}
		if pos+1 < len(line) && line[pos] == '/' && line[pos+1] == '/' {
			break
		}
		pos++
	}
	return line[start:pos], pos
}

func parseBare(lineNo, column int, raw string) Token {
	if value, ok := parseInt(raw); ok {
		return Token{Kind: TokenInt, Raw: raw, Value: raw, Int: value, Line: lineNo, Column: column}
	}
	if value, ok := parseFloat(raw); ok {
		return Token{Kind: TokenFloat, Raw: raw, Value: raw, Float: value, Line: lineNo, Column: column}
	}
	return Token{Kind: TokenIdent, Raw: raw, Value: raw, Line: lineNo, Column: column}
}

func parseInt(raw string) (int64, bool) {
	if strings.ContainsAny(raw, ".eE") {
		return 0, false
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	return value, err == nil
}

func parseFloat(raw string) (float64, bool) {
	if !strings.ContainsAny(raw, ".eE") {
		return 0, false
	}
	value, err := strconv.ParseFloat(raw, 64)
	return value, err == nil
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r'
}
