package pvf

// Text 返回指定 section 的第一个文本或标识符值。
func (d *Document) Text(name string) (string, bool) {
	values := d.Texts(name)
	if len(values) == 0 {
		return "", false
	}
	return values[0], true
}

// Int 返回指定 section 的第一个整数值。
func (d *Document) Int(name string) (int64, bool) {
	values := d.Ints(name)
	if len(values) == 0 {
		return 0, false
	}
	return values[0], true
}

// Number 返回指定 section 的第一个数字值。
func (d *Document) Number(name string) (float64, bool) {
	values := d.Numbers(name)
	if len(values) == 0 {
		return 0, false
	}
	return values[0], true
}

// Texts 返回指定 section 内所有文本和标识符值。
func (d *Document) Texts(name string) []string {
	tokens, ok := d.Section(name)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind == TokenString || token.Kind == TokenIdent {
			out = append(out, token.Value)
		}
	}
	return out
}

// Ints 返回指定 section 内所有整数值。
func (d *Document) Ints(name string) []int64 {
	tokens, ok := d.Section(name)
	if !ok {
		return nil
	}
	out := make([]int64, 0, len(tokens))
	for _, token := range tokens {
		if token.Kind == TokenInt {
			out = append(out, token.Int)
		}
	}
	return out
}

// Numbers 返回指定 section 内所有数字值。
func (d *Document) Numbers(name string) []float64 {
	tokens, ok := d.Section(name)
	if !ok {
		return nil
	}
	out := make([]float64, 0, len(tokens))
	for _, token := range tokens {
		switch token.Kind {
		case TokenInt:
			out = append(out, float64(token.Int))
		case TokenFloat:
			out = append(out, token.Float)
		}
	}
	return out
}
