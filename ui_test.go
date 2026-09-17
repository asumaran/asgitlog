package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// testModel is a sized model over n synthetic commits, with settings
// persisted to a throwaway state dir and no delta. Init is never run, so no
// git process starts: renders are answered by hand (see settle).
func testModel(t *testing.T, n int) *model {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := newModel(loadPrefs(), "", logOpts{})
	batch := make([]commit, n)
	for i := range batch {
		batch[i] = mustParse(t, rec(fmt.Sprintf("%040d", i), fmt.Sprintf("%07d", i), "Ada", "ada@x.io",
			"01/01/2026 10:00", "", fmt.Sprintf("commit number %d", i)))
	}
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 43})
	m.Update(logBatch{commits: batch, done: true})
	return m
}

// settle answers every pending render (the wanted one, then the prefetched
// neighbors) with a body naming the render's key.
func settle(m *model) {
	for m.inflight != "" {
		k := m.inflight
		m.Update(previewMsg{key: k, hash: strings.SplitN(k, "|", 2)[0], render: render{content: "BODY OF " + k}})
	}
}

func press(m *model, keys ...string) {
	named := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
		"backspace": tea.KeyBackspace, "tab": tea.KeyTab, "f1": tea.KeyF1, "home": tea.KeyHome, "end": tea.KeyEnd}
	for _, k := range keys {
		var msg tea.KeyPressMsg
		mods := tea.KeyMod(0)
		for {
			if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
				k, mods = rest, mods|tea.ModCtrl
			} else if rest, ok := strings.CutPrefix(k, "shift+"); ok {
				k, mods = rest, mods|tea.ModShift
			} else if rest, ok := strings.CutPrefix(k, "alt+"); ok {
				k, mods = rest, mods|tea.ModAlt
			} else {
				break
			}
		}
		if code, ok := named[k]; ok {
			msg = tea.KeyPressMsg{Code: code, Mod: mods}
		} else if mods != 0 {
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Mod: mods}
		} else {
			msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
		}
		m.Update(msg)
	}
}

func typeText(m *model, s string) {
	for _, r := range s {
		press(m, string(r))
	}
}

// edges returns, for the rows layout, the divider over the details, the first
// details line and the main box's bottom edge.
func edges(m *model) [3]string {
	box := strings.Split(ansi.Strip(m.mainBox()), "\n")
	return [3]string{box[1+m.listH()], box[2+m.listH()], box[len(box)-1]}
}

func screen(m *model) string  { return ansi.Strip(m.View().Content) }
func lines(m *model) []string { return strings.Split(screen(m), "\n") }
func pref(name string) string { return readPref(name) }
func hashOf(i int) string     { return fmt.Sprintf("%040d", i) }
func keyAt(m *model, i int) string {
	w := m.activeVP().Width()
	return previewKey(hashOf(i), w, effectiveDiff(m.prefs.diff, w))
}

func TestGeometry(t *testing.T) {
	m := testModel(t, 100)
	// 43 lines: info box 3 + input box 3 + help box 3 + the main box's two
	// borders leave 32. Rows: the details get 70%, the divider 1, the list
	// the rest.
	if m.mainH() != 32 || m.detailsH() != 22 || m.listH() != 9 {
		t.Errorf("rows: main=%d details=%d list=%d", m.mainH(), m.detailsH(), m.listH())
	}
	if m.listW() != 158 || m.detailsW() != 158 || m.prevVP.Height() != 22 || m.prevVP.Width() != 156 {
		t.Errorf("rows widths: list=%d details=%d vp=%dx%d", m.listW(), m.detailsW(), m.prevVP.Width(), m.prevVP.Height())
	}
	press(m, "ctrl+l")
	// Columns: list (25%) and details (75%) at full height, a divider between
	// them.
	if m.detailsW() != 118 || m.listW() != 39 || m.listH() != 32 || m.detailsH() != 32 || m.prevVP.Height() != 32 || m.prevVP.Width() != 116 {
		t.Errorf("columns: list=%dx%d details=%dx%d vp=%dx%d", m.listW(), m.listH(), m.detailsW(), m.detailsH(), m.prevVP.Width(), m.prevVP.Height())
	}
	for _, size := range [][2]int{{160, 43}, {80, 24}, {61, 18}, {40, 16}} {
		for range 2 {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			// With the help folded and expanded: the frame always fits.
			for _, view := range []string{"short help", "full help"} {
				ls := lines(m)
				if len(ls) != size[1] {
					t.Errorf("%v layout=%s %s: view has %d lines", size, m.prefs.layout, view, len(ls))
				}
				for _, l := range ls {
					if w := ansi.StringWidth(l); w > size[0] {
						t.Errorf("%v layout=%s %s: line is %d cells: %q", size, m.prefs.layout, view, w, l)
					}
				}
				press(m, "f1")
			}
			press(m, "ctrl+l")
		}
	}
}

