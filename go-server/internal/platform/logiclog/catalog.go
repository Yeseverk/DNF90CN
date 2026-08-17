package logiclog

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

type Reason struct {
	Code        string `json:"code"`
	Category    string `json:"category"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type Catalog struct {
	reasons map[string]Reason
}

func NewCatalog(reasons []Reason) (*Catalog, error) {
	catalog := &Catalog{reasons: make(map[string]Reason, len(reasons))}
	for _, reason := range reasons {
		reason.Code = strings.TrimSpace(reason.Code)
		reason.Category = strings.TrimSpace(reason.Category)
		reason.Name = strings.TrimSpace(reason.Name)
		if reason.Code == "" {
			return nil, fmt.Errorf("logiclog reason code is required")
		}
		if reason.Name == "" {
			return nil, fmt.Errorf("logiclog reason %s name is required", reason.Code)
		}
		if _, exists := catalog.reasons[reason.Code]; exists {
			return nil, fmt.Errorf("duplicate logiclog reason code %s", reason.Code)
		}
		catalog.reasons[reason.Code] = reason
	}
	return catalog, nil
}

func LoadCatalogCSV(path string) (*Catalog, error) {
	file, err := os.Open(path) // #nosec G304 -- 目录表路径由工具/测试传入的受控文件路径提供。
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return ReadCatalogCSV(file)
}

func ReadCatalogCSV(r io.Reader) (*Catalog, error) {
	reader := csv.NewReader(r)
	reader.TrimLeadingSpace = true
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return NewCatalog(nil)
	}
	start := 0
	if len(rows[0]) > 0 && strings.EqualFold(strings.TrimSpace(rows[0][0]), "code") {
		start = 1
	}
	reasons := make([]Reason, 0, len(rows)-start)
	for i, row := range rows[start:] {
		if len(row) < 3 {
			return nil, fmt.Errorf("logiclog reason csv row %d must contain code,category,name", i+start+1)
		}
		reason := Reason{
			Code:     row[0],
			Category: row[1],
			Name:     row[2],
		}
		if len(row) > 3 {
			reason.Description = row[3]
		}
		reasons = append(reasons, reason)
	}
	return NewCatalog(reasons)
}

func (c *Catalog) Get(code string) (Reason, bool) {
	if c == nil {
		return Reason{}, false
	}
	reason, ok := c.reasons[strings.TrimSpace(code)]
	return reason, ok
}

func (c *Catalog) MustAllow(code string) error {
	if c == nil {
		return nil
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("logiclog reason code is required")
	}
	if _, ok := c.reasons[code]; !ok {
		return fmt.Errorf("unknown logiclog reason code %s", code)
	}
	return nil
}
