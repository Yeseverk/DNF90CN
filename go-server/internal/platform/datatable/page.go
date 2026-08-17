package datatable

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const DefaultPageLimit = 100

type PageOptions struct {
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type Page[T any] struct {
	Table      Table    `json:"table"`
	Manifest   Manifest `json:"manifest"`
	Rows       []T      `json:"rows,omitempty"`
	Offset     int      `json:"offset"`
	Limit      int      `json:"limit"`
	Total      int      `json:"total"`
	NextCursor string   `json:"next_cursor,omitempty"`
	HasMore    bool     `json:"has_more"`
}

func LoadRowsPage[T any](ctx context.Context, source RowSource, name string, options PageOptions) (Page[T], error) {
	if err := contextErr(ctx); err != nil {
		return Page[T]{}, err
	}
	rows, err := LoadRows[T](source, name)
	if err != nil {
		return Page[T]{}, err
	}
	return PageRows(rows, options)
}

func PageRows[T any](set RowSet[T], options PageOptions) (Page[T], error) {
	offset, limit, err := normalizePageOptions(options, len(set.Rows))
	if err != nil {
		return Page[T]{}, err
	}
	end := offset + limit
	if end > len(set.Rows) {
		end = len(set.Rows)
	}
	pageRows := append([]T(nil), set.Rows[offset:end]...)
	next := ""
	hasMore := end < len(set.Rows)
	if hasMore {
		next = EncodePageCursor(end)
	}
	return Page[T]{
		Table:      set.Table,
		Manifest:   set.Manifest,
		Rows:       pageRows,
		Offset:     offset,
		Limit:      limit,
		Total:      len(set.Rows),
		NextCursor: next,
		HasMore:    hasMore,
	}, nil
}

func (s RowSet[T]) Page(options PageOptions) (Page[T], error) {
	return PageRows(s, options)
}

func (v *TableView[T]) Page(options PageOptions) (Page[T], error) {
	if v == nil {
		return Page[T]{}, ErrTableRequired
	}
	return v.Snapshot().Page(options)
}

func EncodePageCursor(offset int) string {
	if offset <= 0 {
		return ""
	}
	payload, _ := json.Marshal(map[string]int{"offset": offset})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func DecodePageCursor(cursor string) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("decode page cursor: %w", err)
	}
	var payload struct {
		Offset int `json:"offset"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, fmt.Errorf("parse page cursor: %w", err)
	}
	if payload.Offset < 0 {
		return 0, fmt.Errorf("page cursor offset must be non-negative")
	}
	return payload.Offset, nil
}

func normalizePageOptions(options PageOptions, total int) (int, int, error) {
	offset := options.Offset
	if strings.TrimSpace(options.Cursor) != "" {
		decoded, err := DecodePageCursor(options.Cursor)
		if err != nil {
			return 0, 0, err
		}
		offset = decoded
	}
	if offset < 0 {
		return 0, 0, fmt.Errorf("page offset must be non-negative")
	}
	if offset > total {
		offset = total
	}
	limit := options.Limit
	if limit < 0 {
		return 0, 0, fmt.Errorf("page limit must be positive")
	}
	if limit == 0 {
		limit = DefaultPageLimit
	}
	return offset, limit, nil
}

func ParsePageOptions(offsetText, limitText, cursor string) (PageOptions, error) {
	options := PageOptions{Cursor: strings.TrimSpace(cursor)}
	if strings.TrimSpace(offsetText) != "" {
		offset, err := strconv.Atoi(strings.TrimSpace(offsetText))
		if err != nil {
			return PageOptions{}, fmt.Errorf("parse page offset: %w", err)
		}
		options.Offset = offset
	}
	if strings.TrimSpace(limitText) != "" {
		limit, err := strconv.Atoi(strings.TrimSpace(limitText))
		if err != nil {
			return PageOptions{}, fmt.Errorf("parse page limit: %w", err)
		}
		options.Limit = limit
	}
	return options, nil
}