func TestRowsLayoutIsBottomUp(t *testing.T) {
	m := testModel(t, 5)
	ls := lines(m)
	// Four boxes: summary, input (counter on its top edge), main, help.
	if !strings.HasPrefix(ls[0], "╭─") || !strings.HasPrefix(ls[2], "╰─") || !strings.HasSuffix(ls[3], " 5/5 ─╮") ||
		!strings.HasPrefix(ls[4], "│ asgitlog") || !strings.HasPrefix(ls[6], "╭─") || !strings.HasPrefix(ls[40], "╭─") ||
		!strings.HasPrefix(ls[41], "│ type filter") || !strings.HasPrefix(ls[42], "╰─") {
		t.Errorf("boxes:\n%s", screen(m))
	}
	for i, l := range ls {
		if ansi.StringWidth(l) != 160 {
			t.Errorf("line %d is %d cells wide: %q", i, ansi.StringWidth(l), l)
		}
	}
	last := m.listY() + m.listH() - 1
	if m.listY() != 7 || !strings.HasPrefix(ls[last], "│▌ 0000000") || !strings.HasPrefix(ls[last-1], "│  0000001") {
		t.Errorf("newest commit should sit right above the divider:\n%s\n%s", ls[last-1], ls[last])
	}
	if strings.Trim(ls[m.listY()], " │") != "" {
		t.Errorf("a short list leaves the top of the list area blank: %q", ls[m.listY()])
	}
	if !strings.HasPrefix(ls[last+1], "├─ delta not found") || !strings.HasSuffix(ls[last+1], "─┤") || !strings.HasPrefix(ls[last+2], "│ commit ") {
		t.Errorf("the divider and the details go below the list:\n%s\n%s", ls[last+1], ls[last+2])
	}
	press(m, "up")
	if m.cursor != 1 {
		t.Errorf("up should move to the older commit above, cursor=%d", m.cursor)
	}
	press(m, "down", "down")
	if m.cursor != 0 {
		t.Errorf("down should come back to the newest, cursor=%d", m.cursor)
	}
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: last - 3})
	if m.cursor != 3 {
		t.Errorf("click on the fourth line from the bottom should select row 3, got %d", m.cursor)
	}
	for _, y := range []int{1, 4, last + 1, last + 10} { // summary, input, divider, details
		m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: y})
	}
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 0, Y: last}) // the border
	if m.cursor != 3 {
		t.Errorf("clicks outside the list moved the cursor to %d", m.cursor)
	}
}

