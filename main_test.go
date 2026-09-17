package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func rec(hash, short, author, email, date, decor, subject string) string {
	return strings.Join([]string{hash, short, author, email, date, decor, subject}, fieldSep)
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

func TestParseShortstat(t *testing.T) {
	cases := map[string]detail{
		" 3 files changed, 10 insertions(+), 2 deletions(-)": {files: 3, added: 10, deleted: 2},
		" 1 file changed, 1 insertion(+)":                    {files: 1, added: 1},
		" 2 files changed, 7 deletions(-)":                   {files: 2, deleted: 7},
		"":                                                   {},
	}
	for in, want := range cases {
		if got := parseShortstat(in); got != want {
			t.Errorf("parseShortstat(%q) = %+v, want %+v", in, got, want)
		}
	}
	if s := ansi.Strip(statLine(detail{files: 1, added: 4})); s != "1 file, +4 -0" {
		t.Errorf("statLine = %q", s)
	}
	if s := statLine(detail{}); s != "no changes" {
		t.Errorf("empty statLine = %q", s)
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
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestFilterKeepsLogOrder(t *testing.T) {
	cs := testCommits(t, "x preview y", "unrelated", "preview", "the preview pane")
	// The exact match (index 2) would rank first in a scored search.
	if got := hitIdx(filterCommits(cs, nil, 0, []string{"preview"})); !sameInts(got, []int{0, 2, 3}) {
		t.Errorf("hits = %v, want [0 2 3]", got)
	}
}

func TestFilterTermsAreANDed(t *testing.T) {
	cs := testCommits(t, "fix preview scroll", "fix list", "preview colors", "scroll fix for the preview")
	got := filterCommits(cs, nil, 0, queryTerms("  preview   fix "))
	if !sameInts(hitIdx(got), []int{0, 3}) {
		t.Fatalf("hits = %v, want [0 3]", hitIdx(got))
	}
	// Matched offsets of both terms are reported, and point at the right bytes.
	var b strings.Builder
	seen := map[int]bool{}
	for _, off := range got[0].matched {
		seen[off] = true
	}
	for i := 0; i < len(cs[0].corpus); i++ {
		if seen[i] {
			b.WriteByte(cs[0].corpus[i])
		}
	}
	if s := b.String(); s != "fixpreview" {
		t.Errorf("matched bytes spell %q, want %q", s, "fixpreview")
	}
}

func TestFilterSubsetAndFrom(t *testing.T) {
	cs := testCommits(t, "alpha", "beta alpha", "gamma", "alpha again")
	if got := hitIdx(filterCommits(cs, []int{1, 2, 3}, 0, []string{"alpha"})); !sameInts(got, []int{1, 3}) {
		t.Errorf("among: %v, want [1 3]", got)
	}
	if got := hitIdx(filterCommits(cs, nil, 2, []string{"alpha"})); !sameInts(got, []int{3}) {
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
		{"", "f", false},
		{" ", " f", false},
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

func TestRowWidthsAndColumns(t *testing.T) {
	c := sampleCommit(t)
	for _, w := range []int{200, 150, 120, 100, 80, 60, 40, 25} {
		for _, compact := range []bool{false, true} {
			row := plainSegs(rowSegs(&c, w, 7, compact, false))
			if got := ansi.StringWidth(row); got != w {
				t.Errorf("width %d compact=%v: row is %d cells: %q", w, compact, got, row)
			}
		}
	}

	wide := plainSegs(rowSegs(&c, 150, 7, false, false))
	if !strings.HasPrefix(wide, "  0123456 Ada Lovelace    <ada@example.c…> feat(ui): añade") {
		t.Errorf("wide row starts with %q", wide[:60])
	}
	if !strings.HasSuffix(wide, " 05/03/2026") {
		t.Errorf("wide row must end with the date: %q", wide)
	}
	// Refs are right-aligned in their own column, right before the date.
	if !strings.Contains(wide, "… 05/03/2026") || !strings.Contains(wide, "HEAD -> main, tag: v1.0") {
		t.Errorf("refs column wrong: %q", wide)
	}

	compact := plainSegs(rowSegs(&c, 60, 7, true, false))
	if !strings.HasPrefix(compact, "  0123456 05/03/2026 feat(ui)") || strings.Contains(compact, "Ada") {
		t.Errorf("compact row = %q", compact)
	}
	if sel := plainSegs(rowSegs(&c, 60, 7, true, true)); !strings.HasPrefix(sel, "▌ 0123456") {
		t.Errorf("selected row = %q", sel)
	}
}

func TestRowSubjectColumnIsStable(t *testing.T) {
	a := mustParse(t, rec("h", "abc1234", "Al", "a@b.c", "01/01/2026 00:00", "", "short"))
	b := mustParse(t, rec("h", "abc12345", "A much longer author name", "long.email@example.com", "01/01/2026 00:00",
		"refs/heads/x", "a subject long enough to be truncated by any reasonable list width, really"))
	ra, rb := plainSegs(rowSegs(&a, 140, 8, false, false)), plainSegs(rowSegs(&b, 140, 8, false, false))
	col := func(row, needle string) int { return ansi.StringWidth(row[:strings.Index(row, needle)]) }
	if col(ra, "short") != col(rb, "a subject") {
		t.Errorf("subject columns differ:\n%q\n%q", ra, rb)
	}
	if col(ra, "01/01/2026") != col(rb, "01/01/2026") {
		t.Errorf("date columns differ:\n%q\n%q", ra, rb)
	}
}

func TestNarrowWideRowDropsColumns(t *testing.T) {
	c := sampleCommit(t)
	if row := plainSegs(rowSegs(&c, 90, 7, false, false)); strings.Contains(row, "HEAD") || !strings.Contains(row, "Ada") {
		t.Errorf("90 cols should drop refs only: %q", row)
	}
	if row := plainSegs(rowSegs(&c, 60, 7, false, false)); strings.Contains(row, "Ada") || !strings.Contains(row, "05/03/2026") {
		t.Errorf("60 cols should drop the author too: %q", row)
	}
}

func TestRenderSegsHighlightsMatchedBytes(t *testing.T) {
	c := sampleCommit(t)
	hits := filterCommits([]commit{c}, nil, 0, []string{"analítico"})
	if len(hits) != 1 {
		t.Fatal("expected a hit")
	}
	out := renderSegs(rowSegs(&c, 150, 7, false, false), hits[0].matched, false)
	if ansi.Strip(out) != plainSegs(rowSegs(&c, 150, 7, false, false)) {
		t.Errorf("highlighting changed the text: %q", ansi.Strip(out))
	}
	// The multibyte í must survive being split into highlight runs.
	if !strings.Contains(ansi.Strip(out), "analítico") {
		t.Errorf("multibyte subject broken: %q", ansi.Strip(out))
	}
	if out == renderSegs(rowSegs(&c, 150, 7, false, false), nil, false) {
		t.Error("matched offsets produced no highlight")
	}
}

// ---- prefs ----

func TestPrefsRoundTrip(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if p := loadPrefs(); p.layout != layoutRows || p.diff != diffSBS {
		t.Errorf("defaults = %+v", p)
	}
	savePref("layout", layoutColumns)
	savePref("diff", diffSingle)
	if p := loadPrefs(); p.layout != layoutColumns || p.diff != diffSingle {
		t.Errorf("after save = %+v", p)
	}
	if got := filepath.Base(prefsDir()); got != "asgitlog" {
		t.Errorf("prefs dir = %q", prefsDir())
	}
	savePref("layout", "garbage")
	if p := loadPrefs(); p.layout != layoutRows {
		t.Errorf("unknown values must fall back to the default, got %+v", p)
	}
}

// ---- preview header ----

func TestPreviewHeader(t *testing.T) {
	c := sampleCommit(t)
	d := detail{body: "First paragraph.\n\nSecond one.", files: 2, added: 5, deleted: 1}
	got := ansi.Strip(previewHeader(&c, &d, 100))
	want := strings.Join([]string{
		"commit 0123456789abcdef0123456789abcdef01234567 (HEAD -> main, tag: v1.0, origin/main, refs/stash)",
		"Author: Ada Lovelace <ada@example.com>",
		"Date:   05/03/2026 14:30",
		"Stat:   2 files, +5 -1",
		"",
		"    feat(ui): añade el motor analítico",
		"",
		"    First paragraph.",
		"",
		"    Second one.",
	}, "\n")
	if got != want {
		t.Errorf("header:\n%s\nwant:\n%s", got, want)
	}
	loading := ansi.Strip(previewHeader(&c, nil, 100))
	if !strings.Contains(loading, "Stat:   …") || strings.Contains(loading, "First") {
		t.Errorf("loading header:\n%s", loading)
	}
	for _, l := range strings.Split(ansi.Strip(previewHeader(&c, &d, 40)), "\n") {
		if ansi.StringWidth(l) > 40 {
			t.Errorf("line wider than 40: %q", l)
		}
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
	t.Chdir(dir)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	return dir
}

func collectLog(t *testing.T) []commit {
	t.Helper()
	var all []commit
	for b := range streamLog(context.Background()) {
		if b.err != nil {
			t.Fatalf("streamLog: %v", b.err)
		}
		all = append(all, b.commits...)
	}
	return all
}

func TestStreamLogAndDetail(t *testing.T) {
	dir := gitRepo(t)
	if !insideWorkTree() {
		t.Fatal("insideWorkTree = false in a repo")
	}
	cs := collectLog(t)
	if len(cs) != 3 {
		t.Fatalf("got %d commits, want 3", len(cs))
	}
	if cs[0].subject() != "empty one" || cs[2].subject() != "first commit" {
		t.Errorf("order/subjects wrong: %q .. %q", cs[0].subject(), cs[2].subject())
	}
	if len(cs[0].refs) != 1 || !cs[0].refs[0].head || cs[0].refs[0].text != "main" {
		t.Errorf("HEAD refs = %+v", cs[0].refs)
	}
	if len(cs[1].refs) != 1 || cs[1].refs[0].text != "tag: v1" {
		t.Errorf("tag refs = %+v", cs[1].refs)
	}
	if cs[1].author() != "Ada" || cs[1].email() != "ada@x.io" || len(cs[1].hash) != 40 {
		t.Errorf("fields wrong: %+v", cs[1])
	}
	if len(cs[1].date()) != 10 || !strings.HasSuffix(cs[1].date(), "/03/2026") {
		t.Errorf("date = %q", cs[1].date())
	}

	d, err := loadDetail(context.Background(), cs[1].hash)
	if err != nil {
		t.Fatal(err)
	}
	if want := (detail{body: "With a body.\nOn two lines.", files: 2, added: 3, deleted: 1}); d != want {
		t.Errorf("detail = %+v, want %+v", d, want)
	}
	if d, _ := loadDetail(context.Background(), cs[0].hash); d != (detail{}) {
		t.Errorf("empty commit detail = %+v", d)
	}

	ri := loadRepoInfo()
	if real, _ := filepath.EvalSymlinks(dir); ri.Top != real || ri.Branch != "main" || ri.Upstream != "" {
		t.Errorf("repoInfo = %+v", ri)
	}
}

func TestStreamLogReportsEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Chdir(dir)
	var last logBatch
	for b := range streamLog(context.Background()) {
		last = b
	}
	if !last.done || last.err == nil {
		t.Errorf("expected a final batch with git's error, got %+v", last)
	}
}

func TestRenderDiffFallsBackWithoutDelta(t *testing.T) {
	gitRepo(t)
	cs := collectLog(t)
	out, err := renderDiff(context.Background(), cs[1].hash, 80, true, "")
	if err != nil {
		t.Fatal(err)
	}
	if plain := ansi.Strip(out); !strings.Contains(plain, "+three") || !strings.Contains(plain, "b.txt") {
		t.Errorf("fallback diff:\n%s", plain)
	}
	if empty, err := renderDiff(context.Background(), cs[0].hash, 80, true, ""); err != nil || empty != "" {
		t.Errorf("empty commit diff = %q, %v", empty, err)
	}
}

func TestRenderDiffWithDelta(t *testing.T) {
	deltaBin, err := exec.LookPath("delta")
	if err != nil {
		t.Skip("delta not installed")
	}
	gitRepo(t)
	cs := collectLog(t)
	for _, sbs := range []bool{true, false} {
		out, err := renderDiff(context.Background(), cs[1].hash, 90, sbs, deltaBin)
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
	}
}
