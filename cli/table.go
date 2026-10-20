package cli

import (
	"cmp"
	"strings"
)

// TableOpts shapes Table. The zero value is left-aligned columns two spaces
// apart, no header, no indent.
type TableOpts struct {
	Head   []string // column headers, bolded
	Right  []bool   // per column: right-aligned
	Gap    int      // spaces between columns (0 = 2)
	Indent int      // spaces before every line
}

// Table renders rows as aligned columns. Cells may be coloured: widths are
// visible widths. The last left-aligned column is not padded, so no line
// carries trailing spaces.
func Table(rows [][]string, o TableOpts) string {
	gap := strings.Repeat(" ", cmp.Or(o.Gap, 2))
	lead := strings.Repeat(" ", o.Indent)
	body := rows
	if o.Head != nil {
		head := make([]string, len(o.Head))
		for i, h := range o.Head {
			head[i] = UI.Head(h)
		}
		body = append([][]string{head}, rows...)
	}
	cols := 0
	for _, r := range body {
		cols = max(cols, len(r))
	}
	w := make([]int, cols)
	for _, r := range body {
		for c, cell := range r {
			w[c] = max(w[c], Width(cell))
		}
	}
	lines := make([]string, len(body))
	for i, r := range body {
		cells := make([]string, cols)
		for c := range cols {
			cell := ""
			if c < len(r) {
				cell = r[c]
			}
			switch {
			case c < len(o.Right) && o.Right[c]:
				cells[c] = PadStart(cell, w[c])
			case c == cols-1:
				cells[c] = cell
			default:
				cells[c] = PadEnd(cell, w[c])
			}
		}
		lines[i] = strings.TrimRight(lead+strings.Join(cells, gap), " ")
	}
	return strings.Join(lines, "\n")
}

// KV renders key/value pairs with the keys aligned and muted, for a detail
// view.
func KV(pairs [][2]string, indent int) string {
	rows := make([][]string, len(pairs))
	for i, p := range pairs {
		rows[i] = []string{UI.Muted(p[0]), p[1]}
	}
	return Table(rows, TableOpts{Indent: indent})
}
