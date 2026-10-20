// Package export writes items in formats other programs open (ADR-0071):
// a page as a Word document, tables and lists as CSV. It is pure: it gets
// what to write and writes it, without reading the store.
package export

import (
	"encoding/csv"
	"io"
	"strings"
)

// CSV writes rows as CSV with a byte order mark, so spreadsheet programs
// read it as UTF-8, separated by sep: a semicolon for languages whose
// spreadsheets expect one (German), a comma otherwise. A cell that a
// spreadsheet would take for a formula is quoted with an apostrophe first,
// so an exported value never runs as a formula (CSV injection).
func CSV(w io.Writer, rows [][]string, sep rune) error {
	if _, err := io.WriteString(w, "\xef\xbb\xbf"); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.Comma = sep
	cw.UseCRLF = true
	for _, row := range rows {
		safe := make([]string, len(row))
		for i, cell := range row {
			safe[i] = defuse(cell)
		}
		if err := cw.Write(safe); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// defuse keeps a cell from being read as a formula.
func defuse(cell string) string {
	if cell != "" && strings.ContainsRune("=+-@\t\r", rune(cell[0])) {
		return "'" + cell
	}
	return cell
}

// TableRows splits a table block's text into rows of cells: rows are
// separated by newlines, cells by tabs (ADR-0042).
func TableRows(text string) [][]string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var rows [][]string
	for _, line := range strings.Split(text, "\n") {
		rows = append(rows, strings.Split(line, "\t"))
	}
	return rows
}

// Separator is the CSV separator for a UI language.
func Separator(lang string) rune {
	if lang == "de" {
		return ';'
	}
	return ','
}
