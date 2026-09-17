package main

// The commit preview: a natively rendered header (hash, refs, author, date,
// stat, message) followed by the diff as rendered by delta. Diff rendering is
// delegated on purpose: `git show | delta --width=N [--side-by-side]` and the
// ANSI output goes into a viewport as is. A render can take a while on large
// commits, so it runs as a tea.Cmd and results are cached per (commit, width,
// diff mode).

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// maxDiffBytes caps the rendered diff kept in memory; a vendored-dependency
// commit can otherwise produce hundreds of megabytes of ANSI.
const maxDiffBytes = 16 << 20

type previewMsg struct {
	key     string
	hash    string
	detail  detail
	content string
	err     error
	// cancelled marks a render whose context was cancelled because the
	// selection moved on; its error is not worth showing.
	cancelled bool
}

func previewKey(hash string, width int, mode string) string {
	return hash + "|" + strconv.Itoa(width) + "|" + mode
}

// renderPreviewCmd renders header + diff for c at width. ctx cancels the git
// and delta processes when the selection moves on before they finish.
func renderPreviewCmd(ctx context.Context, c commit, width int, mode, deltaBin string) tea.Cmd {
	key := previewKey(c.hash, width, mode)
	return func() tea.Msg {
		d, err := loadDetail(ctx, c.hash)
		if err != nil {
			return previewMsg{key: key, hash: c.hash, err: err, cancelled: ctx.Err() != nil}
		}
		diff, err := renderDiff(ctx, c.hash, width, mode == diffSBS, deltaBin)
		if err != nil || ctx.Err() != nil {
			if err == nil {
				err = ctx.Err()
			}
			return previewMsg{key: key, hash: c.hash, err: err, cancelled: ctx.Err() != nil}
		}
		if diff == "" {
			diff = stDim.Render("(no diff)")
		}
		content := previewHeader(&c, &d, width) + "\n\n" + diff
		return previewMsg{key: key, hash: c.hash, detail: d, content: content}
	}
}

// renderDiff pipes the commit's patch through delta. delta ignores COLUMNS
// and falls back to 80 columns when stdout is not a tty, so the width is
// always explicit. Without delta the patch falls back to git's own colors.
func renderDiff(ctx context.Context, hash string, width int, sbs bool, deltaBin string) (string, error) {
	if deltaBin == "" {
		git := exec.CommandContext(ctx, "git", "show", "--format=", "--color=always", hash)
		out, err := limitedOutput(git)
		return strings.ReplaceAll(out, "\t", "    "), err
	}
	git := exec.CommandContext(ctx, "git", "show", "--format=", "--no-color", hash)
	args := []string{"--width=" + strconv.Itoa(width), "--paging=never"}
	if sbs {
		args = append(args, "--side-by-side")
	}
	delta := exec.CommandContext(ctx, deltaBin, args...)
	pipe, err := git.StdoutPipe()
	if err != nil {
		return "", err
	}
	delta.Stdin = pipe
	if err := git.Start(); err != nil {
		return "", err
	}
	out, err := limitedOutput(delta)
	_ = git.Wait() // fails with SIGPIPE when the output was capped; delta's result is what counts
	return out, err
}

// limitedOutput runs cmd and returns at most maxDiffBytes of its stdout, cut
// at a line boundary with a note when the cap was hit.
func limitedOutput(cmd *exec.Cmd) (string, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	data, _ := io.ReadAll(io.LimitReader(stdout, maxDiffBytes+1))
	capped := len(data) > maxDiffBytes
	if capped {
		_ = cmd.Process.Kill()
		data = data[:maxDiffBytes]
		if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
			data = data[:i]
		}
	}
	err = cmd.Wait()
	out := strings.Trim(string(data), "\n") // delta opens with a blank line
	if capped {
		return out + "\n\n" + stDim.Render("(diff truncated at "+strconv.Itoa(maxDiffBytes>>20)+" MiB)"), nil
	}
	if err != nil && out == "" {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", &renderError{firstLine(msg)}
		}
		return "", err
	}
	return out, nil
}

type renderError struct{ msg string }

func (e *renderError) Error() string { return e.msg }

// previewHeader mirrors `git show`'s header with a Stat line added. d is nil
// while the detail is still loading: everything the list already knows renders
// right away and the stat/body fill in when the render lands.
func previewHeader(c *commit, d *detail, width int) string {
	first := stHash.Render("commit " + c.hash)
	if len(c.refs) > 0 {
		first += stHash.Render(" (") + renderSegs(refSegs(c), nil, false) + stHash.Render(")")
	}
	stat := stDim.Render("…")
	if d != nil {
		stat = statLine(*d)
	}
	lines := []string{
		wrapIndent(first, width, 0),
		wrapIndent(stKey.Render("Author:")+" "+c.author()+" <"+c.email()+">", width, 0),
		stKey.Render("Date:") + "   " + c.dateTime(),
		stKey.Render("Stat:") + "   " + stat,
		"",
		wrapIndent(stTitle.Render(c.subject()), width, 4),
	}
	if d != nil && d.body != "" {
		lines = append(lines, "")
		for _, l := range strings.Split(d.body, "\n") {
			lines = append(lines, wrapIndent(l, width, 4))
		}
	}
	return strings.Join(lines, "\n")
}

func statLine(d detail) string {
	if d.files == 0 && d.added == 0 && d.deleted == 0 {
		return "no changes"
	}
	return plural(d.files, "file") + ", " +
		stAdded.Render("+"+strconv.Itoa(d.added)) + " " + stDeleted.Render("-"+strconv.Itoa(d.deleted))
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// wrapIndent word-wraps s to width and indents every resulting line.
func wrapIndent(s string, width, indent int) string {
	w := max(10, width-indent)
	s = ansi.Wrap(strings.ReplaceAll(s, "\t", "    "), w, "")
	if indent == 0 {
		return s
	}
	prefix := strings.Repeat(" ", indent)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}
