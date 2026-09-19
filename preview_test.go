package main

import (
	"context"
	"os/exec"
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