func TestColumnsLayoutIsTopDown(t *testing.T) {
	m := testModel(t, 5)
	press(m, "ctrl+l")
	ls := lines(m)
	y := m.listY()
	// Compact rows: hash, relative date, subject; a vertical divider to the
	// details, tied into the top and bottom edges.
	if !strings.HasPrefix(ls[y], "│▌ 0000000 ") || !strings.Contains(ls[y], " commit number 0") || strings.Contains(ls[y], "Ada") {
		t.Errorf("first list line = %q", ls[y])
	}
	col := ansi.StringWidth(ls[y][:strings.Index(ls[y], "│ commit ")])
	if col != 1+m.listW() || !strings.Contains(ls[y-1], "┬─ delta not found") || !strings.Contains(ls[y+m.listH()], "┴─") ||
		ansi.StringWidth(ls[y-1][:strings.Index(ls[y-1], "┬")]) != col {
		t.Errorf("divider should run the full height at column %d:\n%s\n%s", col, ls[y-1], ls[y])
	}
	for i, l := range ls {
		if ansi.StringWidth(l) != 160 {
			t.Errorf("line %d is %d cells wide: %q", i, ansi.StringWidth(l), l)
		}
	}
	press(m, "down")
	if m.cursor != 1 {
		t.Errorf("down should move to the older commit below, cursor=%d", m.cursor)
	}
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: y + 3})
	if m.cursor != 3 {
		t.Errorf("click on the fourth list line should select row 3, got %d", m.cursor)
	}
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: m.listW() + 5, Y: y}) // over the details
	if m.cursor != 3 {
		t.Errorf("a click on the details moved the cursor to %d", m.cursor)
	}
}

func TestLayoutTogglePersistsAndKeepsCommit(t *testing.T) {
	m := testModel(t, 100)
	press(m, "up", "up", "up", "pgup")
	want := m.current().hash
	if m.cursor != 3+m.listH() {
		t.Fatalf("cursor = %d", m.cursor)
	}
	press(m, "ctrl+l")
	if m.prefs.layout != layoutColumns || pref("layout") != layoutColumns {
		t.Errorf("layout not switched/persisted: %q / %q", m.prefs.layout, pref("layout"))
	}
	if m.current().hash != want {
		t.Error("cursor moved to another commit on layout change")
	}
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	if m.current().hash != want {
		t.Error("cursor moved to another commit on resize")
	}
	if m.cursor < m.top || m.cursor >= m.top+m.listH() {
		t.Errorf("cursor %d outside the window [%d,%d)", m.cursor, m.top, m.top+m.listH())
	}
	if !strings.Contains(screen(m), "▌ "+m.current().short()) {
		t.Error("selected row not on screen")
	}
	press(m, "ctrl+l")
	if pref("layout") != layoutRows {
		t.Error("layout not persisted back to rows")
	}
}

func TestResizeList(t *testing.T) {
	m := testModel(t, 50)
	h := m.listH()
	press(m, "shift+right")
	if m.listH() <= h || m.prefs.splitRows != 65 || pref("split-rows") != "65" {
		t.Errorf("rows: list %d -> %d, split=%d (%q)", h, m.listH(), m.prefs.splitRows, pref("split-rows"))
	}
	press(m, "ctrl+l")
	w := m.listW()
	press(m, "shift+left")
	if m.listW() >= w || m.prefs.splitColumns != 80 || pref("split-columns") != "80" || m.prevVP.Width() != m.detailsW()-2 {
		t.Errorf("columns: list %d -> %d, split=%d", w, m.listW(), m.prefs.splitColumns)
	}
	for range 20 {
		press(m, "shift+left")
	}
	if m.prefs.splitColumns != splitMax {
		t.Errorf("split should clamp at %d, got %d", splitMax, m.prefs.splitColumns)
	}
}

func TestDiffModeCycle(t *testing.T) {
	m := testModel(t, 5)
	m.deltaBin = "/usr/bin/delta"
	top := func() string { return edges(m)[0] }
	if !strings.HasPrefix(top(), "├─ auto") || !strings.Contains(top(), "─ auto: side-by-side ─") || !strings.HasSuffix(m.wantKey, "|sbs") {
		t.Errorf("default (156 wide): %q key=%q", top(), m.wantKey)
	}
	press(m, "ctrl+t")
	if m.prefs.diff != diffSBS || !strings.Contains(top(), "─ side-by-side ─") || strings.Contains(top(), "auto") {
		t.Errorf("after one ctrl+t: %q", top())
	}
	press(m, "ctrl+t")
	if pref("diff") != diffSingle || !strings.Contains(top(), "─ single column ─") || !strings.HasSuffix(m.wantKey, "|single") {
		t.Errorf("after two: %q key=%q", top(), m.wantKey)
	}
	press(m, "ctrl+t")
	if pref("diff") != diffAuto {
		t.Errorf("third ctrl+t should be back to auto, got %q", pref("diff"))
	}
	// Auto follows the preview width.
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if !strings.Contains(top(), "auto: single column") || !strings.HasSuffix(m.wantKey, "|single") {
		t.Errorf("auto at 96 columns: %q key=%q", top(), m.wantKey)
	}
}

