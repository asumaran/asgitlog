package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// rec builds a log record; parents and the author time are fixed unless the
// test cares (see recP).
func rec(hash, short, author, email, date, decor, subject string) string {
	return recP(hash, short, author, email, date, decor, "p1", "1767261600", subject)
}

func recP(hash, short, author, email, date, decor, parents, at, subject string) string {
	return strings.Join([]string{hash, short, author, email, date, decor, parents, at, subject}, fieldSep)
}

func mustParse(t *testing.T, r string) commit {
	t.Helper()
	c, ok := parseCommit(r)
	if !ok {
		t.Fatalf("parseCommit(%q) failed", r)
	}
	return c
}

func sampleCommit(t *testing.T) commit {
	return mustParse(t, rec("0123456789abcdef0123456789abcdef01234567", "0123456", "Ada Lovelace",
		"ada@example.com", "05/03/2026 14:30",
		"HEAD -> refs/heads/main, tag: refs/tags/v1.0, refs/remotes/origin/main, refs/stash",
		"feat(ui): añade el motor analítico"))
}

func TestParseCommitFields(t *testing.T) {
	c := sampleCommit(t)
	checks := map[string][2]string{
		"short":    {c.short(), "0123456"},
		"author":   {c.author(), "Ada Lovelace"},
		"email":    {c.email(), "ada@example.com"},
		"subject":  {c.subject(), "feat(ui): añade el motor analítico"},
		"date":     {c.date(), "05/03/2026"},
		"dateTime": {c.dateTime(), "05/03/2026 14:30"},
	}
	for name, v := range checks {
		if v[0] != v[1] {
			t.Errorf("%s = %q, want %q", name, v[0], v[1])
		}
	}
	want := "0123456 Ada Lovelace <ada@example.com> feat(ui): añade el motor analítico " +
		"HEAD -> main, tag: v1.0, origin/main, refs/stash 05/03/2026 14:30"
	if c.corpus != want {
		t.Errorf("corpus = %q\nwant     %q", c.corpus, want)
	}
	if c.when != 1767261600 || c.merge() {
		t.Errorf("when=%d merge=%v", c.when, c.merge())
	}
	m := mustParse(t, recP("h", "abc1234", "A", "a@b", "01/01/2026 00:00", "", "aaaa bbbb", "1", "Merge branch x"))
	if !m.merge() || m.parents != "aaaa bbbb" {
		t.Errorf("merge not detected: %+v", m)
	}
}

func TestParseCommitRefs(t *testing.T) {
	c := sampleCommit(t)
	want := []ref{
		{text: "main", kind: refBranch, head: true},
		{text: "tag: v1.0", kind: refTag},
		{text: "origin/main", kind: refRemote},
		{text: "refs/stash", kind: refOther},
	}
	if len(c.refs) != len(want) {
		t.Fatalf("got %d refs, want %d", len(c.refs), len(want))
	}
	for i, w := range want {
		g := c.refs[i]
		if g.text != w.text || g.kind != w.kind || g.head != w.head {
			t.Errorf("ref %d = %+v, want %+v", i, g, w)
		}
		if got := c.corpus[g.off : g.off+len(g.text)]; got != g.text {
			t.Errorf("ref %d offset points at %q, want %q", i, got, g.text)
		}
	}

	detached := mustParse(t, rec("h", "abc1234", "A", "a@b", "01/01/2026 00:00", "HEAD, refs/heads/x", "s"))
	if detached.refs[0].kind != refHead || detached.refs[0].head {
		t.Errorf("detached HEAD parsed as %+v", detached.refs[0])
	}
	if none := mustParse(t, rec("h", "abc1234", "A", "a@b", "01/01/2026 00:00", "", "s")); none.refs != nil {
		t.Errorf("no decorations should give nil refs, got %+v", none.refs)
	}
}

func TestParseCommitRejectsMalformed(t *testing.T) {
	for _, r := range []string{"", "only" + fieldSep + "two", rec("h", "s", "a", "e", "short", "", "subj")} {
		if _, ok := parseCommit(r); ok {
			t.Errorf("parseCommit(%q) should fail", r)
		}
	}
}

