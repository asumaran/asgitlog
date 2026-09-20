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

func TestFileLinesHunk(t *testing.T) {
	rule := " " + strings.Repeat("─", 40)
	content := strings.Join([]string{"── diff ───", "", " \x1b[1mui.go\x1b[m            +6 -27  ", "▌ 1  x  +1 -1", rule, " list.go           +1 -0", "▌ 2  y"}, "\n")
	// A diff row that happens to end like the counts (index 3) is not a header:
	// it is neither first nor under a rule or a blank line.
	if got := fileLines(content); !sameInts(got, []int{2, 5}) {
		t.Errorf("fileLines = %v, want [2 5]", got)
	}
}

func TestRenderDiffWithHunk(t *testing.T) {
	hunkBin, err := exec.LookPath("hunk")
	if err != nil {
		t.Skip("hunk not installed")
	}
	gitRepo(t)
	second := bySubject(t, collectLog(t, logOpts{}), "second commit")
	for _, sbs := range []bool{true, false} {
		out, err := renderDiff(context.Background(), second, 90, sbs, diffTool{name: toolHunk, bin: hunkBin}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(ansi.Strip(out), "three") {
			t.Errorf("sbs=%v: diff content missing:\n%s", sbs, ansi.Strip(out))
		}
		lines := strings.Split(out, "\n")
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > 90 {
				t.Errorf("sbs=%v: line is %d cells, over the pty's 90", sbs, w)
			}
		}
		if strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
			t.Errorf("sbs=%v: the blank rows of the tall screen should be cut", sbs)
		}
		if files := fileLines(out); len(files) != 2 {
			t.Errorf("sbs=%v: hunk file headers found at %v, want 2:\n%s", sbs, files, ansi.Strip(out))
		}
	}
	// The first frame is handed over before the syntax highlighting is in:
	// the same text, so the final render replaces it without anything moving.
	var first string
	out, err := renderDiff(context.Background(), second, 90, true, diffTool{name: toolHunk, bin: hunkBin}, nil, func(s string) { first = s })
	if err != nil || first == "" || ansi.Strip(first) != ansi.Strip(out) {
		t.Errorf("early frame: err=%v\n%s\n--- final:\n%s", err, ansi.Strip(first), ansi.Strip(out))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := renderDiff(ctx, second, 90, true, diffTool{name: toolHunk, bin: hunkBin}, nil, nil); err == nil {
		t.Error("a cancelled render should fail")
	}
}

// TestRenderDiffIgnoringWhitespace: -w keeps the real changes and says so when
// there is nothing else. The uncommitted changes are the commit under test.
func TestRenderDiffIgnoringWhitespace(t *testing.T) {
	dir := gitRepo(t)
	wt := commit{wt: true}
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	render := func(tool diffTool) string {
		t.Helper()
		out, err := renderDiff(context.Background(), &wt, 90, false, tool, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		return ansi.Strip(out)
	}
	write("one\n\t2\nthree\nfour\n") // HEAD has one, 2, three
	if out := render(diffTool{}); !strings.Contains(out, "-2") || !strings.Contains(out, "+four") {
		t.Errorf("the plain diff shows the indentation change:\n%s", out)
	}
	if out := render(diffTool{ignoreWS: true}); strings.Contains(out, "-2") || !strings.Contains(out, "+four") {
		t.Errorf("-w keeps +four and drops the indentation change:\n%s", out)
	}

	write("one\n\t2\nthree\n")
	msg := renderPreviewCmd(context.Background(), wt, 90, diffSingle, diffTool{ignoreWS: true}, nil)().(previewMsg)
	if msg.err != nil || !strings.Contains(ansi.Strip(msg.render.content), "only whitespace changes") {
		t.Errorf("a commit that only changed whitespace says so: err=%v\n%s", msg.err, ansi.Strip(msg.render.content))
	}
	if previewKey("h", 90, diffTool{}, diffSingle) == previewKey("h", 90, diffTool{ignoreWS: true}, diffSingle) {
		t.Error("the two renders of a commit need their own key")
	}
}

func TestRenderPreviewUsesTheDiskCache(t *testing.T) {
	deltaBin, err := exec.LookPath("delta")
	if err != nil {
		t.Skip("delta not installed")
	}
	gitRepo(t)
	renderCache = testCache(t)
	t.Cleanup(func() { renderCache = nil })
	second := bySubject(t, collectLog(t, logOpts{}), "second commit")
	tool := diffTool{name: toolDelta, bin: deltaBin}
	first := renderPreviewCmd(context.Background(), *second, 90, diffSingle, tool, nil)().(previewMsg)
	if first.err != nil {
		t.Fatal(first.err)
	}
	stored, ok := renderCache.get(tool, second.hash, 90, diffSingle)
	if !ok || !strings.Contains(first.render.content, stored) {
		t.Fatalf("the diff should be stored as rendered: ok=%v", ok)
	}
	// Served from the disk: the stored text is what shows, under a fresh header.
	renderCache.put(tool, second.hash, 90, diffSingle, "FROM THE CACHE")
	again := renderPreviewCmd(context.Background(), *second, 90, diffSingle, tool, nil)().(previewMsg)
	if again.err != nil || !strings.Contains(again.render.content, "FROM THE CACHE") || !strings.Contains(again.render.content, "second commit") {
		t.Errorf("second render: err=%v\n%s", again.err, again.render.content)
	}
}

func TestRenderIDCoversThePaths(t *testing.T) {
	if renderID("", []string{"src"}) != "" {
		t.Errorf("the working tree row has no id: it is never stored")
	}
	if renderID("abc", nil) == renderID("abc", []string{"src"}) {
		t.Errorf("a log limited to paths must not share the renders of the whole commit")
	}
}