func TestFilterFlow(t *testing.T) {
	m := testModel(t, 200)
	press(m, "up", "up")
	typeText(m, "number 15")
	// Substring terms: 15, 115 and 150..159, in log order.
	if m.rowCount() != 12 || m.cursor != 0 || m.current().subject() != "commit number 15" {
		t.Fatalf("hits=%d cursor=%d current=%q", m.rowCount(), m.cursor, m.current().subject())
	}
	for i := 1; i < m.rowCount(); i++ {
		a, _ := m.rowAt(i - 1)
		b, _ := m.rowAt(i)
		if a.hash >= b.hash {
			t.Fatalf("hits out of log order at %d", i)
		}
	}
	if line := lines(m)[infoBoxH]; !strings.HasSuffix(line, "─ 12/200 ─╮") {
		t.Errorf("counter on the input box: %q", line)
	}
	press(m, "up")
	keep := m.current().hash
	if m.current().subject() != "commit number 115" {
		t.Errorf("up should move within the hits, on %q", m.current().subject())
	}
	for range len("number 15") {
		press(m, "backspace")
	}
	if m.filtering() || m.rowCount() != 200 {
		t.Errorf("query not cleared: %q", m.query)
	}
	if m.current().hash != keep {
		t.Error("clearing the query must stay on the selected commit")
	}
	typeText(m, "zzzz")
	if m.current() != nil || !strings.Contains(screen(m), "no matching commits") {
		t.Errorf("empty result not handled:\n%s", screen(m))
	}
}

func TestFilterIncludesCommitsStreamedLater(t *testing.T) {
	m := testModel(t, 10)
	typeText(m, "fresh")
	if m.rowCount() != 0 {
		t.Fatalf("unexpected hits: %d", m.rowCount())
	}
	late := mustParse(t, rec(strings.Repeat("f", 40), "fffffff", "Ada", "ada@x.io", "01/01/2026 10:00", "", "a fresh commit"))
	m.loading, m.logCh = true, make(chan logBatch)
	m.Update(logBatch{commits: []commit{late}, done: true})
	if m.rowCount() != 1 || m.current().subject() != "a fresh commit" {
		t.Errorf("streamed commit not filtered in: rows=%d", m.rowCount())
	}
	m.Update(logBatch{gen: 99, commits: []commit{late}})
	if len(m.commits) != 11 {
		t.Error("batches of a replaced stream must be dropped")
	}
}

