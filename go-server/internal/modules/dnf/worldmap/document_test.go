package worldmap

import (
	"errors"
	"testing"
)

func TestParseDocumentSupportsLocalPVFMultilineBackticks(t *testing.T) {
	doc, err := ParseDocument("dungeon/multiline.dgn", "[greed]\r\n`AAIIJJEE\r\n AABBHHEE` \r\n[map specification]\r\n`map` 0 0 10\r\n")
	if err != nil {
		t.Fatalf("parse multiline local PVF: %v", err)
	}
	greed, ok := doc.Text("greed")
	if !ok || greed != "AAIIJJEE\n AABBHHEE" {
		t.Fatalf("multiline greed = %q, %v", greed, ok)
	}
	if len(doc.Sections) != 2 || doc.Tokens[2].Line != 4 {
		t.Fatalf("section line accounting changed: sections=%+v tokens=%+v", doc.Sections, doc.Tokens)
	}
}

func TestParseDocumentLeavesCommentsAndQuotedBackticksAlone(t *testing.T) {
	text := "# `comment\n// `comment too\n[name]\n\"literal ` value\"\n"
	doc, err := ParseDocument("dungeon/comments.dgn", text)
	if err != nil {
		t.Fatalf("parse comments: %v", err)
	}
	if name, ok := doc.Text("name"); !ok || name != "literal ` value" {
		t.Fatalf("name = %q, %v", name, ok)
	}
}

func TestParseDocumentRejectsTrulyUnclosedBacktick(t *testing.T) {
	_, err := ParseDocument("dungeon/bad.dgn", "[greed]\n`never closed\n")
	if !errors.Is(err, ErrMultilineStringNotClosed) {
		t.Fatalf("expected ErrMultilineStringNotClosed, got %v", err)
	}
}

func TestRawSectionsRetainNestedLocalPVFScope(t *testing.T) {
	doc := parseTestDocument(t, "map/scope.map", "[outer]\n[inner]\n7\n[/inner]\n[/outer]\n")
	sections, diagnostics := rawSections(doc)
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if len(sections) != 4 || !sections[0].Block || !sections[1].Block {
		t.Fatalf("sections = %+v", sections)
	}
	if len(sections[1].Scope) != 1 || sections[1].Scope[0] != "outer" {
		t.Fatalf("inner scope = %+v", sections[1].Scope)
	}
	if len(sections[2].Scope) != 1 || sections[2].Scope[0] != "outer" {
		t.Fatalf("inner closing scope = %+v", sections[2].Scope)
	}
}