func TestParseNumstat(t *testing.T) {
	d := parseNumstat("\n10\t2\tui.go\n-\t-\tdocs/demo.gif\n0\t7\tpath with\ttab.txt\n")
	want := []fileStat{{path: "ui.go", added: 10, deleted: 2}, {path: "docs/demo.gif", binary: true}, {path: "path with\ttab.txt", deleted: 7}}
	if !reflect.DeepEqual(d.files, want) || d.added != 10 || d.deleted != 9 {
		t.Errorf("parseNumstat = %+v", d)
	}
	if s := ansi.Strip(statLine(d)); s != "3 files changed  +10 -9" {
		t.Errorf("statLine = %q", s)
	}
	if s := ansi.Strip(statLine(detail{})); s != "no changes" {
		t.Errorf("empty statLine = %q", s)
	}
	if s := ansi.Strip(sectionRule(statLine(d), 40)); s != "── 3 files changed  +10 -9 ─────────────" || ansi.StringWidth(s) != 40 {
		t.Errorf("sectionRule = %q", s)
	}
}

func TestRepoInfoString(t *testing.T) {
	t.Setenv("HOME", "/Users/x")
	ri := repoInfo{Top: "/Users/x/dev/repo", Branch: "main"}
	if got := ri.String(); got != "~/dev/repo  main" {
		t.Errorf("no upstream: %q", got)
	}
	ri.Upstream, ri.Ahead, ri.Behind = "origin/main", 2, 1
	if got := ri.String(); got != "~/dev/repo  main -> origin/main (ahead 2, behind 1)" {
		t.Errorf("with upstream: %q", got)
	}
	if got := homeRel("/Users/xavier/repo"); got != "/Users/xavier/repo" {
		t.Errorf("homeRel must only match whole path components, got %q", got)
	}
}

func TestWebAndCommitURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:asumaran/asgotopr.git":       "https://github.com/asumaran/asgotopr",
		"https://github.com/asumaran/asgotopr.git":   "https://github.com/asumaran/asgotopr",
		"https://user@github.com/asumaran/asgotopr":  "https://github.com/asumaran/asgotopr",
		"ssh://git@gitlab.example.com:2222/g/s/repo": "https://gitlab.example.com/g/s/repo",
		"/srv/git/repo.git":                          "",
		"":                                           "",
	}
	for in, want := range cases {
		if got := webURL(in); got != want {
			t.Errorf("webURL(%q) = %q, want %q", in, got, want)
		}
	}
	if got := commitURL("https://github.com/a/b", "abc"); got != "https://github.com/a/b/commit/abc" {
		t.Errorf("github: %q", got)
	}
	if got := commitURL("https://gitlab.com/a/b", "abc"); got != "https://gitlab.com/a/b/-/commit/abc" {
		t.Errorf("gitlab: %q", got)
	}
	if got := commitURL("https://bitbucket.org/a/b", "abc"); got != "https://bitbucket.org/a/b/commits/abc" {
		t.Errorf("bitbucket: %q", got)
	}
}

func TestLogArgs(t *testing.T) {
	args := logOpts{revs: []string{"main..dev"}, paths: []string{"src", "README.md"}, all: true, pickaxe: "needle"}.args()
	tail := strings.Join(args[len(args)-6:], " ")
	if tail != "--all -Sneedle main..dev -- src README.md" {
		t.Errorf("args tail = %q", tail)
	}
	if plain := (logOpts{}).args(); plain[len(plain)-1] != "--" {
		t.Errorf("revisions must always be closed with --: %v", plain)
	}
}

// ---- filter ----

func testCommits(t *testing.T, subjects ...string) []commit {
	t.Helper()
	out := make([]commit, len(subjects))
	for i, s := range subjects {
		out[i] = mustParse(t, rec(strings.Repeat("a", 40), "aaaaaa"+string(rune('0'+i)), "Ada", "ada@x.io", "01/01/2026 10:00", "", s))
	}
	return out
}

func hitIdx(hits []hit) []int {
	out := make([]int, len(hits))
	for i, h := range hits {
		out[i] = h.idx
	}
	return out
}

func sameInts(a, b []int) bool {
	return reflect.DeepEqual(append([]int{}, a...), append([]int{}, b...))
}

// spelled returns the corpus bytes at the matched offsets, in corpus order.
func spelled(c commit, h hit) string {
	seen := map[int]bool{}
	for _, off := range h.matched {
		seen[off] = true
	}
	var b strings.Builder
	for i := 0; i < len(c.corpus); i++ {
		if seen[i] {
			b.WriteByte(c.corpus[i])
		}
	}
	return b.String()
}

