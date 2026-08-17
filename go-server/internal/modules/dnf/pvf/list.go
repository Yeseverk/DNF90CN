package pvf

// ParseList 从 `.lst` 文档中提取 id 到 PVF 路径的引用。
func ParseList(doc *Document) []ListEntry {
	if doc == nil || len(doc.Tokens) == 0 {
		return nil
	}
	entries := make([]ListEntry, 0)
	for idx := 0; idx+1 < len(doc.Tokens); idx++ {
		id := doc.Tokens[idx]
		if id.Kind != TokenInt {
			continue
		}
		ref := doc.Tokens[idx+1]
		if ref.Kind != TokenString && ref.Kind != TokenIdent {
			continue
		}
		refPath := cleanPath(ref.Value)
		if refPath == "" {
			continue
		}
		entries = append(entries, ListEntry{
			ID:   id.Int,
			Path: refPath,
		})
		idx++
	}
	return entries
}
