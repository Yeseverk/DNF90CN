package datatable

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var errXLSXPartNotFound = errors.New("xlsx part not found")

type GenerateOptions struct {
	Input       string `json:"input"`
	Output      string `json:"output"`
	Format      string `json:"format,omitempty"`
	Version     string `json:"version,omitempty"`
	ManifestOut string `json:"manifest_out,omitempty"`
	Pretty      bool   `json:"pretty,omitempty"`
}

type GeneratedTable struct {
	Name     string `json:"name"`
	Source   string `json:"source"`
	Output   string `json:"output"`
	Rows     int    `json:"rows"`
	Bytes    int64  `json:"bytes"`
	Checksum string `json:"checksum"`
}

type GenerateReport struct {
	Input       string           `json:"input"`
	Output      string           `json:"output"`
	Version     string           `json:"version,omitempty"`
	Tables      []GeneratedTable `json:"tables"`
	Manifest    Manifest         `json:"manifest"`
	GeneratedAt time.Time        `json:"generated_at"`
}

func GenerateTables(ctx context.Context, options GenerateOptions) (GenerateReport, error) {
	if err := contextErr(ctx); err != nil {
		return GenerateReport{}, err
	}
	options.Input = strings.TrimSpace(options.Input)
	options.Output = strings.TrimSpace(options.Output)
	options.Format = strings.ToLower(strings.TrimSpace(options.Format))
	options.Version = strings.TrimSpace(options.Version)
	options.ManifestOut = strings.TrimSpace(options.ManifestOut)
	if options.Input == "" {
		return GenerateReport{}, ErrDirectoryRequired
	}
	if options.Output == "" {
		return GenerateReport{}, fmt.Errorf("data table output is required")
	}
	info, err := os.Stat(options.Input)
	if err != nil {
		return GenerateReport{}, err
	}
	sources, err := generatorSources(options.Input, info)
	if err != nil {
		return GenerateReport{}, err
	}
	if len(sources) == 0 {
		return GenerateReport{}, fmt.Errorf("no data table source files found in %s", options.Input)
	}
	if err := os.MkdirAll(options.Output, 0o750); err != nil {
		return GenerateReport{}, err
	}
	report := GenerateReport{
		Input:       options.Input,
		Output:      options.Output,
		Version:     options.Version,
		GeneratedAt: time.Now().UTC(),
	}
	tableSources := make(map[string]string, len(sources))
	for _, source := range sources {
		if err := contextErr(ctx); err != nil {
			return GenerateReport{}, err
		}
		tableName := tableNameFromFile(source)
		tableKey := strings.ToLower(tableName)
		if previous, ok := tableSources[tableKey]; ok {
			return GenerateReport{}, fmt.Errorf("duplicate generated data table name %q from %s and %s", tableName, previous, source)
		}
		tableSources[tableKey] = source
		rows, err := loadGeneratedRows(source, options.Format)
		if err != nil {
			return GenerateReport{}, fmt.Errorf("generate table %s: %w", source, err)
		}
		data, err := marshalGeneratedRows(rows, options.Pretty)
		if err != nil {
			return GenerateReport{}, err
		}
		outPath := filepath.Join(options.Output, tableName+".json")
		if err := os.WriteFile(outPath, data, 0o600); err != nil {
			return GenerateReport{}, err
		}
		checksum := sha256.Sum256(data)
		report.Tables = append(report.Tables, GeneratedTable{
			Name:     tableName,
			Source:   source,
			Output:   outPath,
			Rows:     len(rows),
			Bytes:    int64(len(data)),
			Checksum: hex.EncodeToString(checksum[:]),
		})
	}
	sort.Slice(report.Tables, func(i, j int) bool { return report.Tables[i].Name < report.Tables[j].Name })
	registry := NewRegistry(options.Output, options.Version)
	if err := registry.Load(ctx); err != nil {
		return GenerateReport{}, err
	}
	report.Manifest = registry.Manifest()
	if options.ManifestOut != "" {
		manifestData, err := json.MarshalIndent(report.Manifest, "", "  ")
		if err != nil {
			return GenerateReport{}, err
		}
		if err := os.MkdirAll(filepath.Dir(options.ManifestOut), 0o750); err != nil && filepath.Dir(options.ManifestOut) != "." {
			return GenerateReport{}, err
		}
		if err := os.WriteFile(options.ManifestOut, append(manifestData, '\n'), 0o600); err != nil {
			return GenerateReport{}, err
		}
	}
	return report, nil
}

func generatorSources(input string, info os.FileInfo) ([]string, error) {
	if !info.IsDir() {
		if genExtSupported(input) {
			return []string{input}, nil
		}
		return nil, fmt.Errorf("unsupported data table source %s", input)
	}
	var sources []string
	if err := filepath.WalkDir(input, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasPrefix(filepath.Base(path), ".") {
			return nil
		}
		if genExtSupported(path) {
			sources = append(sources, path)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(sources)
	return sources, nil
}

func genExtSupported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json", ".csv", ".xlsx":
		return true
	default:
		return false
	}
}

func loadGeneratedRows(path, format string) ([]map[string]any, error) {
	if format == "" || format == "auto" {
		format = strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	}
	switch format {
	case "json":
		return loadGeneratedJSON(path)
	case "csv":
		return loadGeneratedCSV(path)
	case "xlsx":
		return loadGeneratedXLSX(path)
	default:
		return nil, fmt.Errorf("unsupported data table format %q", format)
	}
}