func TestFilterIsSubstringAndKeepsLogOrder(t *testing.T) {
	cs := testCommits(t, "x Preview y", "p r e v i e w scattered", "preview", "the PREVIEW pane, a preview")
	got := filterCommits(cs, nil, 0, queryTerms("preview"))
	// Index 1 only matches as a subsequence; index 2 would rank first if scored.
	if !sameInts(hitIdx(got), []int{0, 2, 3}) {
		t.Fatalf("hits = %v, want [0 2 3]", hitIdx(got))
	}
	if s := spelled(cs[3], got[2]); s != "PREVIEWpreview" {
		t.Errorf("every occurrence should be reported, got %q", s)
	}
}

func TestFilterFuzzyTerm(t *testing.T) {
	cs := testCommits(t, "x Preview y", "p r e v i e w scattered", "unrelated")
	if got := hitIdx(filterCommits(cs, nil, 0, queryTerms("~prvw"))); !sameInts(got, []int{0, 1}) {
		t.Errorf("fuzzy hits = %v, want [0 1]", got)
	}
	if terms := queryTerms(" ~  FiX ~a "); len(terms) != 2 || terms[0] != (qterm{text: "fix"}) || terms[1] != (qterm{text: "a", fuzzy: true}) {
		t.Errorf("queryTerms = %+v", terms)
	}
}

func TestFilterTermsAreANDed(t *testing.T) {
	cs := testCommits(t, "fix preview scroll", "fix list", "preview colors", "scroll fix for the preview")
	got := filterCommits(cs, nil, 0, queryTerms("  preview   fix "))
	if !sameInts(hitIdx(got), []int{0, 3}) {
		t.Fatalf("hits = %v, want [0 3]", hitIdx(got))
	}
	if s := spelled(cs[0], got[0]); s != "fixpreview" {
		t.Errorf("matched bytes spell %q, want %q", s, "fixpreview")
	}
}

func TestFilterNonASCII(t *testing.T) {
	cs := testCommits(t, "añade el motor ANALÍTICO", "nothing here")
	got := filterCommits(cs, nil, 0, queryTerms("analítico"))
	if !sameInts(hitIdx(got), []int{0}) || spelled(cs[0], got[0]) != "ANALÍTICO" {
		t.Errorf("non-ASCII term: hits=%v", hitIdx(got))
	}
}

func TestFilterSubsetAndFrom(t *testing.T) {
	cs := testCommits(t, "alpha", "beta alpha", "gamma", "alpha again")
	if got := hitIdx(filterCommits(cs, []int{1, 2, 3}, 0, queryTerms("alpha"))); !sameInts(got, []int{1, 3}) {
		t.Errorf("among: %v, want [1 3]", got)
	}
	if got := hitIdx(filterCommits(cs, nil, 2, queryTerms("alpha"))); !sameInts(got, []int{3}) {
		t.Errorf("from: %v, want [3]", got)
	}
	if got := filterCommits(cs, nil, 0, nil); got != nil {
		t.Errorf("no terms should give nil, got %v", got)
	}
}

func TestNarrows(t *testing.T) {
	cases := []struct {
		prev, next string
		want       bool
	}{
		{"fi", "fix", true},
		{"fix", "fix p", true},
		{"~fx", "~fxp", true},
		{"", "f", false},
		{" ", " f", false},
		{"~", "~f", false},
		{"fix", "fi", false},
		{"fix", "pix", false},
	}
	for _, c := range cases {
		if got := narrows(c.prev, c.next); got != c.want {
			t.Errorf("narrows(%q, %q) = %v", c.prev, c.next, got)
		}
	}
}

// ---- rows ----

func wide(w int) rowLayout {
	return rowLayout{width: w, hashW: 7, authorW: 12, now: 1767261600 + 3*86400}
}
func compact(w int) rowLayout { l := wide(w); l.compact = true; return l }

