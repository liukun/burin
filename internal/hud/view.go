package hud

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"burin/internal/leader"
)

// Layout, in DIPs.
const (
	// MaxRows is the most entries a column of the list layout holds; more
	// entries make more columns, as balanced as they go.
	MaxRows = 12

	fontSize  = 13
	padding   = 12
	headerGap = 8
	// columnGap is between the columns of the list layout.
	columnGap = 12
	// rowCellGap is between the cells of a row in the rows layout: small,
	// as each cell's padding and empty mark slot already set it apart.
	rowCellGap  = 0
	rowGap      = 2
	rowPaddingX = 6
	rowPaddingY = 2
	cellGap     = 8
	countGap    = 24
	errorWidth  = 360
	monospace   = "monospace"
	// slack keeps rounding in the text measurements from wrapping text.
	slack = 2
)

// keyColor tells an entry's key from its title.
var keyColor = ui.Hex("#73d94d")

// Marks of the entry statuses. Idle has none.
const (
	MarkRunning = "●"
	MarkDone    = "✓"
	MarkFailed  = "✗"
)

// Build builds the HUD of s and returns the content size it needs.
func Build(c *ui.Context, s leader.Snapshot) (width, height float32) {
	t := c.Theme()
	c.Root().Background(ui.Transparent)
	if len(s.Plain) > 0 {
		return buildPlain(c, t, s.Plain)
	}
	m := measure(c, s)
	panel(c, t, headerGap).Children(func() {
		header(c, t, s, m)
		switch {
		case s.Err != nil:
			ui.Text(c, s.Err.Error()).FontSize(fontSize).TextColor(t.Danger).Wrap().Width(m.errW)
		case len(s.Entries) > 0:
			entries(c, t, s.Layout, m)
		}
	})
	return m.width, m.height
}

// buildPlain builds the HUD of plain lines, one under the other in the
// same box as the entries, and returns the content size it needs.
func buildPlain(c *ui.Context, t *ui.Theme, lines []string) (width, height float32) {
	var textW, lineH float32
	for _, l := range lines {
		w, h := c.MeasureText(0, ui.Span{Text: l, Size: fontSize})
		textW, lineH = max(textW, w+slack), max(lineH, h)
	}
	panel(c, t, rowGap).Children(func() {
		for i, l := range lines {
			ui.Text(c.Key(i), l).FontSize(fontSize).Height(lineH).NoWrap()
		}
	})
	n := float32(len(lines))
	return 2*padding + textW, 2*padding + n*lineH + (n-1)*rowGap
}

// panel is the box the HUD's content goes in, its children gap apart.
func panel(c *ui.Context, t *ui.Theme, gap float32) ui.Element {
	box := ui.Column(c).Fill().Padding(padding).Gap(gap)
	if !c.Vibrancy() {
		box.Background(t.Surface).Radius(cornerRadius)
	}
	return box
}

// metrics are the sizes the layout is built from, measured from the text
// so that the window can be sized to fit it.
type metrics struct {
	lineH, rowH float32
	// markW and keyW are the same across all entries, so that they line
	// up.
	markW, keyW float32
	// columns holds the entries of the list layout, top to bottom, each
	// column columnW wide.
	columns [][]leader.EntryState
	columnW []float32
	// rows holds the entries of the rows layout, left to right, each cell
	// as wide as its title needs.
	rows          [][]leader.EntryState
	cellW         [][]float32
	errW, errH    float32
	width, height float32
}

// countText counts the entries of the leader, as the HUD's header shows.
func countText(s leader.Snapshot) string {
	if len(s.Entries) == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", len(s.Entries))
}

func measure(c *ui.Context, s leader.Snapshot) metrics {
	text := func(str, font string, weight int) (float32, float32) {
		return c.MeasureText(0, ui.Span{Text: str, Font: font, Size: fontSize, Weight: weight})
	}
	var m metrics
	titleW, titleH := text(s.Key, "", 700)
	countW, _ := text(countText(s), "", 0)
	_, monoH := text("Ag", monospace, 0)
	_, plainH := text("Ag", "", 0)
	m.lineH = max(titleH, monoH, plainH)
	m.rowH = m.lineH + 2*rowPaddingY
	headerW := titleW + countGap + countW
	contentW, contentH := float32(0), float32(0)

	switch {
	case s.Err != nil:
		m.errW, m.errH = c.MeasureText(errorWidth, ui.Span{Text: s.Err.Error(), Size: fontSize})
		m.errW += slack
		contentW, contentH = m.errW, headerGap+m.errH
	case len(s.Entries) > 0:
		for _, mark := range []string{MarkRunning, MarkDone, MarkFailed} {
			w, _ := text(mark, monospace, 0)
			m.markW = max(m.markW, w)
		}
		for _, e := range s.Entries {
			w, _ := text(e.Key, monospace, 0)
			m.keyW = max(m.keyW, w)
		}
		// entryW is the width of an entry's row of mark, key and title.
		entryW := func(e leader.EntryState) float32 {
			tw, _ := text(e.Title, "", 0)
			return 2*rowPaddingX + m.markW + cellGap + m.keyW + cellGap + tw + slack
		}
		if s.Layout == leader.LayoutRows {
			contentW, contentH = m.measureRows(s.Entries, entryW)
		} else {
			contentW, contentH = m.measureColumns(s.Entries, entryW)
		}
		contentH += headerGap
	}
	m.width = 2*padding + max(headerW, contentW)
	m.height = 2*padding + m.lineH + contentH
	return m
}

