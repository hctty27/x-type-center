package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/xuri/excelize/v2"

	"github.com/hctty27/x-type-center/internal/store"
)

type Mapping struct {
	Sheets []SheetMapping `json:"sheets"`
}

type SheetMapping struct {
	Name         string          `json:"name"`
	FixedProject string          `json:"fixedProject,omitempty"`
	Columns      []ColumnMapping `json:"columns"`
}

type ColumnMapping struct {
	Namespace         string `json:"namespace"`
	Header            string `json:"header"`
	DisplayName       string `json:"displayName,omitempty"`
	DescriptionHeader string `json:"descriptionHeader,omitempty"`
	ProjectHeader     string `json:"projectHeader,omitempty"`
}

type Report struct {
	Sheets          int      `json:"sheets"`
	Namespaces      int      `json:"namespaces"`
	EntriesInserted int      `json:"entriesInserted"`
	EntriesExisting int      `json:"entriesExisting"`
	RangesInserted  int      `json:"rangesInserted"`
	EmptyCells      int      `json:"emptyCells"`
	NonNumericCells int      `json:"nonNumericCells"`
	Warnings        []string `json:"warnings,omitempty"`
}

type Importer struct {
	store *store.MySQL
}

func New(s *store.MySQL) *Importer {
	return &Importer{store: s}
}

func LoadMapping(path string) (Mapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Mapping{}, fmt.Errorf("read mapping: %w", err)
	}
	var mapping Mapping
	if err := json.Unmarshal(data, &mapping); err != nil {
		return Mapping{}, fmt.Errorf("decode mapping: %w", err)
	}
	if len(mapping.Sheets) == 0 {
		return Mapping{}, fmt.Errorf("mapping has no sheets")
	}
	return mapping, nil
}

func (i *Importer) Import(ctx context.Context, filePath string, mapping Mapping) (Report, error) {
	workbook, err := excelize.OpenFile(filePath, excelize.Options{RawCellValue: false})
	if err != nil {
		return Report{}, fmt.Errorf("open xlsx: %w", err)
	}
	defer workbook.Close()

	report := Report{}
	touched := make(map[int64]struct{})

	for _, sheet := range mapping.Sheets {
		rows, err := workbook.GetRows(sheet.Name)
		if err != nil {
			return report, fmt.Errorf("read sheet %q: %w", sheet.Name, err)
		}
		if len(rows) == 0 {
			return report, fmt.Errorf("sheet %q is empty", sheet.Name)
		}
		report.Sheets++

		headers := rows[0]
		for _, column := range sheet.Columns {
			valueColumn, err := resolveColumn(headers, column.Header)
			if err != nil {
				return report, fmt.Errorf("sheet %q namespace %q: %w", sheet.Name, column.Namespace, err)
			}
			descriptionColumn := -1
			if column.DescriptionHeader != "" {
				descriptionColumn, err = resolveColumn(headers, column.DescriptionHeader)
				if err != nil {
					return report, fmt.Errorf("sheet %q namespace %q description column: %w", sheet.Name, column.Namespace, err)
				}
			}
			projectColumn := -1
			if column.ProjectHeader != "" {
				projectColumn, err = resolveColumn(headers, column.ProjectHeader)
				if err != nil {
					return report, fmt.Errorf("sheet %q namespace %q project column: %w", sheet.Name, column.Namespace, err)
				}
			}

			displayName := strings.TrimSpace(column.DisplayName)
			if displayName == "" {
				displayName = column.Namespace
			}
			namespace, err := i.store.EnsureNamespace(ctx, column.Namespace, displayName)
			if err != nil {
				return report, fmt.Errorf("ensure namespace %q: %w", column.Namespace, err)
			}
			if _, ok := touched[namespace.ID]; !ok {
				report.Namespaces++
				touched[namespace.ID] = struct{}{}
			}

			for rowIndex := 1; rowIndex < len(rows); rowIndex++ {
				raw := cell(rows[rowIndex], valueColumn)
				parsed := parseValue(raw)
				if parsed.Kind == valueEmpty {
					report.EmptyCells++
					continue
				}
				if parsed.Kind == valueUnknown {
					report.NonNumericCells++
					report.Warnings = appendWarning(report.Warnings, fmt.Sprintf("%s!%s%d ignored: %q", sheet.Name, columnName(valueColumn+1), rowIndex+1, raw))
					continue
				}

				project := strings.TrimSpace(sheet.FixedProject)
				if projectColumn >= 0 {
					if fromRow := strings.TrimSpace(cell(rows[rowIndex], projectColumn)); fromRow != "" {
						project = fromRow
					}
				}
				description := ""
				if descriptionColumn >= 0 {
					description = strings.TrimSpace(cell(rows[rowIndex], descriptionColumn))
				}
				sourceRef := fmt.Sprintf("%s!%s%d", sheet.Name, columnName(valueColumn+1), rowIndex+1)

				switch parsed.Kind {
				case valueNumber:
					inserted, err := i.store.ImportHistoricalEntry(ctx, namespace.ID, parsed.Value, project, description, sourceRef)
					if err != nil {
						return report, fmt.Errorf("import %s value %d: %w", sourceRef, parsed.Value, err)
					}
					if inserted {
						report.EntriesInserted++
					} else {
						report.EntriesExisting++
					}
				case valueReservedRange:
					rangeProject := project
					if parsed.Project != "" {
						rangeProject = parsed.Project
					}
					rangeDescription := description
					if rangeDescription == "" {
						rangeDescription = strings.TrimSpace(raw)
					}
					if err := i.store.EnsureReservedRange(ctx, namespace.ID, parsed.Start, parsed.End, rangeProject, rangeDescription); err != nil {
						return report, fmt.Errorf("import reserved range %s: %w", sourceRef, err)
					}
					report.RangesInserted++
				}
			}
		}
	}

	for namespaceID := range touched {
		if err := i.store.RecalculateNamespaceCursor(ctx, namespaceID); err != nil {
			return report, fmt.Errorf("recalculate namespace %d cursor: %w", namespaceID, err)
		}
	}
	return report, nil
}