func TestRowWidthsAndColumns(t *testing.T) {
	c := sampleCommit(t)
	for _, w := range []int{200, 150, 120, 100, 80, 60, 40, 25} {
		for _, l := range []rowLayout{wide(w), compact(w)} {
			row := plainSegs(rowSegs(&c, l, false))
			if got := ansi.StringWidth(row); got != w {
				t.Errorf("width %d compact=%v: row is %d cells: %q", w, l.compact, got, row)
			}
		}
	}

	row := plainSegs(rowSegs(&c, wide(150), false))
	if !strings.HasPrefix(row, "  0123456 Ada Lovelace feat(ui): añade el motor analítico ") {
		t.Errorf("wide row starts with %q", row[:70])
	}
	if strings.Contains(row, "ada@example") {
		t.Errorf("the email is not a list column: %q", row)
	}
	// Refs take only the room they need, right before the date.
	if !strings.HasSuffix(row, "  HEAD -> main, tag: v1.0, origin/main, refs/stash 05/03/2026") {
		t.Errorf("refs/date tail wrong: %q", row)
	}

	crow := plainSegs(rowSegs(&c, compact(60), false))
	if !strings.HasPrefix(crow, "  0123456   3d feat(ui)") || strings.Contains(crow, "Ada") {
		t.Errorf("compact row = %q", crow)
	}
	if sel := plainSegs(rowSegs(&c, compact(60), true)); !strings.HasPrefix(sel, "▌ 0123456") {
		t.Errorf("selected row = %q", sel)
	}
}

func TestRowColumnsAreStable(t *testing.T) {
	a := mustParse(t, rec("h", "abc1234", "Al", "a@b.c", "01/01/2026 00:00", "", "short"))
	b := mustParse(t, rec("h", "abc12345", "A much longer author name", "long.email@example.com", "01/01/2026 00:00",
		"refs/heads/x", "a subject long enough to be truncated by any reasonable list width, really, it just goes on and on and on"))
	l := rowLayout{width: 120, hashW: 8, authorW: maxAuthorW}
	ra, rb := plainSegs(rowSegs(&a, l, false)), plainSegs(rowSegs(&b, l, false))
	col := func(row, needle string) int { return ansi.StringWidth(row[:strings.Index(row, needle)]) }
	if col(ra, "short") != col(rb, "a subject") {
		t.Errorf("subject columns differ:\n%q\n%q", ra, rb)
	}
	if col(ra, "01/01/2026") != col(rb, "01/01/2026") || !strings.HasSuffix(rb, "… x 01/01/2026") {
		t.Errorf("date/refs columns differ:\n%q\n%q", ra, rb)
	}
}

func TestNarrowWideRowDropsAuthor(t *testing.T) {
	c := sampleCommit(t)
	if row := plainSegs(rowSegs(&c, wide(70), false)); !strings.Contains(row, "Ada") {
		t.Errorf("70 cols should keep the author: %q", row)
	}
	if row := plainSegs(rowSegs(&c, wide(50), false)); strings.Contains(row, "Ada") || !strings.Contains(row, "05/03/2026") {
		t.Errorf("50 cols should drop the author: %q", row)
	}
}

func TestSpecialRows(t *testing.T) {
	m := mustParse(t, recP("h", "abc1234", "A", "a@b", "01/01/2026 00:00", "", "p1 p2", "1", "Merge branch x"))
	if out := renderSegs(rowSegs(&m, wide(80), false), nil, false); !strings.Contains(out, "\x1b[2m") {
		t.Errorf("merge subjects should be faint: %q", out)
	}
	wt := commit{}
	wt, _ = parseCommit(recP("", wtShort, "", "", "01/01/2026 00:00", "", "", "1", "Uncommitted changes (2 files)"))
	wt.wt = true
	row := plainSegs(rowSegs(&wt, wide(80), false))
	if !strings.HasPrefix(row, "  *       ") || !strings.Contains(row, "Uncommitted changes") || strings.Contains(row, "2026") {
		t.Errorf("working tree row = %q", row)
	}
}

func TestRelDate(t *testing.T) {
	now := int64(1_800_000_000)
	cases := map[int64]string{30: "now", 300: "5m", 7200: "2h", 3 * 86400: "3d", 21 * 86400: "3w", 90 * 86400: "3mo", 800 * 86400: "2y"}
	for age, want := range cases {
		if got := relDate(now-age, now); got != want {
			t.Errorf("relDate(%ds) = %q, want %q", age, got, want)
		}
	}
}

