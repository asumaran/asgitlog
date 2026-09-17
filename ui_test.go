package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// testModel is a sized model over n synthetic commits, with settings
// persisted to a throwaway state dir and no delta.
func testModel(t *testing.T, n int) *model {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := newModel(loadPrefs(), "")
	batch := make([]commit, n)
	for i := range batch {
		batch[i] = mustParse(t, rec(fmt.Sprintf("%040d", i), fmt.Sprintf("%07d", i), "Ada", "ada@x.io",
			"01/01/2026 10:00", "", fmt.Sprintf("commit number %d", i)))
	}
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 43})
	m.Update(logBatch{commits: batch, done: true})
	return m
}

func press(m *model, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "pgdown":
			msg = tea.KeyPressMsg{Code: tea.KeyPgDown}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		default:
			if rest, ok := strings.CutPrefix(k, "ctrl+"); ok {
				msg = tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
			} else {
				msg = tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
			}
		}
		m.Update(msg)
	}
}

func typeText(m *model, s string) {
	for _, r := range s {
		press(m, string(r))
	}
}

func screen(m *model) string { return ansi.Strip(m.View().Content) }

func TestGeometry(t *testing.T) {
	m := testModel(t, 100)
	// 43 lines: input + 40 body + info + help. Rows: preview gets 70%.
	if m.bodyH() != 40 || m.previewBlockH() != 28 || m.listH() != 12 {
		t.Errorf("rows: body=%d preview=%d list=%d", m.bodyH(), m.previewBlockH(), m.listH())
	}
	if m.listW() != 160 || m.prevW() != 160 || m.prevVP.Height() != 27 {
		t.Errorf("rows widths: list=%d prev=%d vpH=%d", m.listW(), m.prevW(), m.prevVP.Height())
	}
	press(m, "ctrl+l")
	if m.prevW() != 120 || m.listW() != 37 || m.listH() != 40 || m.prevVP.Height() != 39 {
		t.Errorf("columns: list=%dx%d prev=%d vpH=%d", m.listW(), m.listH(), m.prevW(), m.prevVP.Height())
	}
	for _, size := range [][2]int{{160, 43}, {80, 24}, {40, 10}, {30, 6}} {
		for range 2 {
			m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			lines := strings.Split(screen(m), "\n")
			if len(lines) != size[1] {
				t.Errorf("%v layout=%s: view has %d lines", size, m.prefs.layout, len(lines))
			}
			for _, l := range lines {
				if w := ansi.StringWidth(l); w > size[0] {
					t.Errorf("%v layout=%s: line is %d cells: %q", size, m.prefs.layout, w, l)
				}
			}
			press(m, "ctrl+l")
		}
	}
}

func TestLayoutTogglePersistsAndKeepsCommit(t *testing.T) {
	m := testModel(t, 100)
	press(m, "down", "down", "down", "pgdown")
	want := m.current().hash
	press(m, "ctrl+l")
	if m.prefs.layout != layoutColumns || loadPrefs().layout != layoutColumns {
		t.Errorf("layout not switched/persisted: %q / %q", m.prefs.layout, loadPrefs().layout)
	}
	if m.current().hash != want {
		t.Error("cursor moved to another commit on layout change")
	}
	if !strings.Contains(screen(m), "▌ "+m.current().short()+" 01/01/2026") {
		t.Errorf("columns layout should list compact rows:\n%s", screen(m))
	}
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 20})
	if m.current().hash != want {
		t.Error("cursor moved to another commit on resize")
	}
	if m.cursor < m.top || m.cursor >= m.top+m.listH() {
		t.Errorf("cursor %d outside the window [%d,%d)", m.cursor, m.top, m.top+m.listH())
	}
	press(m, "ctrl+l")
	if loadPrefs().layout != layoutRows {
		t.Error("layout not persisted back to rows")
	}
}

func TestDiffModeToggle(t *testing.T) {
	m := testModel(t, 5)
	m.deltaBin = "/usr/bin/delta"
	if !strings.Contains(m.labelRule(80), "side-by-side") {
		t.Errorf("default label: %q", ansi.Strip(m.labelRule(80)))
	}
	before := m.wantKey
	press(m, "ctrl+t")
	if m.prefs.diff != diffSingle || loadPrefs().diff != diffSingle {
		t.Error("diff mode not switched/persisted")
	}
	if m.wantKey == before || !strings.HasSuffix(m.wantKey, "|single") {
		t.Errorf("preview key should follow the mode: %q", m.wantKey)
	}
	if !strings.Contains(screen(m), "single column") {
		t.Error("label should show the current mode")
	}
}