func resolveColumn(headers []string, expected string) (int, error) {
	target := normalizeHeader(expected)
	if target == "" {
		return -1, fmt.Errorf("empty header mapping")
	}
	matches := make([]int, 0, 1)
	for idx, header := range headers {
		if normalizeHeader(header) == target {
			matches = append(matches, idx)
		}
	}
	switch len(matches) {
	case 0:
		return -1, fmt.Errorf("header %q not found", expected)
	case 1:
		return matches[0], nil
	default:
		return -1, fmt.Errorf("header %q matched %d columns; mapping must be unique", expected, len(matches))
	}
}

func normalizeHeader(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(value))
}

func cell(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

type valueKind int

const (
	valueEmpty valueKind = iota
	valueNumber
	valueReservedRange
	valueUnknown
)

type parsedValue struct {
	Kind    valueKind
	Value   int64
	Start   int64
	End     int64
	Project string
}

var (
	rangePattern   = regexp.MustCompile(`^\s*(\d+)\s*[-~～—–]\s*(\d+)\s*(.*)$`)
	reserveProject = regexp.MustCompile(`(?i)([A-Za-z][A-Za-z0-9_-]*)\s*预留`)
)

func parseValue(raw string) parsedValue {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return parsedValue{Kind: valueEmpty}
	}
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return parsedValue{Kind: valueNumber, Value: value}
	}
	if value, err := strconv.ParseFloat(raw, 64); err == nil && value == float64(int64(value)) {
		return parsedValue{Kind: valueNumber, Value: int64(value)}
	}
	match := rangePattern.FindStringSubmatch(raw)
	if len(match) == 4 {
		start, err1 := strconv.ParseInt(match[1], 10, 64)
		end, err2 := strconv.ParseInt(match[2], 10, 64)
		if err1 == nil && err2 == nil && start <= end {
			project := ""
			if projectMatch := reserveProject.FindStringSubmatch(match[3]); len(projectMatch) == 2 {
				project = strings.ToUpper(projectMatch[1])
			}
			return parsedValue{Kind: valueReservedRange, Start: start, End: end, Project: project}
		}
	}
	return parsedValue{Kind: valueUnknown}
}

func columnName(column int) string {
	if column <= 0 {
		return "?"
	}
	var out []byte
	for column > 0 {
		column--
		out = append([]byte{byte('A' + column%26)}, out...)
		column /= 26
	}
	return string(out)
}

func appendWarning(warnings []string, warning string) []string {
	const maxWarnings = 50
	if len(warnings) >= maxWarnings {
		return warnings
	}
	return append(warnings, warning)
}