func TestPreviewSingleFlightAndPrefetch(t *testing.T) {
	m := testModel(t, 10)
	a := keyAt(m, 0)
	if m.inflight != a || m.wantKey != a {
		t.Fatalf("first render not started: inflight=%q want=%q", m.inflight, m.wantKey)
	}
	if s := screen(m); !strings.Contains(s, "rendering…") || !strings.Contains(s, "commit "+hashOf(0)) {
		t.Errorf("placeholder should carry the instant header:\n%s", s)
	}
	cancelled := false
	m.cancelRender = func() { cancelled = true }
	press(m, "up")
	b := keyAt(m, 1)
	if !cancelled || m.inflight != a || m.wantKey != b {
		t.Fatalf("moving on must cancel, not stack: cancelled=%v inflight=%q", cancelled, m.inflight)
	}
	// The cancelled render reports back: the wanted one starts.
	m.Update(previewMsg{key: a, cancelled: true, err: fmt.Errorf("killed")})
	if m.inflight != b || len(m.failed) != 0 {
		t.Fatalf("wanted render not started: inflight=%q failed=%v", m.inflight, m.failed)
	}
	m.Update(previewMsg{key: b, hash: hashOf(1), render: render{content: "HEADER\n\nTHE DIFF"}})
	if !strings.Contains(screen(m), "THE DIFF") {
		t.Errorf("render not shown:\n%s", screen(m))
	}
	// Served: the neighbors are rendered ahead, one at a time, older first.
	if m.inflight != keyAt(m, 2) {
		t.Fatalf("prefetch should start with the older neighbor, inflight=%q", m.inflight)
	}
	settle(m)
	for _, i := range []int{0, 1, 2} {
		if _, ok := m.renders[keyAt(m, i)]; !ok {
			t.Errorf("commit %d not cached after the prefetch", i)
		}
	}
	if _, ok := m.renders[keyAt(m, 3)]; ok {
		t.Error("prefetch must stop at the direct neighbors")
	}
	press(m, "up")
	if !strings.Contains(screen(m), "BODY OF "+keyAt(m, 2)) || strings.Contains(screen(m), "rendering…") {
		t.Errorf("a prefetched neighbor should show instantly:\n%s", screen(m))
	}
}

func TestPreviewErrorIsShownOnce(t *testing.T) {
	m := testModel(t, 3)
	m.Update(previewMsg{key: m.wantKey, hash: hashOf(0), err: fmt.Errorf("delta exploded")})
	if !strings.Contains(screen(m), "delta exploded") {
		t.Errorf("error not surfaced:\n%s", screen(m))
	}
	settle(m)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 43}) // any event that re-evaluates the preview
	if m.inflight != "" || !strings.Contains(screen(m), "delta exploded") {
		t.Errorf("a failed render must not be retried in a loop: inflight=%q", m.inflight)
	}
}

func TestPreviewBoxEdges(t *testing.T) {
	m := testModel(t, 3)
	body := make([]string, 100)
	for i := range body {
		body[i] = fmt.Sprintf("line %02d", i)
	}
	body[29], body[30], body[31] = "", "src/ui.go", strings.Repeat("─", 40)
	body[59], body[60], body[61] = "", "src/list.go", strings.Repeat("─", 40)
	content := strings.Join(body, "\n")
	m.Update(previewMsg{key: m.wantKey, hash: hashOf(0), render: render{content, fileLines(content)}})
	box := edges(m)
	if strings.Contains(box[0], "0000000") || !strings.HasSuffix(box[2], " 22/100 ─╯") {
		t.Errorf("at the top:\n%s\n%s", box[0], box[2])
	}
	press(m, "tab")
	box = edges(m)
	if m.prevVP.YOffset() != 30 || !strings.HasPrefix(box[1], "│ src/ui.go") {
		t.Errorf("tab should jump to the first file: offset=%d %q", m.prevVP.YOffset(), box[1])
	}
	if !strings.HasPrefix(box[0], "├─ delta not found: plain git colors  0000000 commit number 0 ─") || !strings.HasSuffix(box[2], " 52/100 ─╯") {
		t.Errorf("scrolled: the commit goes on the divider, the position on the bottom edge:\n%s\n%s", box[0], box[2])
	}
	press(m, "tab", "tab")
	if m.prevVP.YOffset() != 60 {
		t.Errorf("tab past the last file stays there, offset=%d", m.prevVP.YOffset())
	}
	press(m, "shift+tab")
	if m.prevVP.YOffset() != 30 {
		t.Errorf("shift+tab should go back to the previous file, offset=%d", m.prevVP.YOffset())
	}
	press(m, "shift+tab")
	if m.prevVP.YOffset() != 0 {
		t.Errorf("shift+tab before the first file goes to the top, offset=%d", m.prevVP.YOffset())
	}
}

