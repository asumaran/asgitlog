package main

// The commit preview: a natively rendered header (hash, refs, author, date,
// stat, message, changed files) followed by the diff as rendered by delta.
// Diff rendering is delegated on purpose: `git show | delta --width=N
// [--side-by-side]` and the ANSI output goes into a viewport as is. A render
// can take a while on large commits, so it runs as a tea.Cmd and results are
// cached per (commit, width, diff mode).

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	// maxDiffBytes caps the rendered diff kept in memory; a vendored-dependency
	// commit can otherwise produce hundreds of megabytes of ANSI.
	maxDiffBytes = 16 << 20
	// maxHeaderFiles caps the changed-files (and untracked) list of the
	// header; past that the list would bury the diff.
	maxHeaderFiles = 50
)

// render is one cached preview: the content and the lines where each file's
// diff starts (for jumping between files).
// A render is partial while its tool is still refining it (hunk's syntax
// highlighting): it is shown, and replaced when the final one arrives.
type render struct {
	content string
	files   []int
	partial bool
}

type previewMsg struct {
	key    string
	hash   string
	detail detail
	render render
	err    error
	// cancelled marks a render whose context was cancelled because the
	// selection moved on; its error is not worth showing.
	cancelled bool
	// next waits for what follows a partial render: the final one, or how it
	// ended. The pipeline is still running until that reports back.
	next tea.Cmd
}

// previewKey identifies a render. mode is the effective diff mode (auto is
// resolved by the caller), so auto and an explicit mode share their renders.
func previewKey(hash string, width int, tool diffTool, mode string) string {
	if tool.ignoreWS {
		mode += "-w"
	}
	return hash + "|" + strconv.Itoa(width) + "|" + tool.name + "|" + mode
}

// renderPreviewCmd renders header + diff for c at width. ctx cancels the git
// and delta processes when the selection moves on before they finish.
func renderPreviewCmd(ctx context.Context, c commit, width int, mode string, tool diffTool, paths []string) tea.Cmd {
	key := previewKey(c.hash, width, tool, mode)
	// At most a partial and a final message: the pipeline never blocks on a
	// program that went away.
	msgs := make(chan previewMsg, 2)
	next := func() tea.Msg { return <-msgs }
	run := func() previewMsg {
		fail := func(err error) previewMsg {
			return previewMsg{key: key, hash: c.hash, err: err, cancelled: ctx.Err() != nil}
		}
		d, err := loadDetail(ctx, &c, paths)
		if err != nil {
			return fail(err)
		}
		rendered := func(diff string, partial bool) previewMsg {
			content := previewHeader(&c, &d, width)
			if diff == "" && tool.ignoreWS && len(d.files) > 0 {
				diff = stDim.Render("(only whitespace changes)")
			}
			if diff != "" { // an empty commit already says "no changes"
				content += "\n\n" + sectionRule(stLabel.Render("diff"), width) + "\n\n" + diff
			}
			return previewMsg{key: key, hash: c.hash, detail: d, render: render{content, fileLines(content), partial}}
		}
		id := renderID(c.hash, paths)
		if diff, ok := renderCache.get(tool, id, width, mode); ok {
			return rendered(diff, false)
		}
		diff, err := renderDiff(ctx, &c, width, mode == diffSBS, tool, paths, func(diff string) {
			msg := rendered(diff, true)
			msg.next = next
			msgs <- msg
		})
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			return fail(err)
		}
		if tool.bin != "" { // plain git is as fast as reading it back
			renderCache.put(tool, id, width, mode, diff)
		}
		return rendered(diff, false)
	}
	return func() tea.Msg {
		go func() { msgs <- run() }()
		return next()
	}
}

// renderID addresses a commit's render in the disk cache: a commit never
// changes, so its hash (and what the log is limited to) says it all. The
// working tree row has no hash and is never stored.
func renderID(hash string, paths []string) string {
	if hash == "" {
		return ""
	}
	return strings.Join(append([]string{hash}, paths...), "\x00")
}

