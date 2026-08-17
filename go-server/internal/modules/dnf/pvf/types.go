package pvf

import "errors"

var (
	ErrSourceRequired = errors.New("dnf pvf source is required")
	ErrPathRequired   = errors.New("dnf pvf path is required")
	ErrDocNotFound    = errors.New("dnf pvf document not found")
)

type TokenKind string

const (
	// TokenSection 表示 PVF 文本中的 `[section]` 段落标记。
	TokenSection TokenKind = "section"
	// TokenString 表示反引号、单引号或双引号包裹的文本值。
	TokenString TokenKind = "string"
	// TokenInt 表示整数值。
	TokenInt TokenKind = "int"
	// TokenFloat 表示浮点值。
	TokenFloat TokenKind = "float"
	// TokenIdent 表示未加引号的普通标识符。
	TokenIdent TokenKind = "ident"
	// TokenSymbol 表示结构符号，例如 `{`、`}`、`=`。
	TokenSymbol TokenKind = "symbol"
)

// Token 表示 PVF 文本解析后的最小节点。
type Token struct {
	Kind   TokenKind `json:"kind"`
	Raw    string    `json:"raw"`
	Value  string    `json:"value,omitempty"`
	Int    int64     `json:"int,omitempty"`
	Float  float64   `json:"float,omitempty"`
	Line   int       `json:"line"`
	Column int       `json:"column"`
}

// Section 表示一个 `[section]` 段及其 token 范围。
type Section struct {
	Name  string `json:"name"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// Document 表示单个 PVF 文本文档的解析结果。
type Document struct {
	Path     string    `json:"path"`
	Tokens   []Token   `json:"tokens"`
	Sections []Section `json:"sections"`
}

// ListEntry 表示 `.lst` 中的 id 到 PVF 路径引用。
type ListEntry struct {
	ID   int64  `json:"id"`
	Path string `json:"path"`
}

// Snapshot 表示 DNF PVF 内存索引的简要状态。
type Snapshot struct {
	Documents int `json:"documents"`
	Lists     int `json:"lists"`
	Refs      int `json:"refs"`
}

// Section 返回指定段落内的 token 副本。
func (d *Document) Section(name string) ([]Token, bool) {
	if d == nil {
		return nil, false
	}
	key := sectionKey(name)
	for _, section := range d.Sections {
		if sectionKey(section.Name) != key {
			continue
		}
		if section.Start < 0 || section.End > len(d.Tokens) || section.Start > section.End {
			return nil, false
		}
		out := make([]Token, section.End-section.Start)
		copy(out, d.Tokens[section.Start:section.End])
		return out, true
	}
	return nil, false
}
