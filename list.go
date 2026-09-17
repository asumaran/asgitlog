package main

// List rows. A row is built from segments (text + style + position in the
// commit's search corpus) laid out for the list width at render time. Two
// formats, tied to the layout:
//
//   - wide (rows layout): hash, author <email>, subject, refs, date. Name and
//     email get fixed widths (15 + 14; the email is a quick hint, not meant
//     to be read whole), refs sit in their own right-aligned 28-wide column
//     before the date, and the subject takes what is left, so every column
//     starts at the same place on every row.
//   - compact (columns layout): hash, date, subject. Author, refs and stats
//     are in the preview header right next to it.

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	gutterW  = 2 // "▌ " on the selected row
	authorW  = 15
	emailW   = 14
	refsW    = 28
	dateW    = 10
	minSubjW = 20 // below this the wide format starts dropping columns
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

// rowSegs builds one list row, exactly width cells wide. hashW is the widest
// short hash loaded so far (git lengthens ambiguous abbreviations).
func rowSegs(c *commit, width, hashW int, compact, selected bool) []seg {
	gutter := seg{text: "  ", off: -1}
	if selected {
		gutter = seg{text: "▌ ", st: stCursor, off: -1}
	}
	space := seg{text: " ", off: -1}
	hash := fitSegs([]seg{{text: c.short(), st: stHash, off: 0}}, hashW, false)
	date := seg{text: c.date(), st: stDate, off: c.oDate}
	subject := []seg{{text: c.subject(), off: c.oSubject}}

	row := []seg{gutter}
	row = append(row, hash...)
	row = append(row, space)
	rest := width - gutterW - hashW - 1

	if compact {
		row = append(row, date, space)
		row = append(row, fitSegs(subject, rest-dateW-1, false)...)
		return fitSegs(row, width, false)
	}

	// Narrow terminals drop the refs column first, then the author.
	subjW := rest - (authorW + emailW + 4) - (refsW + 1) - (dateW + 1)
	showRefs, showAuthor := true, true
	if subjW < minSubjW {
		showRefs = false
		subjW += refsW + 1
	}
	if subjW < minSubjW {
		showAuthor = false
		subjW += authorW + emailW + 4
	}
	if showAuthor {
		row = append(row, fitSegs([]seg{{text: c.author(), st: stAuthor, off: c.oAuthor}}, authorW, false)...)
		row = append(row, seg{text: " <", st: stAuthor, off: c.oEmail - 2})
		row = append(row, fitSegs([]seg{{text: c.email(), st: stAuthor, off: c.oEmail}}, emailW, false)...)
		row = append(row, seg{text: ">", st: stAuthor, off: c.oSubject - 2}, space)
	}
	row = append(row, fitSegs(subject, subjW, false)...)
	row = append(row, space)
	if showRefs {
		row = append(row, fitSegs(refSegs(c), refsW, true)...)
		row = append(row, space)
	}
	row = append(row, date)
	return fitSegs(row, width, false)
}

// renderSegs styles a row. matched holds corpus byte offsets to highlight;
// the selected row gets its background on every segment so the bar spans the
// whole list width without losing the column colors.
func renderSegs(segs []seg, matched []int, selected bool) string {
	var hl map[int]bool
	if len(matched) > 0 {
		hl = make(map[int]bool, len(matched))
		for _, i := range matched {
			hl[i] = true
		}
	}
	var b strings.Builder
	for _, s := range segs {
		st := s.st
		if selected {
			st = st.Background(selBg).Bold(true)
		}
		if hl == nil || s.off < 0 {
			b.WriteString(st.Render(s.text))
			continue
		}
		// Split the segment into highlighted and plain runs.
		mst := st.Foreground(matchFg).Underline(true)
		start, cur := 0, false
		flush := func(end int) {
			if end > start {
				if cur {
					b.WriteString(mst.Render(s.text[start:end]))
				} else {
					b.WriteString(st.Render(s.text[start:end]))
				}
			}
			start = end
		}
		for i := range s.text { // i is a byte offset, like the matcher's
			if on := hl[s.off+i]; on != cur {
				flush(i)
				cur = on
			}
		}
		flush(len(s.text))
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