func TestFullViewNavigationAndSearch(t *testing.T) {
	m := testModel(t, 3)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	settle(m)
	press(m, "enter")
	if m.mode != modeFull {
		t.Fatal("enter should open the full view")
	}
	body := make([]string, 60)
	for i := range body {
		body[i] = fmt.Sprintf("line %02d", i)
	}
	body[40] = "\x1b[32mline 40 has a Needle in it\x1b[0m"
	body[50] = "another needle, and a second NEEDLE"
	m.Update(previewMsg{key: m.inflight, hash: hashOf(0), render: render{content: strings.Join(body, "\n")}})
	settle(m)
	if s := screen(m); !strings.Contains(s, "line 00") || strings.Contains(s, "asgitlog") || !strings.Contains(s, "back") || !strings.Contains(s, "1/3  0000000") {
		t.Errorf("full view:\n%s", s)
	}
	press(m, "j", "j")
	if m.fullVP.YOffset() != 2 {
		t.Errorf("j should scroll, offset=%d", m.fullVP.YOffset())
	}
	press(m, "G")
	if !m.fullVP.AtBottom() {
		t.Error("G should go to the bottom")
	}
	press(m, "g", "/")
	if m.mode != modeSearch {
		t.Fatal("/ should open the search input")
	}
	typeText(m, "needle")
	press(m, "enter")
	if m.mode != modeFull || len(m.matchLines) != 2 || m.matchLines[0] != 40 {
		t.Fatalf("search state: mode=%v lines=%v", m.mode, m.matchLines)
	}
	if s := screen(m); !strings.Contains(s, "line 40 has a Needle") || !strings.Contains(s, "match 1/2") {
		t.Errorf("first match not shown:\n%s", s)
	}
	if !strings.Contains(m.fullVP.GetContent(), "\x1b[7m") {
		t.Error("matches should be highlighted")
	}
	press(m, "n")
	if !strings.Contains(screen(m), "second NEEDLE") || m.matchIdx != 1 {
		t.Errorf("n should reach the next match:\n%s", screen(m))
	}
	press(m, "n")
	if m.matchIdx != 0 {
		t.Error("n should wrap around")
	}
	press(m, "esc")
	if m.mode != modeFull || m.searchTerm != "" || strings.Contains(m.fullVP.GetContent(), "\x1b[7m") {
		t.Error("esc should clear the search first")
	}

	// Commit to commit without leaving the full view.
	press(m, "]")
	if s := screen(m); m.cursor != 1 || !strings.Contains(s, "2/3  0000001") || !strings.Contains(s, "BODY OF "+keyAt(m, 1)) {
		t.Errorf("] should open the older commit:\n%s", s)
	}
	press(m, "right", "right", "right")
	if m.cursor != 2 {
		t.Errorf("] stops at the oldest commit, cursor=%d", m.cursor)
	}
	press(m, "[", "left")
	if m.cursor != 0 || m.fullVP.YOffset() != 0 {
		t.Errorf("[ should come back to the newest at the top, cursor=%d offset=%d", m.cursor, m.fullVP.YOffset())
	}

	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	settle(m)
	h := m.fullVP.Height()
	press(m, "?")
	if s := screen(m); !m.help.ShowAll || !strings.Contains(s, "newer/older") || !strings.Contains(s, "copy the hash") ||
		m.fullVP.Height() != h-3 || len(lines(m)) != 30 {
		t.Errorf("? expands the help in place (4 lines) and the viewport gives way (%d -> %d):\n%s", h, m.fullVP.Height(), s)
	}
	press(m, "esc")
	if m.mode != modeFull || m.help.ShowAll || m.fullVP.Height() != h {
		t.Error("esc folds the help first, staying in the full view")
	}
	press(m, "esc")
	settle(m)
	if s := screen(m); m.mode != modeList || !strings.Contains(s, "asgitlog") || !strings.Contains(s, "BODY OF "+keyAt(m, 0)) {
		t.Errorf("esc should return to the list with its preview restored:\n%s", s)
	}
}