// renderDiff draws the commit's patch with tool (see renderPatch).
func renderDiff(ctx context.Context, c *commit, width int, sbs bool, tool diffTool, paths []string, early func(string)) (string, error) {
	var args []string
	if c.wt {
		args = []string{"-c", "core.quotepath=false", "diff", "HEAD", tool.colorArg()}
	} else {
		args = append(append([]string{}, showArgs...), tool.colorArg(), "--format=", c.hash)
	}
	if tool.ignoreWS {
		args = append(args, "-w")
	}
	patch, err := limitedOutput(exec.CommandContext(ctx, "git", append(args, pathArgs(paths)...)...), false)
	if err != nil {
		return "", err
	}
	return renderPatch(ctx, tool, []byte(patch), width, sbs, early)
}

// hunkFileLine is hunk's file header: the path and the counts at the ends of
// an otherwise blank line.
var hunkFileLine = regexp.MustCompile(`^ \S.*\s\+\d+ -\d+\s*$`)

// fileLines finds where each file's diff starts in a rendered preview: delta
// prints the path over a rule of "─", hunk under one (or a blank line, for the
// first file) with its counts, plain git a "diff --git" line.
func fileLines(content string) []int {
	lines := strings.Split(content, "\n")
	var out []int
	for i, l := range lines {
		raw := ansi.Strip(l)
		plain := strings.TrimSpace(raw)
		if strings.HasPrefix(plain, "diff --git ") {
			out = append(out, i)
			continue
		}
		if hunkFileLine.MatchString(raw) && (i == 0 || strings.Trim(ansi.Strip(lines[i-1]), " ─") == "") {
			out = append(out, i)
			continue
		}
		// A path line: not blank, under a blank line (or first), over a rule.
		if plain == "" || i+1 >= len(lines) || i > 0 && strings.TrimSpace(ansi.Strip(lines[i-1])) != "" {
			continue
		}
		rule := strings.TrimSpace(ansi.Strip(lines[i+1]))
		if len(rule) >= 3*10 && strings.Trim(rule, "─") == "" && strings.Trim(plain, "─") != "" {
			out = append(out, i)
		}
	}
	return out
}

// previewHeader mirrors `git show`'s header with a Stat line and the changed
// files added. d is nil while the detail is still loading: everything the list
// already knows renders right away and the rest fills in with the render.
func previewHeader(c *commit, d *detail, width int) string {
	var lines []string
	if c.wt {
		lines = []string{stHash.Render("Working tree") + stDim.Render(" (uncommitted changes against HEAD)")}
	} else {
		first := stHash.Render("commit " + c.hash)
		if len(c.refs) > 0 {
			first += stHash.Render(" (") + renderSegs(refSegs(c), nil, false) + stHash.Render(")")
		}
		lines = []string{wrapIndent(first, width, 0)}
		if c.merge() {
			var short []string
			for _, p := range strings.Fields(c.parents) {
				short = append(short, p[:min(len(p), len(c.short()))])
			}
			lines = append(lines, stKey.Render("Merge:")+"  "+strings.Join(short, " ")+stDim.Render("  (diff against the first parent)"))
		}
		lines = append(lines,
			wrapIndent(stKey.Render("Author:")+" "+c.author()+" <"+c.email()+">", width, 0),
			stKey.Render("Date:")+"   "+c.dateTime(),
			"",
			wrapIndent(stTitle.Render(c.subject()), width, 4),
		)
		if d != nil && d.body != "" {
			lines = append(lines, "")
			for _, l := range strings.Split(d.body, "\n") {
				lines = append(lines, wrapIndent(l, width, 4))
			}
		}
	}
	if d != nil {
		// The totals title the file list, so the numbers sit with what they
		// count instead of floating in the header.
		lines = append(lines, "", sectionRule(statLine(*d), width))
		lines = append(lines, fileList(d.files, width)...)
		if len(d.untracked) > 0 {
			lines = append(lines, "", sectionRule(stLabel.Render(strconv.Itoa(len(d.untracked))+" untracked"), width))
			for i, p := range d.untracked {
				if i == maxHeaderFiles {
					lines = append(lines, stDim.Render("    … "+strconv.Itoa(len(d.untracked)-i)+" more"))
					break
				}
				lines = append(lines, "    "+truncate(p, max(10, width-4)))
			}
		}
	}
	return strings.Join(lines, "\n")
}

