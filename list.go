package main

// List rows. A row is built from segments (text + style + position in the
// commit's search corpus) laid out for the list width at render time. Two
// formats, tied to the layout:
//
//   - wide (rows layout): hash, author, subject, refs, date. The author column
//     is as wide as the longest name loaded (up to 15). Refs only take the
//     room they need, right-aligned against the date, and the subject gets
//     everything else, so every column starts at the same place on every row.
//     The email is not shown (the preview header has it) but stays searchable.
//   - compact (columns layout): hash, relative date, subject. Author, refs and
//     stats are in the preview header right next to it.

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	gutterW    = 2 // "▌ " on the selected row
	maxAuthorW = 15
	dateW      = 10
	relDateW   = 4
	minSubjW   = 24 // below this the wide format drops the author column
)

// seg is a run of text with one style. off is the byte offset of text in the
// commit's corpus, or -1 when the text is not part of it (padding, ellipsis).
type seg struct {
	text string
	st   lipgloss.Style
	off  int
}

func segsWidth(segs []seg) int {
	w := 0
	for _, s := range segs {
		w += ansi.StringWidth(s.text)
	}
	return w
}

func pad(n int) seg { return seg{text: strings.Repeat(" ", max(0, n)), off: -1} }

// fitSegs truncates segs to w cells (ending in "…" when cut) and pads the
// result to exactly w, on the left when alignRight is set.
func fitSegs(segs []seg, w int, alignRight bool) []seg {
	if w <= 0 {
		return nil
	}
	total := segsWidth(segs)
	if total > w {
		var out []seg
		room := w - 1 // keep a cell for the ellipsis
		last := lipgloss.NewStyle()
		for _, s := range segs {
			if room <= 0 {
				break
			}
			sw := ansi.StringWidth(s.text)
			if sw > room {
				s.text = ansi.Truncate(s.text, room, "")
				sw = ansi.StringWidth(s.text)
			}
			room -= sw
			last = s.st
			out = append(out, s)
		}
		out = append(out, seg{text: "…", st: last, off: -1})
		segs, total = out, segsWidth(out)
	}
	if total < w {
		if alignRight {
			return append([]seg{pad(w - total)}, segs...)
		}
		return append(segs, pad(w-total))
	}
	return segs
}

func refStyle(k refKind) lipgloss.Style {
	switch k {
	case refHead:
		return stRefHead
	case refBranch:
		return stRefBranch
	case refRemote:
		return stRefRemote
	case refTag:
		return stRefTag
	}
	return stRefOther
}

// refSegs lays the decorations out the way git's %C(auto)%D colors them.
func refSegs(c *commit) []seg {
	var segs []seg
	for i, r := range c.refs {
		if i > 0 {
			prev := c.refs[i-1]
			segs = append(segs, seg{text: ", ", off: prev.off + len(prev.text)})
		}
		if r.head {
			segs = append(segs, seg{text: headArrow, st: stRefHead, off: r.off - len(headArrow)})
		}
		segs = append(segs, seg{text: r.text, st: refStyle(r.kind), off: r.off})
	}
	return segs
}

// relDate is a compact age ("now", "5m", "3h", "2d", "3w", "5mo", "2y").
func relDate(when, now int64) string {
	d := now - when
	switch {
	case d < 60:
		return "now"
	case d < 3600:
		return strconv.FormatInt(d/60, 10) + "m"
	case d < 86400:
		return strconv.FormatInt(d/3600, 10) + "h"
	case d < 14*86400:
		return strconv.FormatInt(d/86400, 10) + "d"
	case d < 60*86400:
		return strconv.FormatInt(d/(7*86400), 10) + "w"
	case d < 365*86400:
		return strconv.FormatInt(d/(30*86400), 10) + "mo"
	}
	return strconv.FormatInt(d/(365*86400), 10) + "y"
}

// rowLayout is what a row needs to know about the list it is drawn in.
type rowLayout struct {
	width   int
	hashW   int   // widest short hash loaded (git lengthens ambiguous ones)
	authorW int   // widest author name loaded, capped at maxAuthorW
	compact bool  // columns layout
	now     int64 // reference for relative dates
}

// rowSegs builds one list row, exactly l.width cells wide.
func rowSegs(c *commit, l rowLayout, selected bool) []seg {
	gutter := seg{text: "  ", off: -1}
	if selected {
		gutter = seg{text: "▌ ", st: stCursor, off: -1}
	}
	space := seg{text: " ", off: -1}
	subjSt := lipgloss.NewStyle()
	switch {
	case c.wt:
		subjSt = stWorkTree
	case c.merge():
		subjSt = stMerge
	}
	subject := []seg{{text: c.subject(), st: subjSt, off: c.oSubject}}

	row := []seg{gutter}
	row = append(row, fitSegs([]seg{{text: c.short(), st: stHash, off: 0}}, l.hashW, false)...)
	row = append(row, space)
	rest := l.width - gutterW - l.hashW - 1

	if l.compact {
		age := ""
		if !c.wt {
			age = relDate(c.when, l.now)
		}
		row = append(row, fitSegs([]seg{{text: age, st: stDate, off: -1}}, relDateW, true)...)
		row = append(row, space)
		row = append(row, fitSegs(subject, rest-relDateW-1, false)...)
		return fitSegs(row, l.width, false)
	}

	// A narrow list drops the author.
	subjW := rest - (l.authorW + 1) - (dateW + 1)
	if l.authorW > 0 && subjW >= minSubjW {
		row = append(row, fitSegs([]seg{{text: c.author(), st: stAuthor, off: c.oAuthor}}, l.authorW, false)...)
		row = append(row, space)
	} else {
		subjW = rest - (dateW + 1)
	}
	// Refs take what they need, up to 45% of the subject area.
	if refs := refSegs(c); len(refs) > 0 && subjW > 12 {
		refsW := min(segsWidth(refs), subjW*45/100)
		row = append(row, fitSegs(subject, subjW-refsW-1, false)...)
		row = append(row, space)
		row = append(row, fitSegs(refs, refsW, true)...)
	} else {
		row = append(row, fitSegs(subject, subjW, false)...)
	}
	row = append(row, space)
	date := seg{text: c.date(), st: stDate, off: c.oDate}
	if c.wt {
		date = pad(dateW)
	}
	row = append(row, date)
	return fitSegs(row, l.width, false)
}

// renderSegs styles a row. matched holds corpus byte offsets to highlight;
// the selected row gets its background on every segment so the bar spans the
// whole list width without losing the column colors.
func renderSegs(segs []seg, matched []int, selected bool) string {
	hl := matchSet(matched)
	var b strings.Builder
	for _, s := range segs {
		st := s.st
		if selected {
			st = onSel(st)
		}
		if hl == nil || s.off < 0 {
			b.WriteString(st.Render(s.text))
			continue
		}
		b.WriteString(highlightFrom(s.text, s.off, hl, st))
	}
	return b.String()
}

func plainSegs(segs []seg) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.text)
	}
	return b.String()
}
