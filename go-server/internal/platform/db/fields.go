package db

// FieldRegistry 保存字段的合法集合和稳定顺序。
type FieldRegistry[F comparable] struct {
	order []F
	known map[F]struct{}
}

// NewFieldRegistry 创建字段注册表。
func NewFieldRegistry[F comparable](order []F) FieldRegistry[F] {
	registry := FieldRegistry[F]{
		order: append([]F(nil), order...),
		known: make(map[F]struct{}, len(order)),
	}
	for _, field := range order {
		registry.known[field] = struct{}{}
	}
	return registry
}

// All 返回全部字段并保持注册顺序。
func (r FieldRegistry[F]) All() []F {
	return append([]F(nil), r.order...)
}

// IsKnown 判断字段是否已注册。
func (r FieldRegistry[F]) IsKnown(field F) bool {
	_, ok := r.known[field]
	return ok
}

// Normalize 去重、过滤未知字段，并按注册顺序返回。
func (r FieldRegistry[F]) Normalize(fields []F) []F {
	if len(fields) == 0 {
		return nil
	}
	seen := make(map[F]struct{}, len(fields))
	for _, field := range fields {
		if r.IsKnown(field) {
			seen[field] = struct{}{}
		}
	}
	out := make([]F, 0, len(seen))
	for _, field := range r.order {
		if _, ok := seen[field]; ok {
			out = append(out, field)
		}
	}
	return out
}

// Merge 合并两组字段并做归一化。
func (r FieldRegistry[F]) Merge(left, right []F) []F {
	if len(left) == 0 {
		return r.Normalize(right)
	}
	if len(right) == 0 {
		return r.Normalize(left)
	}
	merged := make([]F, 0, len(left)+len(right))
	merged = append(merged, left...)
	merged = append(merged, right...)
	return r.Normalize(merged)
}

// NewSet 创建字段集合。
func (r FieldRegistry[F]) NewSet(fields ...F) FieldSet[F] {
	set := FieldSet[F]{
		registry: r,
		values:   make(map[F]struct{}),
	}
	set.Add(fields...)
	return set
}

// AllSet 创建包含所有字段的集合。
func (r FieldRegistry[F]) AllSet() FieldSet[F] {
	set := r.NewSet()
	set.AddAll()
	return set
}

// FieldSet 表示一组已注册字段。
type FieldSet[F comparable] struct {
	registry FieldRegistry[F]
	values   map[F]struct{}
}

// IsZero 判断字段集合是否为空。
func (s FieldSet[F]) IsZero() bool {
	return len(s.values) == 0
}

// Add 向集合加入字段并忽略未知字段。
func (s FieldSet[F]) Add(fields ...F) {
	if s.values == nil {
		return
	}
	for _, field := range fields {
		if s.registry.IsKnown(field) {
			s.values[field] = struct{}{}
		}
	}
}

// AddAll 把注册表里的所有字段加入集合。
func (s FieldSet[F]) AddAll() {
	if s.values == nil {
		return
	}
	for _, field := range s.registry.order {
		s.values[field] = struct{}{}
	}
}

// Merge 合并另一个字段集合。
func (s FieldSet[F]) Merge(other FieldSet[F]) {
	if s.values == nil {
		return
	}
	for field := range other.values {
		if s.registry.IsKnown(field) {
			s.values[field] = struct{}{}
		}
	}
}

// List 按注册顺序返回字段列表。
func (s FieldSet[F]) List() []F {
	if len(s.values) == 0 {
		return nil
	}
	out := make([]F, 0, len(s.values))
	for _, field := range s.registry.order {
		if _, ok := s.values[field]; ok {
			out = append(out, field)
		}
	}
	return out
}

// Clone 拷贝字段集合。
func (s FieldSet[F]) Clone() FieldSet[F] {
	if len(s.values) == 0 {
		return FieldSet[F]{registry: s.registry}
	}
	out := FieldSet[F]{
		registry: s.registry,
		values:   make(map[F]struct{}, len(s.values)),
	}
	for field := range s.values {
		out.values[field] = struct{}{}
	}
	return out
}