func TestRenderSegsHighlightsMatchedBytes(t *testing.T) {
	c := sampleCommit(t)
	hits := filterCommits([]commit{c}, nil, 0, queryTerms("analítico"))
	if len(hits) != 1 {
		t.Fatal("expected a hit")
	}
	plain := plainSegs(rowSegs(&c, wide(150), false))
	out := renderSegs(rowSegs(&c, wide(150), false), hits[0].matched, false)
	// The multibyte í must survive being split into highlight runs.
	if ansi.Strip(out) != plain || !strings.Contains(plain, "analítico") {
		t.Errorf("highlighting changed the text: %q", ansi.Strip(out))
	}
	if out == renderSegs(rowSegs(&c, wide(150), false), nil, false) {
		t.Error("matched offsets produced no highlight")
	}
}

// ---- prefs ----

func TestPrefsRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if p := loadPrefs(); p != (prefs{layout: layoutRows, diff: diffAuto, tool: toolDelta, splitRows: 70, splitColumns: 75}) {
		t.Errorf("defaults = %+v", p)
	}
	savePref("layout", layoutColumns)
	savePref("diff", diffSingle)
	savePref("split-rows", "55")
	savePref("split-columns", "80")
	savePref("renderer", toolHunk)
	if p := loadPrefs(); p != (prefs{layout: layoutColumns, diff: diffSingle, tool: toolHunk, splitRows: 55, splitColumns: 80}) {
		t.Errorf("after save = %+v", p)
	}
	if got := filepath.Base(prefsDir()); got != "asgitlog" {
		t.Errorf("prefs dir = %q", prefsDir())
	}
	savePref("layout", "garbage")
	savePref("split-rows", "5")
	if p := loadPrefs(); p.layout != layoutRows || p.splitRows != 70 {
		t.Errorf("invalid values must fall back to the defaults, got %+v", p)
	}
}

func TestEffectiveDiff(t *testing.T) {
	if effectiveDiff(diffAuto, 119) != diffSingle || effectiveDiff(diffAuto, 120) != diffSBS {
		t.Error("auto should switch at 120 columns")
	}
	if effectiveDiff(diffSBS, 40) != diffSBS || effectiveDiff(diffSingle, 300) != diffSingle {
		t.Error("explicit modes ignore the width")
	}
	if diffLabel(diffAuto, 200) != "auto: side-by-side" || diffLabel(diffSingle, 200) != "single column" {
		t.Errorf("labels: %q / %q", diffLabel(diffAuto, 200), diffLabel(diffSingle, 200))
	}
}

// ---- preview header ----

func TestPreviewHeader(t *testing.T) {
	c := sampleCommit(t)
	d := detail{body: "First paragraph.\n\nSecond one.", added: 5, deleted: 1,
		files: []fileStat{{path: "ui.go", added: 5, deleted: 1}, {path: "docs/demo.gif", binary: true}}}
	got := ansi.Strip(previewHeader(&c, &d, 100))
	want := strings.Join([]string{
		"commit 0123456789abcdef0123456789abcdef01234567 (HEAD -> main, tag: v1.0, origin/main, refs/stash)",
		"Author: Ada Lovelace <ada@example.com>",
		"Date:   05/03/2026 14:30",
		"",
		"    feat(ui): añade el motor analítico",
		"",
		"    First paragraph.",
		"",
		"    Second one.",
		"",
		"── 2 files changed  +5 -1 " + strings.Repeat("─", 74),
		"    ui.go          +5 -1",
		"    docs/demo.gif  binary",
	}, "\n")
	if got != want {
		t.Errorf("header:\n%s\nwant:\n%s", got, want)
	}
	loading := ansi.Strip(previewHeader(&c, nil, 100))
	if strings.Contains(loading, "changed") || strings.Contains(loading, "First") || !strings.Contains(loading, "Date:") {
		t.Errorf("loading header:\n%s", loading)
	}
	for _, l := range strings.Split(ansi.Strip(previewHeader(&c, &d, 40)), "\n") {
		if ansi.StringWidth(l) > 40 {
			t.Errorf("line wider than 40: %q", l)
		}
	}
	empty := ansi.Strip(previewHeader(&c, &detail{}, 100))
	if !strings.HasSuffix(empty, "\n── no changes "+strings.Repeat("─", 86)) {
		t.Errorf("an empty commit says so in the section title:\n%s", empty)
	}
}