func TestHelpKeyOnlyWithEmptyQuery(t *testing.T) {
	m := testModel(t, 3)
	mainH := m.mainH()
	press(m, "?")
	s := screen(m)
	if !m.help.ShowAll || !strings.Contains(s, "search the diffs") || !strings.Contains(s, "~word") || strings.Contains(s, "• enter full diff") {
		t.Fatalf("? on an empty query should expand the help:\n%s", s)
	}
	if m.mainH() != mainH-4 || len(lines(m)) != 43 || !strings.HasPrefix(lines(m)[42], "╰─") || !strings.HasPrefix(lines(m)[36], "╭─") {
		t.Errorf("the main box gives the help box its 5 lines: main %d->%d\n%s", mainH, m.mainH(), s)
	}
	press(m, "esc")
	if m.help.ShowAll || m.mainH() != mainH || !strings.Contains(screen(m), "• enter full diff") {
		t.Fatal("esc should only fold the help")
	}
	typeText(m, "why?")
	if m.help.ShowAll || m.ti.Value() != "why?" {
		t.Errorf("? inside a query is just text: help=%v value=%q", m.help.ShowAll, m.ti.Value())
	}
	press(m, "f1", "f1")
	if m.help.ShowAll || m.ti.Value() != "why?" {
		t.Error("f1 toggles the help whatever the query")
	}
}

