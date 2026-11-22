package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// printTable draws a simple ASCII table (works on every terminal, incl. old Windows consoles).
func printTable(w io.Writer, title string, headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, r := range rows {
		for i, cell := range r {
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}

	line := func(fill string, sep string) string {
		parts := make([]string, len(widths))
		for i, wd := range widths {
			parts[i] = strings.Repeat(fill, wd+2)
		}
		return "+" + strings.Join(parts, sep) + "+"
	}
	row := func(cells []string) string {
		parts := make([]string, len(widths))
		for i, wd := range widths {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			parts[i] = " " + cell + strings.Repeat(" ", wd-utf8.RuneCountInString(cell)) + " "
		}
		return "|" + strings.Join(parts, "|") + "|"
	}

	if title != "" {
		fmt.Fprintln(w, title)
	}
	fmt.Fprintln(w, line("-", "+"))
	fmt.Fprintln(w, row(headers))
	fmt.Fprintln(w, line("-", "+"))
	for _, r := range rows {
		fmt.Fprintln(w, row(r))
	}
	fmt.Fprintln(w, line("-", "+"))
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ") // one line
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-3]) + "..."
}