func TestPreviewHeaderVariants(t *testing.T) {
	m := mustParse(t, recP(strings.Repeat("c", 40), "ccccccc", "A", "a@b", "01/01/2026 00:00", "",
		strings.Repeat("a", 40)+" "+strings.Repeat("b", 40), "1", "Merge branch x"))
	if h := ansi.Strip(previewHeader(&m, &detail{}, 100)); !strings.Contains(h, "Merge:  aaaaaaa bbbbbbb") {
		t.Errorf("merge header:\n%s", h)
	}
	var many []fileStat
	for i := range maxHeaderFiles + 3 {
		many = append(many, fileStat{path: fmt.Sprintf("deep/nested/directory/structure/file%02d.go", i), added: 1})
	}
	h := ansi.Strip(previewHeader(&m, &detail{files: many}, 80))
	if !strings.Contains(h, "… 3 more files") || !strings.Contains(h, "    deep/nested/directory/structure/file49.go  +1 -0") ||
		strings.Contains(h, "file50.go") {
		t.Errorf("the list is only capped past %d files:\n%s", maxHeaderFiles, h)
	}
}

func TestFileListNeverCutsPaths(t *testing.T) {
	long := "a/very/long/path/that/does/not/fit/the/line/at/all/component.tsx"
	files := []fileStat{{path: "ui.go", added: 120, deleted: 30}, {path: "internal/preview/header.go", added: 1}, {path: long, added: 2, deleted: 2}}
	for _, w := range []int{100, 44, 30} {
		lines := fileList(files, w)
		joined := strings.ReplaceAll(strings.ReplaceAll(ansi.Strip(strings.Join(lines, "")), " ", ""), "+", " +")
		for _, f := range files {
			if !strings.Contains(joined, f.path) {
				t.Errorf("width %d: %q was cut:\n%s", w, f.path, ansi.Strip(strings.Join(lines, "\n")))
			}
		}
		for _, l := range lines {
			if ansi.StringWidth(l) > w || strings.Contains(l, "…") {
				t.Errorf("width %d: bad line %q", w, ansi.Strip(l))
			}
		}
	}
	// Wide enough: one aligned column. Narrow: the long path takes its own
	// line(s) and the short ones stay aligned.
	if got := ansi.Strip(strings.Join(fileList(files, 100), "\n")); got != "    ui.go"+strings.Repeat(" ", len(long)-5)+"  +120 -30\n"+
		"    internal/preview/header.go"+strings.Repeat(" ", len(long)-26)+"    +1  -0\n    "+long+"    +2  -2" {
		t.Errorf("aligned list:\n%s", got)
	}
	narrow := strings.Split(ansi.Strip(strings.Join(fileList(files, 44), "\n")), "\n")
	if narrow[0] != "    ui.go"+strings.Repeat(" ", 25)+"  +120 -30" || len(narrow) != 4 || narrow[1] != "    internal/preview/header.go        +1  -0" || !strings.HasSuffix(narrow[3], "    +2  -2") {
		t.Errorf("narrow list:\n%s", strings.Join(narrow, "\n"))
	}
	wt, _ := parseCommit(recP("", wtShort, "", "", "01/01/2026 00:00", "", "", "1", "Uncommitted changes"))
	wt.wt = true
	h := ansi.Strip(previewHeader(&wt, &detail{files: files[:1], added: 1, untracked: []string{"new.txt"}}, 80))
	if !strings.HasPrefix(h, "Working tree") || !strings.Contains(h, "── 1 file changed  +1 -0 ─") || !strings.Contains(h, "── 1 untracked ─") ||
		!strings.Contains(h, "    new.txt") || strings.Contains(h, "Author") {
		t.Errorf("working tree header:\n%s", h)
	}
}

func TestFileLines(t *testing.T) {
	rule := strings.Repeat("─", 40)
	content := strings.Join([]string{"commit abc", "", "\x1b[34mui.go\x1b[0m", "\x1b[34m" + rule + "\x1b[0m", "│ 1 │x", rule, "", "list.go", rule, "diff --git a/x b/x"}, "\n")
	// The rule right under a diff line (index 5) is content, not a file header.
	if got := fileLines(content); !sameInts(got, []int{2, 7, 9}) {
		t.Errorf("fileLines = %v, want [2 7 9]", got)
	}
}