func loadGeneratedJSON(path string) ([]map[string]any, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err == nil {
		return rows, nil
	}
	var wrapped struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(data, &wrapped); err != nil {
		return nil, err
	}
	if wrapped.Items == nil {
		return nil, fmt.Errorf("json table must be an array or object with items")
	}
	return wrapped.Items, nil
}

func loadGeneratedCSV(path string) ([]map[string]any, error) {
	file, err := os.Open(path) //nolint:gosec // G304：路径来自框架配置、仓库扫描或测试临时目录，调用点负责限定输入范围。
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	return recordsToRows(records)
}

func loadGeneratedXLSX(path string) ([]map[string]any, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	shared, err := readXLSXStrings(&reader.Reader)
	if err != nil {
		return nil, err
	}
	sheet, err := readZipFile(&reader.Reader, "xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	records, err := parseXLSXSheet(sheet, shared)
	if err != nil {
		return nil, err
	}
	return recordsToRows(records)
}

func recordsToRows(records [][]string) ([]map[string]any, error) {
	if len(records) == 0 {
		return nil, nil
	}
	headers := normalizeHeaders(records[0])
	if len(headers) == 0 {
		return nil, fmt.Errorf("data table header row is empty")
	}
	rows := make([]map[string]any, 0, len(records)-1)
	for _, record := range records[1:] {
		if emptyRecord(record) {
			continue
		}
		row := make(map[string]any, len(headers))
		for idx, header := range headers {
			value := ""
			if idx < len(record) {
				value = strings.TrimSpace(record[idx])
			}
			row[header] = inferGeneratedValue(value)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func normalizeHeaders(headers []string) []string {
	out := make([]string, 0, len(headers))
	seen := make(map[string]struct{}, len(headers))
	for idx, header := range headers {
		header = strings.TrimSpace(header)
		if header == "" {
			header = fmt.Sprintf("col_%d", idx+1)
		}
		base := header
		for suffix := 2; ; suffix++ {
			if _, ok := seen[header]; !ok {
				break
			}
			header = fmt.Sprintf("%s_%d", base, suffix)
		}
		seen[header] = struct{}{}
		out = append(out, header)
	}
	return out
}

func emptyRecord(record []string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func inferGeneratedValue(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	switch strings.ToLower(value) {
	case "true", "false":
		parsed, _ := strconv.ParseBool(strings.ToLower(value))
		return parsed
	}
	if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
		return parsed
	}
	if parsed, err := strconv.ParseFloat(value, 64); err == nil {
		return parsed
	}
	return value
}

func marshalGeneratedRows(rows []map[string]any, pretty bool) ([]byte, error) {
	var data []byte
	var err error
	if pretty {
		data, err = json.MarshalIndent(rows, "", "  ")
	} else {
		data, err = json.Marshal(rows)
	}
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func tableNameFromFile(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	name = strings.TrimSpace(name)
	if name == "" {
		return "table"
	}
	return name
}

func readZipFile(reader *zip.Reader, name string) ([]byte, error) {
	for _, file := range reader.File {
		if filepath.ToSlash(file.Name) != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("%w: %s", errXLSXPartNotFound, name)
}

func readXLSXStrings(reader *zip.Reader) ([]string, error) {
	data, err := readZipFile(reader, "xl/sharedStrings.xml")
	if err != nil {
		if errors.Is(err, errXLSXPartNotFound) {
			return nil, nil
		}
		return nil, err
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var values []string
	var current strings.Builder
	inText := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			if t.Name.Local == "si" {
				current.Reset()
			}
			if t.Name.Local == "t" {
				inText = true
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
			}
			if t.Name.Local == "si" {
				values = append(values, current.String())
			}
		case xml.CharData:
			if inText {
				current.Write([]byte(t))
			}
		}
	}
	return values, nil
}

func parseXLSXSheet(data []byte, shared []string) ([][]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var records [][]string
	var row []string
	var cellRef string
	var cellType string
	var value strings.Builder
	inValue := false
	inInlineText := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row = nil
			case "c":
				cellRef = ""
				cellType = ""
				value.Reset()
				for _, attr := range t.Attr {
					switch attr.Name.Local {
					case "r":
						cellRef = attr.Value
					case "t":
						cellType = attr.Value
					}
				}
			case "v":
				inValue = true
			case "t":
				if cellType == "inlineStr" {
					inInlineText = true
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inValue = false
			case "t":
				inInlineText = false
			case "c":
				text := decodeXLSXCell(value.String(), cellType, shared)
				idx := xlsxCellIndex(cellRef)
				if idx < 0 {
					idx = len(row)
				}
				for len(row) <= idx {
					row = append(row, "")
				}
				row[idx] = text
			case "row":
				records = append(records, append([]string(nil), row...))
			}
		case xml.CharData:
			if inValue || inInlineText {
				value.Write([]byte(t))
			}
		}
	}
	return records, nil
}

func decodeXLSXCell(raw, cellType string, shared []string) string {
	raw = strings.TrimSpace(raw)
	if cellType != "s" {
		return raw
	}
	idx, err := strconv.Atoi(raw)
	if err != nil || idx < 0 || idx >= len(shared) {
		return raw
	}
	return shared[idx]
}

func xlsxCellIndex(ref string) int {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return -1
	}
	idx := 0
	found := false
	for _, r := range ref {
		if r >= 'A' && r <= 'Z' {
			idx = idx*26 + int(r-'A'+1)
			found = true
			continue
		}
		if r >= 'a' && r <= 'z' {
			idx = idx*26 + int(r-'a'+1)
			found = true
			continue
		}
		break
	}
	if !found {
		return -1
	}
	return idx - 1
}
