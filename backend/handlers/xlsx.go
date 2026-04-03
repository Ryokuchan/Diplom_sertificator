package handlers

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

func parseXLSX(path string) ([]map[string]string, error) {
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("no sheets found")
	}

	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, err
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("empty sheet")
	}

	headers := rows[0]
	var records []map[string]string
	for _, row := range rows[1:] {
		rec := make(map[string]string)
		for i, h := range headers {
			if i < len(row) {
				rec[strings.ToLower(strings.TrimSpace(h))] = row[i]
			}
		}
		records = append(records, rec)
	}
	return records, nil
}