// ---- integration: a real repository ----

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
			"GIT_AUTHOR_NAME=Ada", "GIT_AUTHOR_EMAIL=ada@x.io",
			"GIT_COMMITTER_NAME=Ada", "GIT_COMMITTER_EMAIL=ada@x.io",
			"GIT_AUTHOR_DATE=2026-03-05T14:30:00+0000", "GIT_COMMITTER_DATE=2026-03-05T14:30:00+0000",
			"TZ=UTC")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	write("a.txt", "one\ntwo\n")
	run("add", ".")
	run("commit", "-q", "-m", "first commit")
	write("a.txt", "one\n2\nthree\n")
	write("b.txt", "new\n")
	run("add", ".")
	run("commit", "-q", "-m", "second commit", "-m", "With a body.\nOn two lines.")
	run("tag", "v1")
	run("commit", "-q", "--allow-empty", "-m", "empty one")
	// A side branch merged without conflicts: git show's combined diff is empty.
	run("switch", "-q", "-c", "side", "HEAD~1")
	write("side.txt", "side\n")
	run("add", ".")
	run("commit", "-q", "-m", "side work")
	run("switch", "-q", "main")
	run("merge", "-q", "--no-ff", "-m", "Merge branch 'side'", "side")
	t.Chdir(dir)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	return dir
}

func collectLog(t *testing.T, opts logOpts) []commit {
	t.Helper()
	var all []commit
	for b := range streamLog(context.Background(), opts, 7) {
		if b.err != nil {
			t.Fatalf("streamLog: %v", b.err)
		}
		if b.gen != 7 {
			t.Fatalf("batch generation = %d", b.gen)
		}
		all = append(all, b.commits...)
	}
	return all
}

func bySubject(t *testing.T, cs []commit, subject string) *commit {
	t.Helper()
	for i := range cs {
		if cs[i].subject() == subject {
			return &cs[i]
		}
	}
	t.Fatalf("no commit %q", subject)
	return nil
}

func TestStreamLogAndDetail(t *testing.T) {
	dir := gitRepo(t)
	if !insideWorkTree() {
		t.Fatal("insideWorkTree = false in a repo")
	}
	cs := collectLog(t, logOpts{})
	if len(cs) != 5 {
		t.Fatalf("got %d commits, want 5", len(cs))
	}
	if cs[0].subject() != "Merge branch 'side'" || cs[4].subject() != "first commit" {
		t.Errorf("order/subjects wrong: %q .. %q", cs[0].subject(), cs[4].subject())
	}
	if len(cs[0].refs) != 1 || !cs[0].refs[0].head || cs[0].refs[0].text != "main" || !cs[0].merge() {
		t.Errorf("HEAD = %+v", cs[0])
	}
	second := bySubject(t, cs, "second commit")
	if len(second.refs) != 1 || second.refs[0].text != "tag: v1" || second.merge() {
		t.Errorf("tag refs = %+v", second.refs)
	}
	if second.author() != "Ada" || second.email() != "ada@x.io" || len(second.hash) != 40 || second.when == 0 {
		t.Errorf("fields wrong: %+v", second)
	}
	if len(second.date()) != 10 || !strings.HasSuffix(second.date(), "/03/2026") {
		t.Errorf("date = %q", second.date())
	}

	ctx := context.Background()
	d, err := loadDetail(ctx, second, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := detail{body: "With a body.\nOn two lines.", added: 3, deleted: 1,
		files: []fileStat{{path: "a.txt", added: 2, deleted: 1}, {path: "b.txt", added: 1}}}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("detail = %+v, want %+v", d, want)
	}
	if d, _ := loadDetail(ctx, second, []string{"b.txt"}); len(d.files) != 1 || d.files[0].path != "b.txt" {
		t.Errorf("path-limited detail = %+v", d)
	}
	if d, _ := loadDetail(ctx, bySubject(t, cs, "empty one"), nil); len(d.files) != 0 {
		t.Errorf("empty commit detail = %+v", d)
	}
	// A merge is compared against its first parent.
	if d, _ := loadDetail(ctx, &cs[0], nil); len(d.files) != 1 || d.files[0].path != "side.txt" {
		t.Errorf("merge detail = %+v", d)
	}
	if diff, err := renderDiff(ctx, &cs[0], 80, false, diffTool{}, nil, nil); err != nil || !strings.Contains(ansi.Strip(diff), "+side") {
		t.Errorf("merge diff = %q, %v", ansi.Strip(diff), err)
	}

	ri := loadRepoInfo()
	if real, _ := filepath.EvalSymlinks(dir); ri.Top != real || ri.Branch != "main" || ri.Upstream != "" || ri.WebURL != "" {
		t.Errorf("repoInfo = %+v", ri)
	}
}