// sectionRule titles a block of the preview ("── 3 files changed ────"), so
// the message, the file list and the diff read as separate parts.
func sectionRule(title string, width int) string {
	title = " " + title + " " // already styled by the caller
	fill := max(0, width-2-ansi.StringWidth(title))
	return stDim.Render("──") + title + stDim.Render(strings.Repeat("─", fill))
}

// fileList is the per-file line counts, paths aligned in one column. Paths
// are never cut: one that does not fit the column pushes its counts to the
// right, and one that does not fit the line at all wraps.
func fileList(files []fileStat, width int) []string {
	const indent = 4
	shown := files[:min(len(files), maxHeaderFiles)]
	counts := make([]string, len(shown))
	pathW, countsW := 0, 0
	// The + and - counts are two right-aligned columns of their own.
	addW, delW := 0, 0
	for _, f := range shown {
		addW = max(addW, len(strconv.Itoa(f.added))+1)
		delW = max(delW, len(strconv.Itoa(f.deleted))+1)
	}
	padLeft := func(s string, w int) string { return strings.Repeat(" ", max(0, w-len(s))) + s }
	for i, f := range shown {
		counts[i] = stAdded.Render(padLeft("+"+strconv.Itoa(f.added), addW)) + " " +
			stDeleted.Render(padLeft("-"+strconv.Itoa(f.deleted), delW))
		if f.binary {
			counts[i] = stDim.Render(padLeft("binary", addW+1+delW))
		}
		pathW = max(pathW, ansi.StringWidth(f.path))
		countsW = max(countsW, ansi.StringWidth(counts[i]))
	}
	room := max(10, width-indent)              // what a line can hold after the indent
	pathW = min(pathW, max(1, room-2-countsW)) // the aligned column
	prefix := strings.Repeat(" ", indent)
	lines := make([]string, 0, len(shown)+1)
	for i, f := range shown {
		w := ansi.StringWidth(f.path)
		switch {
		case w <= pathW:
			lines = append(lines, prefix+f.path+strings.Repeat(" ", pathW-w)+"  "+counts[i])
		case w+2+ansi.StringWidth(counts[i]) <= room:
			lines = append(lines, prefix+f.path+"  "+counts[i])
		default:
			wrapped := strings.Split(ansi.Hardwrap(f.path, room, false), "\n")
			for _, l := range wrapped {
				lines = append(lines, prefix+l)
			}
			if last := wrapped[len(wrapped)-1]; ansi.StringWidth(last)+2+ansi.StringWidth(counts[i]) <= room {
				lines[len(lines)-1] += "  " + counts[i]
			} else {
				lines = append(lines, prefix+"  "+counts[i])
			}
		}
	}
	if more := len(files) - len(shown); more > 0 {
		lines = append(lines, stDim.Render(prefix+"… "+plural(more, "more file")))
	}
	return lines
}

// statLine is the title of the file list: "3 files changed  +10 -2".
func statLine(d detail) string {
	if len(d.files) == 0 {
		return stLabel.Render("no changes")
	}
	return stLabel.Render(plural(len(d.files), "file")+" changed") + "  " +
		stAdded.Render("+"+strconv.Itoa(d.added)) + " " + stDeleted.Render("-"+strconv.Itoa(d.deleted))
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
