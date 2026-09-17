// asgitlog: browse the git history of the current repository. A fuzzy
// filterable commit list with a preview of the selected commit: a native
// header (hash, refs, author, date, stat, message) and the diff rendered by
// delta. Enter opens the diff full screen. Runs as a herdr plugin popup and
// from a plain shell; either way it works on the repository of the current
// directory.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// version is the release tag; overridden at build time via
// -ldflags "-X main.version=vX.Y.Z" (see scripts/release.sh and CI).
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the embedded version")
	dump := flag.Bool("dump", false, "print the repo summary, settings and list rows (no TUI)")
	query := flag.String("query", "", "with -dump: filter the list with this query")
	show := flag.String("show", "", "with -dump: print the preview of this commit instead of the list")
	limit := flag.Int("n", 20, "with -dump: number of rows to print (0 = all)")
	width := flag.Int("width", 120, "with -dump: width to lay the output out for")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	enterPaneCwd()
	deltaBin, _ := exec.LookPath("delta")
	m := newModel(loadPrefs(), deltaBin)

	if !insideWorkTree() {
		cwd, _ := os.Getwd()
		msg := "not inside a git work tree: " + homeRel(cwd)
		// A popup closes with the process and takes stderr with it, so the
		// error has to be shown inside the TUI there.
		if *dump || os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") == "" {
			fmt.Fprintln(os.Stderr, "asgitlog: "+msg)
			os.Exit(1)
		}
		m.mode, m.fatal = modeFatal, msg
	}

	if *dump {
		os.Exit(runDump(m, *query, *show, *limit, *width))
	}

	if m.mode != modeFatal {
		ctx, cancel := context.WithCancel(context.Background())
		m.logCh, m.stopLog = streamLog(ctx), cancel
	}
	// Alt screen and mouse mode are declared per frame by View().
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if m.mode == modeFatal {
		os.Exit(1)
	}
}

// enterPaneCwd moves to the directory of the pane that was focused when the
// popup opened. herdr starts plugin panes in the plugin's own directory (the
// manifest's "./asgitlog" is resolved against it, so the pane cannot simply be
// opened with another cwd) and describes the invocation, focused pane
// included, in HERDR_PLUGIN_CONTEXT_JSON.
func enterPaneCwd() {
	if os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") == "" {
		return
	}
	var ctx struct {
		FocusedPaneCwd string `json:"focused_pane_cwd"`
		WorkspaceCwd   string `json:"workspace_cwd"`
	}
	if json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")), &ctx) != nil {
		return
	}
	for _, dir := range []string{ctx.FocusedPaneCwd, ctx.WorkspaceCwd} {
		if dir != "" && os.Chdir(dir) == nil {
			return
		}
	}
}

// runDump prints what the TUI would show, without a TTY: the repo summary,
// the settings and the list rows laid out for width (filtered by query), or
// with show the preview of one commit. Colors are kept only on a terminal.
func runDump(m *model, query, show string, limit, width int) int {
	out := func(s string) {
		if !term.IsTerminal(os.Stdout.Fd()) {
			s = ansi.Strip(s)
		}
		fmt.Println(s)
	}
	fmt.Println("repo:  ", loadRepoInfo())
	fmt.Printf("prefs:  layout=%s diff=%s (%s)\n", m.prefs.layout, m.prefs.diff, homeRel(prefsDir()))
	delta := m.deltaBin
	if delta == "" {
		delta = "not found (plain git colors)"
	}
	fmt.Println("delta: ", delta)

	for b := range streamLog(context.Background()) {
		m.addCommits(b.commits)
		if b.err != nil {
			fmt.Fprintln(os.Stderr, "git log:", b.err)
			return 1
		}
	}

	if show != "" {
		full, err := runGit(context.Background(), "rev-parse", "--verify", show+"^{commit}")
		if err != nil {
			fmt.Fprintln(os.Stderr, "asgitlog:", err)
			return 1
		}
		for i := range m.commits {
			if m.commits[i].hash != full {
				continue
			}
			msg := renderPreviewCmd(context.Background(), m.commits[i], width, m.prefs.diff, m.deltaBin)().(previewMsg)
			if msg.err != nil {
				fmt.Fprintln(os.Stderr, "asgitlog:", msg.err)
				return 1
			}
			fmt.Println(strings.Repeat("-", width))
			out(msg.content)
			return 0
		}
		fmt.Fprintln(os.Stderr, "asgitlog: commit is not in the log of HEAD:", show)
		return 1
	}

	m.ti.SetValue(query)
	m.applyQuery()
	fmt.Printf("commits: %d", len(m.commits))
	if m.filtering() {
		fmt.Printf(", %d matching %q", m.rowCount(), query)
	}
	fmt.Println()
	for i := 0; i < m.rowCount() && (limit <= 0 || i < limit); i++ {
		c, matched := m.rowAt(i)
		out(renderSegs(rowSegs(c, width, m.hashW, m.columns(), false), matched, false))
	}
	return 0
}