func TestFilterFlow(t *testing.T) {
	m := testModel(t, 200)
	press(m, "down", "down")
	typeText(m, "number 15")
	// 15, 115, 150..159 and the scattered 1…5 matches, in log order.
	if c := m.current(); c == nil || m.cursor != 0 {
		t.Fatalf("typing should select the first match, cursor=%d", m.cursor)
	}
	first := m.current().subject()
	for i := 1; i < m.rowCount(); i++ {
		a, _ := m.rowAt(i - 1)
		b, _ := m.rowAt(i)
		if a.hash >= b.hash {
			t.Fatalf("hits out of log order at %d", i)
		}
	}
	if !strings.Contains(screen(m), fmt.Sprintf("%d/200", m.rowCount())) {
		t.Errorf("counter missing:\n%s", strings.Split(screen(m), "\n")[0])
	}
	press(m, "down")
	keep := m.current().hash
	if m.current().subject() == first {
		t.Error("down did not move within the hits")
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
}

func TestPreviewSingleFlight(t *testing.T) {
	m := testModel(t, 10)
	a := m.wantKey
	if m.inflight != a || a == "" {
		t.Fatalf("first render not started: inflight=%q want=%q", m.inflight, a)
	}
	if !strings.Contains(screen(m), "rendering…") || !strings.Contains(screen(m), "commit "+m.current().hash) {
		t.Errorf("placeholder should carry the instant header:\n%s", screen(m))
	}
	cancelled := false
	m.cancelRender = func() { cancelled = true }
	press(m, "down")
	b := m.wantKey
	if !cancelled || m.inflight != a || b == a {
		t.Fatalf("moving on must cancel, not stack: cancelled=%v inflight=%q", cancelled, m.inflight)
	}
	// The cancelled render reports back: the wanted one starts.
	m.Update(previewMsg{key: a, cancelled: true, err: fmt.Errorf("killed")})
	if m.inflight != b || m.renderErr != "" {
		t.Fatalf("wanted render not started: inflight=%q err=%q", m.inflight, m.renderErr)
	}
	m.Update(previewMsg{key: b, hash: m.current().hash, content: "HEADER\n\nTHE DIFF", detail: detail{files: 1}})
	if m.inflight != "" || !strings.Contains(screen(m), "THE DIFF") {
		t.Errorf("render not shown:\n%s", screen(m))
	}
	// Cached: coming back needs no new render.
	press(m, "down", "up")
	m.Update(previewMsg{key: m.inflight, cancelled: true})
	if m.wantKey != b || !strings.Contains(screen(m), "THE DIFF") {
		t.Errorf("cache not used:\n%s", screen(m))
	}
}

func TestPreviewErrorIsShown(t *testing.T) {
	m := testModel(t, 3)
	m.Update(previewMsg{key: m.wantKey, hash: m.current().hash, err: fmt.Errorf("delta exploded")})
	if !strings.Contains(screen(m), "delta exploded") || m.inflight != "" {
		t.Errorf("error not surfaced:\n%s", screen(m))
	}
}

func TestFullViewAndSearch(t *testing.T) {
	m := testModel(t, 3)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	body := make([]string, 60)
	for i := range body {
		body[i] = fmt.Sprintf("line %02d", i)
	}
	body[40] = "\x1b[32mline 40 has a Needle in it\x1b[0m"
	body[50] = "another needle, and a second NEEDLE"
	feed := func() {
		m.Update(previewMsg{key: m.inflight, hash: m.current().hash, content: strings.Join(body, "\n")})
	}
	feed()
	press(m, "enter")
	if m.mode != modeFull {
		t.Fatal("enter should open the full view")
	}
	if m.inflight != "" { // same width in the rows layout: served from the cache
		feed()
	}
	if s := screen(m); !strings.Contains(s, "line 00") || strings.Contains(s, "asgitlog") || !strings.Contains(s, "back") {
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
	press(m, "esc")
	if m.mode != modeList || !strings.Contains(screen(m), "asgitlog") {
		t.Error("second esc should return to the list")
	}
	if !strings.Contains(screen(m), "line 00") {
		t.Errorf("list preview should be restored:\n%s", screen(m))
	}
}

func TestClickSelectsRow(t *testing.T) {
	m := testModel(t, 50)
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: 4})
	if m.cursor != 3 {
		t.Errorf("click on screen row 4 should select list row 3, got %d", m.cursor)
	}
	m.Update(tea.MouseClickMsg{Button: tea.MouseLeft, X: 10, Y: 30}) // over the preview
	if m.cursor != 3 {
		t.Errorf("click outside the list moved the cursor to %d", m.cursor)
	}
}

func TestLogErrorAndFatal(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := newModel(loadPrefs(), "")
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m.Update(logBatch{done: true, err: fmt.Errorf("fatal: your current branch 'main' does not have any commits yet")})
	if !strings.Contains(screen(m), "does not have any commits yet") || m.loading {
		t.Errorf("log error not shown:\n%s", screen(m))
	}
	press(m, "enter") // nothing selected: must not open the full view
	if m.mode != modeList {
		t.Error("enter without a commit should do nothing")
	}

	f := newModel(loadPrefs(), "")
	f.mode, f.fatal = modeFatal, "not inside a git work tree: /tmp"
	if !strings.Contains(screen(f), "not inside a git work tree") {
		t.Errorf("fatal view:\n%s", screen(f))
	}
	if _, cmd := f.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}); cmd == nil {
		t.Error("any key should quit the fatal view")
	}
}