func TestLogScopes(t *testing.T) {
	gitRepo(t)
	if out, err := exec.Command("git", "switch", "-q", "-c", "other", "HEAD~1").CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if n := len(collectLog(t, logOpts{})); n != 3 {
		t.Errorf("current branch: %d commits, want 3", n)
	}
	if n := len(collectLog(t, logOpts{all: true})); n != 5 {
		t.Errorf("--all: %d commits, want 5", n)
	}
	if cs := collectLog(t, logOpts{all: true, paths: []string{"b.txt"}}); len(cs) != 1 || cs[0].subject() != "second commit" {
		t.Errorf("path scope = %d commits", len(cs))
	}
	if cs := collectLog(t, logOpts{all: true, pickaxe: "three"}); len(cs) != 1 || cs[0].subject() != "second commit" {
		t.Errorf("pickaxe scope = %d commits", len(cs))
	}
	if cs := collectLog(t, logOpts{revs: []string{"main", "^other"}}); len(cs) != 2 {
		t.Errorf("revision range = %d commits, want 2", len(cs))
	}
}

func TestWorkTreeRow(t *testing.T) {
	dir := gitRepo(t)
	if cs := collectLog(t, logOpts{}); cs[0].wt {
		t.Fatal("a clean tree has no working tree row")
	}
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n2\nthree\nfour\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x\n"), 0o644)
	cs := collectLog(t, logOpts{})
	if !cs[0].wt || cs[0].short() != wtShort || cs[0].subject() != "Uncommitted changes (2 files)" || len(cs) != 6 {
		t.Fatalf("working tree row = %+v (of %d)", cs[0], len(cs))
	}
	d, err := loadDetail(context.Background(), &cs[0], nil)
	if err != nil || len(d.files) != 1 || d.added != 1 || !reflect.DeepEqual(d.untracked, []string{"untracked.txt"}) {
		t.Errorf("working tree detail = %+v, %v", d, err)
	}
	if diff, err := renderDiff(context.Background(), &cs[0], 80, false, diffTool{}, nil, nil); err != nil || !strings.Contains(ansi.Strip(diff), "+four") {
		t.Errorf("working tree diff = %q, %v", ansi.Strip(diff), err)
	}
	if cs := collectLog(t, logOpts{pickaxe: "three"}); cs[0].wt {
		t.Error("a content search has no working tree row")
	}
}

func TestStreamLogReportsEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Chdir(dir)
	var last logBatch
	for b := range streamLog(context.Background(), logOpts{}, 0) {
		last = b
	}
	if !last.done || last.err == nil {
		t.Errorf("expected a final batch with git's error, got %+v", last)
	}
}

func TestRenderDiffFallsBackWithoutDelta(t *testing.T) {
	gitRepo(t)
	cs := collectLog(t, logOpts{})
	out, err := renderDiff(context.Background(), bySubject(t, cs, "second commit"), 80, true, diffTool{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plain := ansi.Strip(out); !strings.Contains(plain, "+three") || !strings.Contains(plain, "b.txt") {
		t.Errorf("fallback diff:\n%s", plain)
	}
	if len(fileLines(out)) != 2 {
		t.Errorf("fileLines on plain git output = %v", fileLines(out))
	}
	if empty, err := renderDiff(context.Background(), bySubject(t, cs, "empty one"), 80, true, diffTool{}, nil, nil); err != nil || empty != "" {
		t.Errorf("empty commit diff = %q, %v", empty, err)
	}
}

func TestRenderDiffWithDelta(t *testing.T) {
	deltaBin, err := exec.LookPath("delta")
	if err != nil {
		t.Skip("delta not installed")
	}
	gitRepo(t)
	second := bySubject(t, collectLog(t, logOpts{}), "second commit")
	for _, sbs := range []bool{true, false} {
		out, err := renderDiff(context.Background(), second, 90, sbs, diffTool{name: toolDelta, bin: deltaBin}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(ansi.Strip(out), "three") {
			t.Errorf("sbs=%v: diff content missing:\n%s", sbs, ansi.Strip(out))
		}
		for _, l := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(l); w > 90 {
				t.Errorf("sbs=%v: line is %d cells, over --width=90", sbs, w)
			}
		}
		if files := fileLines(out); len(files) != 2 {
			t.Errorf("sbs=%v: delta file headers found at %v, want 2", sbs, files)
		}
	}
}