// measureColumns lays entries out in columns and returns their size.
func (m *metrics) measureColumns(entries []leader.EntryState, entryW func(leader.EntryState) float32) (width, height float32) {
	m.columns = split(entries)
	for i, col := range m.columns {
		colW := float32(0)
		for _, e := range col {
			colW = max(colW, entryW(e))
		}
		m.columnW = append(m.columnW, colW)
		width += colW
		if i > 0 {
			width += columnGap
		}
	}
	return width, m.stackH(len(m.columns[0]))
}

// measureRows lays entries out in rows and returns their size: as wide
// as the widest row.
func (m *metrics) measureRows(entries []leader.EntryState, entryW func(leader.EntryState) float32) (width, height float32) {
	m.rows = splitRows(entries)
	for _, row := range m.rows {
		widths := make([]float32, len(row))
		rowW := float32(len(row)-1) * rowCellGap
		for j, e := range row {
			widths[j] = entryW(e)
			rowW += widths[j]
		}
		m.cellW = append(m.cellW, widths)
		width = max(width, rowW)
	}
	return width, m.stackH(len(m.rows))
}

// stackH is the height of n entry rows stacked.
func (m *metrics) stackH(n int) float32 {
	rows := float32(n)
	return rows*m.rowH + (rows-1)*rowGap
}

// split puts entries in columns of at most MaxRows, in order, filling
// each column but the last.
func split(entries []leader.EntryState) [][]leader.EntryState {
	columns := (len(entries) + MaxRows - 1) / MaxRows
	rows := (len(entries) + columns - 1) / columns
	var out [][]leader.EntryState
	for start := 0; start < len(entries); start += rows {
		out = append(out, entries[start:min(start+rows, len(entries))])
	}
	return out
}

// splitRows puts entries in rows, in order, starting a new row before
// each entry with RowBreak. The first entry starts the first row either
// way, so no row is empty.
func splitRows(entries []leader.EntryState) [][]leader.EntryState {
	if len(entries) == 0 {
		return nil
	}
	var out [][]leader.EntryState
	start := 0
	for i, e := range entries {
		startsRow := e.RowBreak && i > 0
		if startsRow {
			out = append(out, entries[start:i])
			start = i
		}
	}
	return append(out, entries[start:])
}

func header(c *ui.Context, t *ui.Theme, s leader.Snapshot, m metrics) {
	// The title is bold already; muted, it does not outshine the entries.
	titleColor := t.TextMuted
	if s.Err != nil {
		titleColor = t.Danger
	}
	ui.Row(c).AlignItems(ui.Center).Height(m.lineH).Children(func() {
		ui.Text(c, s.Key).FontSize(fontSize).Bold().TextColor(titleColor).NoWrap()
		ui.Spacer(c).MinWidth(countGap)
		ui.Text(c, countText(s)).FontSize(fontSize).TextColor(t.TextMuted).NoWrap()
	})
}

func entries(c *ui.Context, t *ui.Theme, layout leader.Layout, m metrics) {
	if layout == leader.LayoutRows {
		entryRows(c, t, m)
		return
	}
	ui.Row(c).Gap(columnGap).AlignItems(ui.Start).Children(func() {
		for i, col := range m.columns {
			ui.Column(c.Key(i)).Gap(rowGap).Width(m.columnW[i]).Children(func() {
				for _, e := range col {
					entry(c.Key(e.Key), t, e, m.columnW[i], m)
				}
			})
		}
	})
}

func entryRows(c *ui.Context, t *ui.Theme, m metrics) {
	ui.Column(c).Gap(rowGap).AlignItems(ui.Start).Children(func() {
		for i, row := range m.rows {
			ui.Row(c.Key(i)).Gap(rowCellGap).Children(func() {
				for j, e := range row {
					entry(c.Key(e.Key), t, e, m.cellW[i][j], m)
				}
			})
		}
	})
}

// entry builds the row of an entry, width wide.
func entry(c *ui.Context, t *ui.Theme, e leader.EntryState, width float32, m metrics) {
	row := ui.Row(c).AlignItems(ui.Center).Gap(cellGap).Width(width).Height(m.rowH).PaddingX(rowPaddingX).Radius(4)
	if e.Status == leader.Running {
		row.Background(t.Accent.Alpha(0.25))
	}
	row.Children(func() {
		mark(c, t, e.Status, m.markW)
		ui.Text(c, e.Key).Font(monospace).FontSize(fontSize).TextColor(keyColor).Width(m.keyW).NoWrap()
		ui.Text(c, e.Title).FontSize(fontSize).NoWrap()
	})
}

func mark(c *ui.Context, t *ui.Theme, st leader.Status, width float32) {
	var (
		text  string
		color ui.Color
	)
	switch st {
	case leader.Running:
		text, color = MarkRunning, t.Accent
	case leader.Done:
		text, color = MarkDone, t.Success
	case leader.Failed:
		text, color = MarkFailed, t.Danger
	default:
		ui.Box(c).Width(width)
		return
	}
	ui.Text(c, text).Font(monospace).FontSize(fontSize).TextColor(color).Width(width).NoWrap()
}