func TestCopyAndBrowse(t *testing.T) {
	m := testModel(t, 3)
	dir := t.TempDir()
	stub := filepath.Join(dir, "stub")
	os.WriteFile(stub, []byte("#!/bin/sh\n{ echo \"args:$*\"; cat; echo; } >> \""+dir+"/log\"\n"), 0o755)
	t.Setenv("ASGITLOG_CLIPBOARD", stub)
	t.Setenv("ASGITLOG_OPENER", stub)
	run := func(cmd tea.Cmd) {
		t.Helper()
		if cmd == nil {
			t.Fatal("expected a command")
		}
		m.Update(cmd())
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	run(cmd)
	if !strings.Contains(screen(m), "copied "+hashOf(0)) {
		t.Errorf("flash missing:\n%s", lines(m)[len(lines(m))-1])
	}
	m.Update(clearFlashMsg(m.flashSeq - 1))
	if m.flash == "" {
		t.Error("an older timer must not clear a newer message")
	}
	m.Update(clearFlashMsg(m.flashSeq))
	if m.flash != "" || !strings.Contains(screen(m), "full diff") {
		t.Error("the help line should come back")
	}

	press(m, "ctrl+o")
	if !strings.Contains(screen(m), "no remote with a web URL") {
		t.Errorf("without a remote:\n%s", lines(m)[len(lines(m))-1])
	}
	m.Update(repoInfoMsg{Top: "/r", Branch: "main", WebURL: "https://github.com/a/b"})
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	run(cmd)
	logged, _ := os.ReadFile(filepath.Join(dir, "log"))
	if !strings.Contains(string(logged), hashOf(0)+"\n") || !strings.Contains(string(logged), "args:https://github.com/a/b/commit/"+hashOf(0)) {
		t.Errorf("stub log:\n%s", logged)
	}
}

func TestScopeAndPickaxeInput(t *testing.T) {
	m := testModel(t, 3)
	m.Update(repoInfoMsg{Top: "/r", Branch: "main"})
	m.opts.paths = []string{"src"}
	press(m, "ctrl+g")
	if m.mode != modePickaxe || !strings.Contains(screen(m), "search the diffs (-S) ❯") {
		t.Fatalf("ctrl+g should open the content search input:\n%s", screen(m))
	}
	typeText(m, "needle")
	if m.ti.Value() != "" || m.pi.Value() != "needle" {
		t.Errorf("typing goes to the search input: filter=%q search=%q", m.ti.Value(), m.pi.Value())
	}
	press(m, "esc")
	if m.mode != modeList || m.opts.pickaxe != "" || m.logGen != 0 {
		t.Errorf("esc cancels: mode=%v pickaxe=%q gen=%d", m.mode, m.opts.pickaxe, m.logGen)
	}
	if s := lines(m)[infoBoxH]; !strings.HasSuffix(s, "─ 3/3 [-- src] ─╮") {
		t.Errorf("scope next to the counter: %q", s)
	}
}

func TestLogErrorAndFatal(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := newModel(loadPrefs(), "", logOpts{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m.Update(logBatch{done: true, err: fmt.Errorf("fatal: your current branch 'main' does not have any commits yet")})
	if !strings.Contains(screen(m), "does not have any commits yet") || m.loading {
		t.Errorf("log error not shown:\n%s", screen(m))
	}
	press(m, "enter") // nothing selected: must not open the full view
	if m.mode != modeList {
		t.Error("enter without a commit should do nothing")
	}

	f := newModel(loadPrefs(), "", logOpts{})
	f.mode, f.fatal = modeFatal, "not inside a git work tree: /tmp"
	if !strings.Contains(screen(f), "not inside a git work tree") {
		t.Errorf("fatal view:\n%s", screen(f))
	}
	if _, cmd := f.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}); cmd == nil {
		t.Error("any key should quit the fatal view")
	}
}

func TestBackToTheNewestCommit(t *testing.T) {
	m := testModel(t, 300)
	typeText(m, "number 250")
	for range len("number 250") {
		press(m, "backspace")
	}
	if m.cursor != 250 {
		t.Fatalf("clearing the query should stay on the found commit, cursor=%d", m.cursor)
	}
	press(m, "home")
	if m.cursor != 0 || m.top != 0 || m.ti.Value() != "" {
		t.Errorf("home should select the newest commit, cursor=%d top=%d", m.cursor, m.top)
	}
	press(m, "end")
	if m.cursor != 299 {
		t.Errorf("end should select the oldest commit, cursor=%d", m.cursor)
	}
	// For keyboards without home/end: alt+arrows, by screen direction. The
	// rows layout is bottom-up, so the newest commit is at the bottom.
	press(m, "alt+down")
	if m.cursor != 0 {
		t.Errorf("rows: alt+down should reach the bottom of the list (newest), cursor=%d", m.cursor)
	}
	press(m, "alt+up")
	if m.cursor != 299 {
		t.Errorf("rows: alt+up should reach the top of the list (oldest), cursor=%d", m.cursor)
	}
	press(m, "ctrl+l", "alt+up")
	if m.cursor != 0 {
		t.Errorf("columns: alt+up should reach the top of the list (newest), cursor=%d", m.cursor)
	}
	press(m, "ctrl+l")

	// The wheel over the list moves the selection; elsewhere it scrolls the
	// diff. Rows layout: up the screen is down the log.
	wheel := func(b tea.MouseButton, x, y int) { m.Update(tea.MouseWheelMsg{Button: b, X: x, Y: y}) }
	y := m.listY() + 2
	wheel(tea.MouseWheelUp, 10, y)
	wheel(tea.MouseWheelUp, 10, y)
	if m.cursor != 2 {
		t.Errorf("rows: wheel up over the list should go to older commits, cursor=%d", m.cursor)
	}
	wheel(tea.MouseWheelDown, 10, y)
	if m.cursor != 1 {
		t.Errorf("rows: wheel down should come back, cursor=%d", m.cursor)
	}
	settle(m)
	body := strings.Repeat("line\n", 200)
	m.renders[m.wantKey] = render{content: body}
	m.shownKey = ""
	m.updatePreview()
	wheel(tea.MouseWheelDown, 10, m.listY()+m.listH()+5) // over the details
	if m.cursor != 1 || m.prevVP.YOffset() == 0 {
		t.Errorf("wheel over the details should scroll them: cursor=%d offset=%d", m.cursor, m.prevVP.YOffset())
	}
	press(m, "ctrl+l") // columns: top-down
	wheel(tea.MouseWheelDown, 10, m.listY()+2)
	if m.cursor != 2 {
		t.Errorf("columns: wheel down over the list should go to older commits, cursor=%d", m.cursor)
	}
	wheel(tea.MouseWheelDown, m.listW()+10, m.listY()+2) // right of the divider
	if m.cursor != 2 {
		t.Errorf("columns: wheel over the details moved the cursor to %d", m.cursor)
	}
}
