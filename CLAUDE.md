# CLAUDE.md

Guidance for working in this repository.

## What this is

`asgitlog` browses the git history of the current repository: a filterable
commit list with a preview of the selected commit (a native header with hash,
refs, author, date, stat, message and changed files, then the diff rendered by
[delta](https://github.com/dandavison/delta)). Enter opens the diff full
screen. It replaces the `fl` zsh/fzf function from the dotfiles and runs two
ways from the same binary: as a herdr plugin popup (on the repository of the
pane that was focused) and from a plain shell (on the current directory,
optionally scoped: `asgitlog [<revision range>...] [-- <path>...]`).

Distributed as a herdr plugin (`herdr plugin install asumaran/asgitlog`; the
manifest's `[[build]]` runs `scripts/fetch-binary.sh`). Each GitHub Release
attaches `asgitlog-darwin-arm64`. There is no published library. Modeled on
`gotopr` (siblings: `gotojira`, `herdr-goto`).

## Stack & layout

Go single module, single `package main`, static binary. TUI: Bubble Tea v2 +
bubbles v2 (`textinput`, `viewport`, `key`, `help`), lipgloss v2,
`sahilm/fuzzy` for the opt-in fuzzy terms. The charm v2 modules are imported
under their canonical `charm.land/<name>/v2` paths (the
`github.com/charmbracelet/<name>/v2` spelling is rejected by `go get`). Files
are split by concern but everything stays in `package main`:

- `main.go`: flags (`-version`, `-dump`, `-query`, `-show`, `-n`, `-width`),
  revision/path arguments, `enterPaneCwd` (plugin pane → focused pane's
  directory), work tree check, `tea.NewProgram`, `runDump`.
- `git.go`: `runGit`, repo summary and remote web URL (`repoInfo`), log scope
  (`logOpts`), the streamed `git log` (`streamLog`, `parseCommit`,
  decorations, the working tree row), per-commit `detail` (body + numstat).
- `filter.go`: substring/fuzzy terms, hits in log order, narrowing.
- `list.go`: row segments, the wide and compact formats, relative dates,
  match highlighting.
- `preview.go`: native header with the file list, `git show | delta` as a
  `tea.Cmd`, file header detection, output cap.
- `hunk.go`: hunk as the alternative diff renderer, captured off a pty.
- `prefs.go`: persisted layout, diff mode and split sizes.
- `cache.go`: the rendered diffs kept on disk between runs.
- `ui.go`: the bubbletea model/Update/View, modes, geometry, pooled
  preview rendering with prefetch, full view and its search, actions, mouse,
  styles.
- `scripts/pty-check.py`: end-to-end driver (see Testing).

## Build & run

```bash
go build -o asgitlog .          # plugin runs ./asgitlog from the repo root
./asgitlog                      # TUI on the repository of the current directory
./asgitlog main..dev -- src/    # scoped to a range and/or paths
./asgitlog -dump                # repo summary, settings, first rows; no TTY
./asgitlog -dump -query x -n 0  # every row matching a query
./asgitlog -dump -show HEAD~2 -width 140   # one commit's preview (header + delta)
go vet ./... && go test ./...
scripts/pty-check.py ./asgitlog
herdr plugin link ~/Developer/asgitlog   # link does NOT run [[build]]; go build yourself
```

Keybinding (user config): `plugin_action` `asumaran.asgitlog.open` →
`scripts/open-pane.sh` → `herdr plugin pane open` (popup, 85% x 90%).

## Behaviour / decisions

- **Data**: one `git log -z` with unit-separated fields
  (`%H %h %an %ae %ad %D %P %at %s`, `--decorate=full` so refs can be
  classified, `<remote>/HEAD` excluded), streamed in batches (200 commits
  first, then 4000) so large histories show up immediately. Each commit is
  ONE string, the search corpus (`short author <email> subject refs date`);
  every displayed field is a slice of it. That keeps a big history at one
  allocation per commit and gives each field's byte offset in the corpus,
  which is what the matchers report, so matches are highlighted in place.
  `sahilm/fuzzy` reports BYTE offsets, not rune indexes.
- **Log scope** (`logOpts`): revisions and paths from the command line
  (`os.Args` is split on `--` before `flag` sees it, because `flag` swallows
  it), `ctrl+a` toggles `--all`, `ctrl+g` asks for a text and rescopes to
  `git log -S<text>` (empty text lifts it). A scope change restarts the
  stream: batches carry a generation and stale ones are dropped; `seekHash`
  lands the cursor on the same commit when it shows up again. The scope is
  shown on the counter rule (the summary line is mostly path in the narrow
  columns layout). Paths also limit the numstat and the diff of the preview.
- **Working tree row**: when `git status --porcelain` is not empty (and HEAD
  exists, and no content search is active) the stream leads with a
  pseudo-commit (`wt`, hash `""`, short `*`) whose preview is
  `git diff HEAD` plus the untracked files. The status check runs while
  `git log` starts, so it does not delay the first batch much. It is rendered
  once per session like any commit.
- **Layout is computed at render time** from structs, so a resize or a layout
  change never re-runs git and the cursor trivially stays on the same commit.
  Only the visible window of rows is rendered. The screen is ONE FRAME of
  four sections (`listView`, built with `hline`/`framed`/`fit`, all lines
  exactly the terminal width) that share their edges (`├─┤`), so no line goes
  to a border of their own (four separate boxes were tried first; the doubled
  borders read as gaps and cost three lines): the repo summary; the filter
  input, whose top edge carries `matches/total [scope]` and the loading mark;
  the main section, holding the list AND the commit details split by a divider
  (`mainLines`); and the help, which grows when `?` expands it (the main
  section gives way). The edge over the details (the divider in rows, the top
  edge in columns) is a plain line: it used to carry the diff mode and the
  scrolled-away commit, which was dropped as noise. The main section's bottom
  edge carries `line/total`. Details have a cell of padding, list rows use
  their own 2-cell gutter.
  - The list reads TOP-DOWN in both layouts: row 0, the newest commit, is the
    first line, right under the filter input. (A bottom-up list like fzf's
    default was tried while the input sat below the list; with the input on
    top it read backwards and was dropped.)
  - Rows layout (details below, horizontal `├─┤` divider). Wide rows: hash, author (as wide as
    the longest name loaded, max 15), subject, refs (only the room they need,
    up to 45% of the subject area, right-aligned against the date), date. No
    email column; it stays searchable. A narrow list drops the author.
  - Columns layout (details beside, vertical divider tied into the edges with
    `┬`/`┴`). Compact rows: hash,
    relative date, subject. Falls back to rows under 60 columns without
    touching the saved setting.
  - `shift+←/→` moves the divider in 5% steps (30-85% for the preview),
    persisted per layout.
  - Merge subjects are faint; the working tree row is yellow italic.
- **Filter**: whitespace-separated terms are ANDed. A plain term is a
  case-insensitive SUBSTRING (every occurrence is highlighted); `~term` is a
  fuzzy subsequence. Substring is the default because a subsequence over a
  whole row matches almost anything ("fix pane" hit 355 of herdr's 1686
  commits). Hits keep log order. Extending the query narrows the previous
  hits instead of rescanning. The selection stays on the same commit while it
  still matches, else the first hit; so deleting the query ends on the commit
  that was found. Commits streamed in while a query is active are filtered as
  they arrive. `foldIndexAll` avoids lowercasing the corpus for ASCII terms.
- **Diff rendering is delegated to delta**, never reimplemented:
  `git show -m --first-parent --format= <hash> | delta --width=N
  --paging=never [--side-by-side]`, ANSI output straight into a `viewport`.
  delta ignores `COLUMNS` and assumes 80 columns when stdout is not a tty, so
  `--width` is always explicit (the viewport width: the frame costs 4 columns).
  `-m --first-parent` makes a merge show what it brought in; git show's
  combined diff is empty for a clean merge. Diff mode: `auto` (side by side
  from 120 columns of preview, the dotfiles' `delta-pager` threshold), `sbs`,
  `single`; `ctrl+t` cycles and flashes the new mode in the help line (the
  list has no standing label for it; the full view's title does). The cache
  key uses the EFFECTIVE mode, so auto shares renders with the explicit modes.
  Without delta in `PATH` the diff falls back to `git show --color=always`
  (the `ctrl+t` flash says so). Output is capped at 16 MiB.
- **hunk as an alternative renderer** (`ctrl+r`, setting `renderer`, only
  when `hunk` is in `PATH`; a saved `hunk` without the binary falls back to
  delta). Only its looks are wanted. hunk has NO static output (`hunk pager`
  passes the patch through when stdout is not a tty, `hunk patch` starts its
  TUI regardless), so `renderHunk` runs `hunk patch <tmpfile> --pager
  --no-sidebar --no-extensions --cursor-line off --no-wrap --mode split|unified`
  on a pty as wide as the viewport and TALL enough for the whole patch
  (`hunkRows`: a row per patch line plus file chrome, capped at 4000 rows,
  since the emulated screen is rows x width cells and costs ~170 MiB at the
  cap; long lines are CUT: `--wrap` was tried and dropped, it breaks lines
  mid-word), feeds the output to `charmbracelet/x/vt`, and takes
  `Render()`'s lines minus the blank tail. The emulator's answers to hunk's
  terminal queries are copied back to the pty (its reply pipe blocks
  otherwise). hunk paints the diff at once (~0.2 s) and repaints as syntax
  highlighting comes in (3-5x longer), with no end mark: the render is done
  after 300 ms without output, then hunk is killed. Waiting that out made
  every commit feel slow, so renders are PROGRESSIVE: the first frame (a
  closed synchronized-output frame, or a 40 ms pause) is reported as a
  `partial` render whose `previewMsg.next` waits for the final one. A partial
  render is shown but stays the one in flight (no prefetch meanwhile); the
  final one replaces it in place (same text, only colors change, so the scroll
  offset holds). A cancelled pipeline drops its partial render, or it would
  never be refined; one that fails after its first frame keeps it.
  `HUNK_MCP_DISABLE=1` is required or every render leaves a `hunk daemon
  serve` behind. The tool's name is part of the render cache key. File jumps
  match hunk's header (` path … +A -D` under a rule or a blank line).
- **Preview header is native** (lipgloss): `commit <hash> (refs)`, `Merge:`
  for merges, Author, Date (`dd/mm/yyyy HH:MM`), bold subject, indented
  body. The preview reads as titled blocks (`sectionRule`): after the message
  comes `── N files changed  +A -D ───` (or `── no changes ───`) over the
  file list with per-file counts (capped at 50 files; paths are NEVER cut: one
  wider than the aligned column pushes its counts right, one wider than the
  line wraps),
  then `── diff ───` over delta's output (omitted for an empty diff). The
  totals title the file list instead of sitting in a `Stat:` header line, so
  the numbers are next to what they count. All of it is viewport content and
  scrolls with the diff. The top block renders instantly from list data;
  body and files need `git show --numstat` and fill in with the render
  (cached per hash).
- **File jumps** (`tab`/`shift+tab`, list and full view): `fileLines` finds
  delta's file headers in the rendered text (a non-blank line under a blank
  one and over a rule of `─`) or plain git's `diff --git`. It depends on
  delta's default file decoration.
- **Bounded render pool with prefetch**: previews render as a `tea.Cmd`,
  cached in memory per (commit, width, tool, effective mode). At most
  `maxPipelines` (3) pipelines are in flight (`m.inflight`), DYING ones
  included: a cancelled render holds its slot until it reports back, so
  holding an arrow key never piles up processes. Every `updatePreview`
  cancels the renders outside the window (`cancelStale`); the wanted render
  starts on a free slot, waits for a dying one to free it, or, with every slot
  live and in the window, takes the least wanted one's. Once the selection is
  served (a partial render counts), the window is rendered ahead in the free
  slots, in parallel: the row ahead, the row behind, then up to
  `prefetchAhead` (4) rows in the direction of travel (`m.dir`). It used to be
  strictly one render at a time with cursor±1 ahead; with hunk's ~0.6 s per
  commit that never kept up with someone stepping through the log. Failed
  renders are remembered (`failed`) and shown, not retried, or the prefetch
  would loop. Nothing renders before the first `WindowSizeMsg`.
- **Disk cache of the rendered diffs** (`cache.go`, `renderCache`): a commit
  never changes, so the diff a tool drew is stored gzipped under
  `${XDG_CACHE_HOME:-~/.cache}/asgitlog/renders/`, keyed by a hash of (format
  version, tool fingerprint, commit, width, effective mode, paths). Only the
  DIFF is stored: the header carries refs, which move. The fingerprint is
  taken once per run: the tool's binary (path, size, mtime) plus, for delta,
  `git config --list` and `DELTA_FEATURES`/`BAT_THEME`, and for hunk its
  `config.toml` (user and repo) and `state.json`. The working tree row and
  plain git output are never stored. A hit is touched; at startup the cache is
  pruned to 3/4 of 128 MiB, least recently used first. A warm popup shows a
  hunk render in ~40 ms instead of 0.2-2 s. `renderCache` is nil in tests (no
  cache); `ASGITLOG_NO_CACHE=1` turns it off; `pty-check.py` sandboxes
  `XDG_CACHE_HOME`.
- **Full view** (enter): same cached content on a full-screen viewport with
  pager keys, `[`/`]` (or `←`/`→`) to the newer/older commit without leaving,
  file jumps, `ctrl+t`, `y`/`o`, and `/` search (`n`/`N`, `esc` clears it
  first). The search highlights with `lipgloss.StyleRanges` per line; the
  viewport's own `SetHighlights` is NOT used because it miscounts lines on
  ANSI content.
- **Actions** are read-only: `ctrl+y` copies the full hash (`pbcopy`, or
  `ASGITLOG_CLIPBOARD`), `ctrl+o` opens the commit on the remote's web page
  (`webURL`/`commitURL`: GitHub, GitLab, Bitbucket shapes; Chrome front-window
  AppleScript like gotopr, or `ASGITLOG_OPENER`) and stays open. Results show
  as a 2-second flash in place of the help line.
- **Help** is bubbles' `help` component and nothing else: the bottom line is
  its short view, and `?` toggles `help.ShowAll`, which expands it IN PLACE
  into the full view (one column per `FullHelp()` group of the current
  context's key map). `footH` measures it and list, preview and full-view
  viewport give way (`resize` on toggle and on mode switches, since the two
  contexts differ in height); on a very short terminal the help is cut
  instead. `esc` folds it before doing anything else. In the list `?` only
  toggles while the query is empty; `f1` always works. A hand-made overlay
  with section titles and notes was tried and dropped; the filter's `~word`
  hint lives on as a help-only binding.
- **Settings** (`layout`, `diff`, `renderer`, `split-rows`, `split-columns`)
  are one plain-text file each under `${XDG_STATE_HOME:-~/.local/state}/asgitlog/`,
  not the herdr plugin state dir: the popup and the shell binary share them.
- **Plugin pane cwd**: herdr starts plugin panes in the plugin root and
  resolves the manifest's `./asgitlog` against the pane's cwd, so the pane
  must NOT be opened with `--cwd`. Instead `enterPaneCwd` (only when
  `HERDR_PLUGIN_ENTRYPOINT_ID` is set) chdirs to `focused_pane_cwd` from
  `HERDR_PLUGIN_CONTEXT_JSON`. `HERDR_ACTIVE_PANE_CWD` only exists for custom
  `[[keys.command]]` commands, not for plugin actions.
- **Errors**: outside a work tree a shell run prints to stderr and exits 1;
  a plugin pane shows the error inside the TUI (the popup closes with the
  process and would take stderr with it). `git log` failures (empty repo) and
  render failures are shown in place.
- **Never query the terminal behind bubbletea's back**; only the program owns
  stdin. Colors are ANSI 0-15, so they follow the terminal theme and no
  background detection is needed.
- **Getting back to the newest commit**: clearing a filter deliberately stays
  on the commit that was found, which can be thousands of rows down, so
  `home`/`end` jump to the newest/oldest commit (taken from the filter input's
  caret on purpose: `←`/`→` and `ctrl+e` still move it), `alt+↑`/`alt+↓` jump
  to the top/bottom of the list, which is the same thing (compact Mac keyboards
  have no home/end keys, only `fn+←/→`, which not every terminal passes on;
  this is the pair the help line advertises), and the wheel over the list
  walks the history.
- **Mouse**: the wheel is routed by pointer position: over the list
  (`overList`) it moves the selection one row per report; anywhere else it scrolls
  the diff. gotopr dropped this routing because trackpad inertia drifting
  across its two columns misrouted events; here the list needs a mouse way
  back up, and the stray event just moves the selection a row. A left click
  on a list row selects it and never opens it. Mouse mode and alt screen are
  declared per frame in `View()`.
- bubbles' `help` skips bindings without keys, so help-only entries carry the
  `helpOnly` key; it also overflows its width when the ellipsis does not fit,
  so `footLine` truncates.

## Testing

Unit tests cover parsing, decorations, numstat, URLs, log arguments,
filtering (substring, fuzzy, AND, non-ASCII, narrowing, byte offsets), row
layout (exact widths, column alignment, narrow fallback, special rows,
relative dates), settings, the header variants, file header detection, and
the model (geometry, both list directions, toggles and persistence, list
resizing, filter flow, render pool + prefetch + partial renders, the disk
cache, render errors, box edges and
file jumps, full view navigation + search + help, copy/browse through stubs,
scope input, error states). Integration tests build a throwaway repository
with a merge and run the real `git log`/`git show`/`delta` paths, log scopes
and the working tree row (delta tests skip when it is not installed). Model
tests never run `Init`, so no git process starts; pending renders are
answered by hand (`settle`).

`scripts/pty-check.py ./asgitlog` (python3 + `pyte`) is the end-to-end check:
it spawns the binary on a pty inside a generated repository with a sandboxed
`XDG_STATE_HOME` and stubs for clipboard and browser, answers the terminal
queries, replays keys, wheel bursts, clicks and resizes, and asserts on
pyte-rendered frames, including scopes, arguments, the working tree row, the
plugin pane environment and the outside-a-repo paths. Its `Screen` subclass
adds the SU/SD scroll sequences pyte lacks; without them frames go stale
after a viewport scroll.

## Commits & branches

- Conventional Commits: `type(scope): description`.
- Never mention AI tooling in commits, PRs, or any repo-visible text.
- Default branch is `main`. Don't commit, tag, or push unless explicitly
  asked (releasing is an explicit, separate request).

## Releasing

`scripts/release.sh <X.Y.Z>`: clean-tree + vet/build/test gate, CHANGELOG
generation from commit subjects, manifest version sync, commit + tag + GitHub
release; CI (`.github/workflows/release.yml`) attaches
`asgitlog-darwin-arm64`. Releasing never touches the linked plugin's
`./asgitlog`; rebuild locally to keep testing dev code.
