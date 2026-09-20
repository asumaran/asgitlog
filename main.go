// asgitlog: browse the git history of the current repository. A fuzzy
// filterable commit list with a preview of the selected commit: a native
// header (hash, refs, author, date, stat, message) and the diff rendered by
// delta. Enter opens the diff full screen. Runs as a herdr plugin popup and
// from a plain shell; either way it works on the repository of the current
// directory.
//
//	asgitlog [flags] [<revision range>...] [-- <path>...]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"slices"
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
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: asgitlog [flags] [<revision range>...] [-- <path>...]")
		flag.PrintDefaults()
	}
	// The flag package swallows the "--" that separates revisions from paths,
	// so the command line is split on it first.
	args, paths := os.Args[1:], []string(nil)
	if i := slices.Index(args, "--"); i >= 0 {
		args, paths = args[:i], args[i+1:]
	}
	_ = flag.CommandLine.Parse(args)
	opts := logOpts{revs: flag.Args(), paths: paths}

	if *showVersion {
		fmt.Println(version)
		return
	}

	enterPaneCwd()
	m := newModel(loadPrefs(), toolBin("asgitlog", "delta"), opts)
	m.hunkBin = toolBin("asgitlog", "hunk")
	renderCache = openDiskCache("asgitlog")

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

	go renderCache.prune(cacheMaxBytes)
	// The log stream starts in Init. Alt screen and mouse mode are declared per frame by View().
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if m.mode == modeFatal {
		os.Exit(1)
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
	fmt.Printf("prefs:  layout=%s diff=%s ignore-whitespace=%v split-rows=%d split-columns=%d (%s)\n",
		m.prefs.layout, m.prefs.diff, m.prefs.ignoreWS, m.prefs.splitRows, m.prefs.splitColumns, homeRel(prefsDir()))
	if s := m.scope(); s != "" {
		fmt.Println("scope: ", s)
	}
	delta := m.deltaBin
	if delta == "" {
		delta = "not found (plain git colors)"
	}
	fmt.Println("delta: ", delta)
	if m.hunkBin != "" {
		fmt.Println("hunk:  ", m.hunkBin)
	}
	fmt.Println("diffs:  by", m.tool().name)
	if renderCache != nil {
		fmt.Println("cache: ", homeRel(renderCache.dir))
	}

	for b := range streamLog(context.Background(), m.opts, m.logGen) {
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
			mode := effectiveDiff(m.prefs.diff, width)
			msg := renderPreviewCmd(context.Background(), m.commits[i], width, mode, m.tool(), m.opts.paths)().(previewMsg)
			for msg.next != nil { // a partial render: wait for the final one
				msg = msg.next().(previewMsg)
			}
			if msg.err != nil {
				fmt.Fprintln(os.Stderr, "asgitlog:", msg.err)
				return 1
			}
			fmt.Println(strings.Repeat("-", width))
			out(msg.render.content)
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
	layout := m.rowLayout()
	layout.width = width
	for i := 0; i < m.rowCount() && (limit <= 0 || i < limit); i++ {
		c, matched := m.rowAt(i)
		out(renderSegs(rowSegs(c, layout, false), matched, false))
	}
	return 0
}
