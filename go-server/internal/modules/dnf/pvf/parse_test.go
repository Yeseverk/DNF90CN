package pvf

import (
	"context"
	"errors"
	"testing"

	platformpvf "longheng.io/server/internal/platform/pvf"
)

func TestParseSectionsAndListEntries(t *testing.T) {
	doc, err := Parse("equipment/equipment.lst", `
[equipment]
1001 `+"`equipment/weapon/sword.equ`"+`
1002 `+"`equipment/armor/coat.equ`"+`

[metadata]
rarity 3 1.5
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(doc.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %+v", doc.Sections)
	}
	tokens, ok := doc.Section("equipment")
	if !ok || len(tokens) != 4 {
		t.Fatalf("expected equipment section tokens, got ok=%v tokens=%+v", ok, tokens)
	}
	entries := ParseList(doc)
	if len(entries) != 2 {
		t.Fatalf("expected list entries, got %+v", entries)
	}
	if entries[0].ID != 1001 || entries[0].Path != "equipment/weapon/sword.equ" {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	if _, err := Parse("", ""); !errors.Is(err, ErrPathRequired) {
		t.Fatalf("expected ErrPathRequired, got %v", err)
	}
	_, err := Parse("bad.lst", "`missing")
	if err == nil {
		t.Fatalf("expected unclosed string error")
	}
}

func TestDocumentValueHelpers(t *testing.T) {
	doc, err := Parse("dungeon/test.dgn", `
[monsters]
1001 1002 `+"`monster/boss.mob`"+`
[maps]
`+"`map/a.map` `map/b.map`"+`
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := doc.Ints("monsters"); len(got) != 2 || got[0] != 1001 || got[1] != 1002 {
		t.Fatalf("unexpected ints: %+v", got)
	}
	if got := doc.Texts("maps"); len(got) != 2 || got[0] != "map/a.map" || got[1] != "map/b.map" {
		t.Fatalf("unexpected texts: %+v", got)
	}
}

func TestParseMultilineBacktickString(t *testing.T) {
	doc, err := Parse("skill/test.skl", "[explain]\n`first line\nsecond line`\n[required level]\n5\n")
	if err != nil {
		t.Fatalf("parse multiline string: %v", err)
	}
	value, ok := doc.Text("explain")
	if !ok || value != "first line\nsecond line" {
		t.Fatalf("multiline value = %q ok=%t", value, ok)
	}
	level, ok := doc.Int("required level")
	if !ok || level != 5 {
		t.Fatalf("required level = %d ok=%t", level, ok)
	}
}

func TestBuildIndexFromMemorySource(t *testing.T) {
	source := &memSource{
		texts: map[string]string{
			"equipment/equipment.lst": "1001 `equipment/weapon/sword.equ`\n",
			"equipment/weapon/sword.equ": `
[name]
` + "`short sword`" + `
[attack]
12
`,
			"skill/skill.lst": "2001 `skill/slash.skl`\n",
		},
		files: []platformpvf.File{
			{ArchivePath: "equipment/equipment.lst"},
			{ArchivePath: "equipment/weapon/sword.equ"},
			{ArchivePath: "skill/skill.lst"},
		},
		reads: make(map[string]int),
	}
	index, err := Build(context.Background(), source, BuildOptions{
		Lists: []string{"equipment/equipment.lst"},
	})
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	if snapshot := index.Snapshot(); snapshot.Documents != 2 || snapshot.Lists != 1 || snapshot.Refs != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	path, ok := index.Resolve("equipment/equipment.lst", 1001)
	if !ok || path != "equipment/weapon/sword.equ" {
		t.Fatalf("unexpected resolved path: %q ok=%v", path, ok)
	}
	doc, ok := index.Document("./EQUIPMENT/weapon/sword.equ")
	if !ok {
		t.Fatalf("expected indexed sword document")
	}
	if _, ok := doc.Section("attack"); !ok {
		t.Fatalf("expected attack section")
	}
	before := source.reads["equipment/weapon/sword.equ"]
	if _, ok := index.Document("equipment/weapon/sword.equ"); !ok {
		t.Fatalf("expected cached document")
	}
	if source.reads["equipment/weapon/sword.equ"] != before {
		t.Fatalf("document lookup should not read source again")
	}
}

func TestBuildIndexResolvesListRelativePaths(t *testing.T) {
	source := &memSource{
		texts: map[string]string{
			"equipment/equipment.lst": "1001 `weapon/sword.equ`\n",
			"equipment/weapon/sword.equ": `
[name]
` + "`relative sword`" + `
`,
		},
		files: []platformpvf.File{
			{ArchivePath: "equipment/equipment.lst"},
			{ArchivePath: "equipment/weapon/sword.equ"},
		},
		reads: make(map[string]int),
	}
	index, err := Build(context.Background(), source, BuildOptions{
		Lists: []string{"equipment/equipment.lst"},
	})
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	resolved, ok := index.Resolve("equipment/equipment.lst", 1001)
	if !ok || resolved != "equipment/weapon/sword.equ" {
		t.Fatalf("resolved = %q ok=%v", resolved, ok)
	}
	if _, ok := index.Document("equipment/weapon/sword.equ"); !ok {
		t.Fatalf("expected relative document to be indexed")
	}
}

func TestBuildIndexLoadsNestedLists(t *testing.T) {
	source := &memSource{
		texts: map[string]string{
			"skill/skilllist.lst":     "0 `SwordmanSkill.lst`\n",
			"skill/SwordmanSkill.lst": "46 `Swordman/UpperSlash.skl`\n",
			"skill/Swordman/UpperSlash.skl": `
[name]
` + "`upper slash`" + `
`,
		},
		files: []platformpvf.File{
			{ArchivePath: "skill/skilllist.lst"},
			{ArchivePath: "skill/SwordmanSkill.lst"},
			{ArchivePath: "skill/Swordman/UpperSlash.skl"},
		},
		reads: make(map[string]int),
	}
	index, err := Build(context.Background(), source, BuildOptions{Lists: []string{"skill/skilllist.lst"}})
	if err != nil {
		t.Fatalf("build nested index: %v", err)
	}
	if snapshot := index.Snapshot(); snapshot.Documents != 3 || snapshot.Lists != 2 || snapshot.Refs != 2 {
		t.Fatalf("unexpected nested snapshot: %+v", snapshot)
	}
	jobList, ok := index.Resolve("skill/skilllist.lst", 0)
	if !ok || jobList != "skill/SwordmanSkill.lst" {
		t.Fatalf("job list = %q ok=%t", jobList, ok)
	}
	skillPath, ok := index.Resolve(jobList, 46)
	if !ok || skillPath != "skill/Swordman/UpperSlash.skl" {
		t.Fatalf("skill path = %q ok=%t", skillPath, ok)
	}
	if _, ok := index.Document(skillPath); !ok {
		t.Fatal("nested skill document was not loaded")
	}
}

func TestBuildIndexRequiresSource(t *testing.T) {
	_, err := Build(context.Background(), nil, BuildOptions{})
	if !errors.Is(err, ErrSourceRequired) {
		t.Fatalf("expected ErrSourceRequired, got %v", err)
	}
}

func TestBuildIndexHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Build(ctx, &memSource{
		texts: map[string]string{"a.lst": "1 `a/a.equ`"},
		reads: make(map[string]int),
	}, BuildOptions{Paths: []string{"a.lst"}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}

type memSource struct {
	texts map[string]string
	files []platformpvf.File
	reads map[string]int
}

func (s *memSource) ReadText(relativePath string) (string, error) {
	key := pathKey(relativePath)
	for path, text := range s.texts {
		if pathKey(path) == key {
			s.reads[cleanPath(path)]++
			return text, nil
		}
	}
	return "", platformpvf.ErrFileNotFound
}

func (s *memSource) Files() []platformpvf.File {
	return append([]platformpvf.File(nil), s.files...)
}
