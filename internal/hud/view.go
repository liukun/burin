package hud

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"burin/internal/leader"
)

// Layout, in DIPs.
const (
	// MaxRows is the most entries a column holds; more entries make more
	// columns, as balanced as they go.
	MaxRows = 12

	fontSize    = 13
	padding     = 12
	headerGap   = 8
	columnGap   = 12
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
	m := measure(c, s)
	box := ui.Column(c).Fill().Padding(padding).Gap(headerGap)
	if !c.Vibrancy() {
		box.Background(t.Surface).Radius(cornerRadius)
	}
	box.Children(func() {
		header(c, t, s, m)
		switch {
		case s.Err != nil:
			ui.Text(c, s.Err.Error()).FontSize(fontSize).TextColor(t.Danger).Wrap().Width(m.errW)
		case len(s.Entries) > 0:
			entries(c, t, m)
		}
	})
	return m.width, m.height
}

// metrics are the sizes the layout is built from, measured from the text
// so that the window can be sized to fit it.
type metrics struct {
	lineH, rowH   float32
	markW, keyW   float32
	columns       [][]leader.EntryState
	columnW       []float32
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
		m.columns = split(s.Entries)
		for i, col := range m.columns {
			w := float32(0)
			for _, e := range col {
				tw, _ := text(e.Title, "", 0)
				w = max(w, tw)
			}
			colW := 2*rowPaddingX + m.markW + cellGap + m.keyW + cellGap + w + slack
			m.columnW = append(m.columnW, colW)
			contentW += colW
			if i > 0 {
				contentW += columnGap
			}
		}
		rows := float32(len(m.columns[0]))
		contentH = headerGap + rows*m.rowH + (rows-1)*rowGap
	}
	m.width = 2*padding + max(headerW, contentW)
	m.height = 2*padding + m.lineH + contentH
	return m
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

func entries(c *ui.Context, t *ui.Theme, m metrics) {
	ui.Row(c).Gap(columnGap).AlignItems(ui.Start).Children(func() {
		for i, col := range m.columns {
			ui.Column(c.Key(i)).Gap(rowGap).Width(m.columnW[i]).Children(func() {
				for _, e := range col {
					entry(c.Key(e.Key), t, e, m)
				}
			})
		}
	})
}

func entry(c *ui.Context, t *ui.Theme, e leader.EntryState, m metrics) {
	row := ui.Row(c).AlignItems(ui.Center).Gap(cellGap).Height(m.rowH).PaddingX(rowPaddingX).Radius(4)
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
