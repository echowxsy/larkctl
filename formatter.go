package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
)

// printOutput dispatches to the appropriate formatter based on outputFormat.
func printOutput(data any) {
	switch outputFormat {
	case "table":
		printTable(os.Stdout, data)
	case "csv":
		printCSV(os.Stdout, data)
	default:
		printJSON(data)
	}
}

// printTable writes aligned column output using text/tabwriter.
func printTable(w io.Writer, data any) {
	rows, cols := extractRows(data)
	if len(rows) == 0 {
		printJSON(data)
		return
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// Header
	fmt.Fprintln(tw, strings.Join(cols, "\t"))
	// Separator
	seps := make([]string, len(cols))
	for i, c := range cols {
		seps[i] = strings.Repeat("-", len(c))
	}
	fmt.Fprintln(tw, strings.Join(seps, "\t"))
	// Rows
	for _, row := range rows {
		vals := make([]string, len(cols))
		for i, col := range cols {
			vals[i] = formatValue(row[col])
		}
		fmt.Fprintln(tw, strings.Join(vals, "\t"))
	}
	tw.Flush()
}

// printCSV writes CSV output.
func printCSV(w io.Writer, data any) {
	rows, cols := extractRows(data)
	if len(rows) == 0 {
		printJSON(data)
		return
	}

	cw := csv.NewWriter(w)
	_ = cw.Write(cols)
	for _, row := range rows {
		record := make([]string, len(cols))
		for i, col := range cols {
			record[i] = formatValue(row[col])
		}
		_ = cw.Write(record)
	}
	cw.Flush()
}

// extractRows normalizes API responses to rows + column names.
// Handles: {"items":[...]}, {"data":[...]}, [...], and other common Feishu response shapes.
// For single objects, wraps as one-row table.
func extractRows(data any) ([]map[string]any, []string) {
	// Try to find a list in the response
	tryKeys := []string{"items", "data", "users", "events", "records", "nodes", "spaces", "tables", "fields", "files"}

	var list []any

	switch v := data.(type) {
	case []any:
		list = v
	case map[string]any:
		for _, key := range tryKeys {
			if val, ok := v[key]; ok {
				if arr, ok := val.([]any); ok {
					list = arr
					break
				}
			}
		}
		if list == nil {
			// Single object — wrap as one row
			list = []any{v}
		}
	default:
		// Re-serialize and try again
		b, err := json.Marshal(data)
		if err != nil {
			return nil, nil
		}
		var generic any
		if err := json.Unmarshal(b, &generic); err != nil {
			return nil, nil
		}
		return extractRows(generic)
	}

	if len(list) == 0 {
		return nil, nil
	}

	// Convert each element to map[string]any
	var rows []map[string]any
	colSeen := map[string]bool{}
	var colOrder []string

	for _, item := range list {
		var row map[string]any
		switch r := item.(type) {
		case map[string]any:
			row = r
		default:
			b, _ := json.Marshal(item)
			if err := json.Unmarshal(b, &row); err != nil || row == nil {
				continue
			}
		}
		rows = append(rows, row)
		for k := range row {
			if !colSeen[k] {
				colSeen[k] = true
				colOrder = append(colOrder, k)
			}
		}
	}

	if len(rows) == 0 {
		return nil, nil
	}

	cols := sortedKeys(colOrder)
	return rows, cols
}

// sortedKeys sorts column names: prioritize id/name/type/status/time columns first, then alphabetical.
func sortedKeys(keys []string) []string {
	priority := []string{"id", "name", "title", "type", "status", "time", "created_time", "updated_time"}
	prio := map[string]int{}
	for i, p := range priority {
		prio[p] = i + 1
	}

	sorted := make([]string, len(keys))
	copy(sorted, keys)

	sort.SliceStable(sorted, func(i, j int) bool {
		pi, pj := 0, 0
		ki, kj := sorted[i], sorted[j]
		// Check for exact match first, then prefix match
		if v, ok := prio[ki]; ok {
			pi = v
		} else {
			for _, p := range priority {
				if strings.HasPrefix(ki, p) || strings.HasSuffix(ki, "_"+p) {
					pi = prio[p] + 100
					break
				}
			}
		}
		if v, ok := prio[kj]; ok {
			pj = v
		} else {
			for _, p := range priority {
				if strings.HasPrefix(kj, p) || strings.HasSuffix(kj, "_"+p) {
					pj = prio[p] + 100
					break
				}
			}
		}
		if pi != pj {
			if pi == 0 {
				return false
			}
			if pj == 0 {
				return true
			}
			return pi < pj
		}
		return ki < kj
	})
	return sorted
}

// formatValue formats a value for table/CSV output.
// Strings are returned as-is, float64 as int if whole, nested objects as truncated JSON (max 60 chars).
func formatValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int, int64, int32:
		return fmt.Sprintf("%d", val)
	default:
		b, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		s := string(b)
		if len(s) > 60 {
			s = s[:57] + "..."
		}
		return s
	}
}
