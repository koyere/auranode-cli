// Package output formats the CLI output as readable tables or JSON for scripts.
package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// Format is the output format chosen by the user.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
)

// Parse validates a format string; returns table by default.
func Parse(s string) (Format, error) {
	switch Format(s) {
	case FormatTable, FormatJSON:
		return Format(s), nil
	case "":
		return FormatTable, nil
	default:
		return FormatTable, fmt.Errorf("invalid format %q (use: table, json)", s)
	}
}

// JSON prints any value as indented JSON.
func JSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Table prints tab-separated rows with a header. Each row must have as many
// columns as headers.
func Table(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	w.Flush()
}

// Render chooses between table and JSON depending on the format. In JSON it prints raw;
// in table mode it uses the provided headers/rows.
func Render(f Format, raw any, headers []string, rows [][]string) error {
	if f == FormatJSON {
		return JSON(raw)
	}
	if len(rows) == 0 {
		fmt.Println("(no results)")
		return nil
	}
	Table(headers, rows)
	return nil
}
