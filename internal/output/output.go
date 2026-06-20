// Package output formatea la salida del CLI en tablas legibles o JSON para scripts.
package output

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// Format es el formato de salida elegido por el usuario.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
)

// Parse valida un string de formato; devuelve table por defecto.
func Parse(s string) (Format, error) {
	switch Format(s) {
	case FormatTable, FormatJSON:
		return Format(s), nil
	case "":
		return FormatTable, nil
	default:
		return FormatTable, fmt.Errorf("formato inválido %q (usa: table, json)", s)
	}
}

// JSON imprime cualquier valor como JSON indentado.
func JSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Table imprime filas tabuladas con cabecera. Cada fila debe tener tantas
// columnas como headers.
func Table(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	w.Flush()
}

// Render elige entre tabla y JSON según el formato. En JSON imprime raw;
// en tabla usa los headers/rows provistos.
func Render(f Format, raw any, headers []string, rows [][]string) error {
	if f == FormatJSON {
		return JSON(raw)
	}
	if len(rows) == 0 {
		fmt.Println("(sin resultados)")
		return nil
	}
	Table(headers, rows)
	return nil
}
