# CLAUDE.md

Guidance for working in this repository.

## What this is

`asgitlog` browses the git history of the current repository: a filterable
commit list with a preview of the selected commit (a native header with hash,
refs, author, date, stat, message and changed files, then the diff rendered by
[hunk](https://hunk.dev), the default, or
[delta](https://github.com/dandavison/delta)). Enter opens the diff full
screen. It started as the replacement of a zsh + fzf function and runs two
ways from the same binary: as a herdr plugin popup (on the repository of the
pane that was focused) and from a plain shell (on the current directory,
optionally scoped: `asgitlog [<revision range>...] [-- <path>...]`).

Distributed as a herdr plugin (`herdr plugin install asumaran/asgitlog`; the
manifest's `[[build]]` runs `scripts/fetch-binary.sh`). Each GitHub Release
attaches the `asgitlog-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64). There is no published library. Modeled on
`asgotopr` (siblings: `asgotoissues`, `asgoto`).

## Stack & layout

Go single module, single `package main`, static binary. TUI: Bubble Tea v2 +
bubbles v2 (`textinput`, `viewport`, `key`, `help`), lipgloss v2,
`sahilm/fuzzy` for the opt-in fuzzy terms. The charm v2 modules are imported
under their canonical `charm.land/<name>/v2` paths (the
`github.com/charmbracelet/<name>/v2` spelling is rejected by `go get`). Files
are split by concern but everything stays in `package main`:

- `main.go`: flags (`-version`, `-dump`, `-query`, `-show`, `-n`, `-width`),
  revision/path arguments, the work tree check (`fatal` outside one),
  `tea.NewProgram`, `runDump`.
- `git.go`: repo summary and remote web URL (`repoInfo`), log scope
  (`logOpts`), the streamed `git log` (`streamLog`, `parseCommit`,
  decorations, the working tree row), per-commit `detail` (body + numstat).
- `filter.go`: substring/fuzzy terms, hits in log order, narrowing.
- `match.go`: `findTight`/`tighten`, the fuzzy matcher with one correction: it is
  greedy (first candidate for each rune, left to right), so a query that
  occurs in one piece could still match scattered letters before it. When the
  query occurs whole, that occurrence is the match; here only the highlight
  changes, hits stay in log order. `hasTerms` says whether a query searches
  for anything: spaces and a bare `~` or `'` do not (asgitlog asks
  `queryTerms` the same thing through `filtering()`). The same file in every
  tool of the family.
- `text.go`: `truncate`, `padRight`, `padLeft`: fitting text, styled or not,
  into cells. The same file in every tool of the family.
- `setting.go`: `loadSetting`, `saveSetting`, a setting the tool remembers,
  one plain-text file each in the state dir; `prefs.go` reads and writes
  through it. The same file in every tool of the family that needs it.
- `listmouse.go`: `inList`, `rowUnder`, `wheelKey`: the mouse over the list.
  The wheel goes through the same code as the arrows; a click moves the
  cursor and never opens anything. The same file in every tool of the family.
- `prompt.go`: the filter input, its prompt (with the tool's name only outside
  herdr's popup, where the pane's title already says it), the placeholder and
  the `(dev)` mark on the edge over the input. `typeInto` hands a message to
  the input and reports whether the query changed: a key, a terminal paste
  and the input's own `ctrl+v` all edit it, and the caller filters again only
  when it did. The same file in every tool of the family. The `/` search and
  the `-S` inputs keep their own prompts.
- `helpfoot.go`: the help line at the foot, cut to the width, and the key that
  opens the panel. `footLine` is what the foot shows: a flash first, then a
  notice in the error color, else the help. The same file in every tool of the
  family.
- `panel.go`: the panel `f1` opens over the screen, options to change in
  place and every key under them (`option`, `panel`, `panelLines`,
  `overlay`). The same file in every tool of the family.
- `listnav.go`: `listNav`, the keys that move the cursor through the list and
  where each one takes it. `scrollTo` keeps the cursor in view (`clampCursor`
  goes through it); `withHeader` names the group header to keep in view with
  it, which a flat list like this one does not need. `emptyList` is what the
  list says instead of rows: the `git log` error, in the error color,
  `No matches` under a filter or a `-S` scope, `No commits` otherwise. The
  same file in every tool of the family, which took these keys from here.
- `highlight.go`: `highlightFrom`, `matchOver`, `onSel` and the
  `stSel`/`stMatch` styles, how a match and the selected row look;
  `renderSegs` renders every segment through it. The same file in every tool
  of the family, which took this look from here.
- `flash.go`: `flash`, `flashMsg`, `clearFlashMsg`: a confirmation that takes
  the help line for a moment. The same file in every tool of the family.
- `clipboard.go`: `copyCmd`: feeds a text to the system clipboard and reports
  it with a `flashMsg`; `ASGITLOG_CLIPBOARD` replaces the command. The same
  file in every tool of the family.
- `border.go`: `hline`, `framed`, `fit`, `scrollPos`: the primitives the frame
  is drawn with (an edge with texts set into it, a line between the frame's
  sides, the position a scrolled viewport reports on an edge). `fitLines` is
  content as exactly so many lines of a width, and `popupView` is the
  `tea.View` every tool returns: the alt screen and, while the mouse is on,
  cell-motion mouse reports. The same file in every tool of the family.
- `homepath.go`: `tildePath`, `homeDir`, `homeRel`: a path with the home
  directory abbreviated to `~`. The same file in every tool of the family that
  shows paths.
- `statedir.go`: `stateDirFor`: the state dir herdr injects
  (`HERDR_PLUGIN_STATE_DIR`) or, when the tool runs on its own, the same
  directory worked out
  (`${XDG_STATE_HOME:-~/.local/state}/herdr/plugins/asumaran.asgitlog`), so
  the popup and a run from the shell share settings and caches. The same file
  in every tool of the family.
- `fatal.go`: `fatal(tool, msg)`: an error that keeps the tool from starting.
  In herdr's popup the message is held until enter, because the pane closes
  with the process and takes stderr with it; in a shell it is plain stderr and
  exit 1. The same file in every tool of the family that needs it.
- `panecwd.go`: `paneDirs`, `paneCwd`, `enterPaneCwd`: which directory a popup
  was opened from. herdr starts a plugin pane in the plugin's own directory
  and hands over `focused_pane_cwd` and `workspace_cwd` in
  `HERDR_PLUGIN_CONTEXT_JSON`; a plain run uses the working directory. The
  same file in every tool of the family that needs it.
- `gitrun.go`: `runGit`: git in the current directory, with git's own stderr
  as the error, and `insideWorkTree`. The same file in every tool of the
  family that needs it.
- `openurl.go`: `openURL`: hands a URL to the browser. On macOS a Chrome that
  is already up gets a new tab in its front window, else `open`; `xdg-open`
  elsewhere; `ASGITLOG_OPENER` replaces all of it. The same file in every tool
  of the family that opens one.
- `diffmode.go`: how a diff is laid out and fetched: the modes `ctrl+t` walks
  (`effectiveDiff`, `diffLabel`), what the main section's bottom edge says
  about the diff (`diffEdge`), and `limitedOutput`, which runs the command
  that prints it without letting a huge one in (`maxDiffBytes` is each tool's
  own). The same file in every tool of the family that shows diffs.
- `list.go`: row segments, the wide and compact formats, relative dates,
  match highlighting.
- `preview.go`: native header with the file list, `git show` handed to the
  renderer as a `tea.Cmd`, file header detection, output cap.
- `hunk.go`: hunk as the diff renderer, captured off a pty.
  The same `hunk.go` and `hunk_test.go` ship in github.com/asumaran/asgotochanged
  (copied, not imported: there is no shared library). A pull request only
  needs to change them here; the maintainer ports the change. Tests of what
  asgitlog does with the render live in `preview_test.go`.
- `prefs.go`: the persisted settings (layout, diff mode, renderer,
  whitespace, refs and the two split sizes), read and written through
  `setting.go`, and `migratePrefs`.
- `rendercache.go`: the rendered diffs kept on disk between runs, addressed by
  an id that cannot go stale (a commit's hash, or `patchID`, a hash of the
  patch itself) plus whatever else changes the output: the renderer, its
  binary and configuration, the width, the mode. The same file asgotochanged
  ships.
- `difftool.go`: what draws a diff: hunk or delta, or git's own colors when
  neither is installed (`diffTool`, `toolBin`, `pickTool`, `renderPatch`).
  `diffPrefs` is the three diff options of the panel (renderer, diff mode,
  whitespace), what each value means and what is flashed about a change. The
  same file asgotochanged ships.
- `ui.go`: the bubbletea model/Update/View, modes, geometry, pooled
  preview rendering with prefetch, full view and its search, actions, mouse,
  styles.
- `scripts/pty-check.py`: end-to-end driver (see Testing).
- `scripts/demo/`: the demo scenario (`scenario.sh` + `keys.json`) that
  `asdemo record` (asumaran/asdemokit, the recording tool shared by the herdr
  plugins) uses to re-record `docs/demo.gif`; see `scripts/demo/README.md`.

## Build & run

```bash
go build -o asgitlog .          # plugin runs ./asgitlog from the repo root
./asgitlog                      # TUI on the repository of the current directory
./asgitlog main..dev -- src/    # scoped to a range and/or paths
./asgitlog -dump                # repo summary, settings, first rows; no TTY
./asgitlog -dump -query x -n 0  # every row matching a query
./asgitlog -dump -show HEAD~2 -width 140   # one commit's preview (header + diff)
go vet ./... && go test ./...
scripts/pty-check.py ./asgitlog
herdr plugin link "$PWD"   # link does NOT run [[build]]; go build yourself
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
  shown on the edge over the input (the summary line is mostly path in the narrow
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
  four sections (`listView`, built with `hline`/`framed`/`fit` from the shared `border.go`, all lines
  exactly the terminal width) that share their edges (`├─┤`), so no line goes
  to a border of their own (four separate boxes were tried first; the doubled
  borders read as gaps and cost three lines): the repo summary; the filter
  input, whose top edge carries the log's `[scope]` and the loading mark;
  the main section, holding the list AND the commit details split by a divider
  (`mainLines`); and the help line. The edge over the details (the divider in rows, the top
  edge in columns) says nothing about the diff: it used to carry the diff mode
  and the scrolled-away commit, which was dropped as noise. The edge under the
  list (the divider in rows, the left part of the bottom edge in columns)
  carries the matches/total counter (`counter`). The main section's bottom edge carries
  the details' `line/total`. Details have a cell of padding, list rows use
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
  A paste from the terminal (`tea.PasteMsg`) and the input's own `ctrl+v`
  filter like a key does (`typeInto`); a paste under the panel or the full
  view is dropped. A query made only of spaces, or a bare `~` or `'`, is not
  a query (`filtering()`): it does not filter or move the selection.
- **Diff rendering is delegated to hunk or delta**, never reimplemented (the
  shared `difftool.go`; hunk has a bullet of its own below). With delta:
  `git show -m --first-parent --format= <hash> | delta --width=N
  --paging=never [--side-by-side]`, ANSI output straight into a `viewport`.
  delta ignores `COLUMNS` and assumes 80 columns when stdout is not a tty, so
  `--width` is always explicit (the viewport width: the frame costs 4 columns).
  `-m --first-parent` makes a merge show what it brought in; git show's
  combined diff is empty for a clean merge. Diff mode: `auto` (side by side
  from 120 columns of preview), `sbs`,
  `single`; `ctrl+t` cycles and flashes the new mode in the help line (the
  list has no standing label for it; the full view's title does). The cache
  key uses the EFFECTIVE mode, so auto shares renders with the explicit modes.
  With neither renderer in `PATH` the diff falls back to `git show --color=always`
  (the `ctrl+t` flash says `no renderer found: plain git colors`). Output is
  capped at 16 MiB.
- **Whitespace** (`ctrl+s` in the list and in the full view, setting
  `whitespace`): git's `-w` (`--ignore-all-space`, what GitHub's "Hide
  whitespace" does) on the `git show` / `git diff HEAD` that makes the patch,
  so it works the same with delta, hunk and plain git. It travels in
  `diffTool.ignoreWS`, which puts it in the render key and in the disk cache's
  address. A commit with nothing left says `(only whitespace changes)`; the
  file list and the counts of the header still come from the full numstat.
  While it is on, `[-w]` stands on the main section's bottom edge before the
  scroll position (`diffEdge`) and next to the diff mode in the full view's
  title.
- **hunk or delta as the renderer** (the panel's first option, setting
  `renderer`; hunk is the family's default, and `pickTool` falls back to delta
  while `hunk` is not in `PATH`, whatever was saved). The option, the diff mode
  and the whitespace are the shared `diffPrefs` (`difftool.go`): `setOption`
  hands the change to `diffPrefs.set`, which says what to flash. The panel
  switches to either renderer that is installed, delta included while hunk is
  missing; one that is not installed flashes `<name> not found` and changes
  nothing. Only the option that changed is saved, so a setting never chosen
  stays unset. Only hunk's looks are wanted. hunk has NO static output (`hunk pager`
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
  then `── diff ───` over the renderer's output (omitted for an empty diff). The
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
- **Disk cache of the rendered diffs** (`rendercache.go`, `renderCache`; the id is `renderID`: the hash and the paths): a commit
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
- **Actions** are read-only: `ctrl+y` copies the full hash (`pbcopy` on macOS, the first of `wl-copy`/`xclip`/`xsel` on Linux, or
  `ASGITLOG_CLIPBOARD`), `ctrl+o` opens the commit on the remote's web page
  (`webURL`/`commitURL`: GitHub, GitLab, Bitbucket shapes; Chrome front-window
  AppleScript like asgotopr, or `ASGITLOG_OPENER`) and stays open. Both go
  through files shared with the family: `clipboard.go`, `openurl.go`, and
  `flash.go` for the confirmation. Results show
  as a 2-second flash in place of the help line.
- **Help and options**: the bottom line is the short view of bubbles' `help`
  (`helpfoot.go`). `f1` opens
  the panel (`panel.go`, the same file in every tool of the family): the
  options on top, to change with `←`/`→` or `space`, and under them every key
  of the current context's key map, laid out by bubbles' `help` from
  `FullHelp()`. The panel is spliced over the middle of the screen, which
  keeps its size, so nothing is resized when it opens; while it is open it
  takes every key and the mouse, and `esc` closes it before it does anything
  else. In the list `?` is text for the filter; the full view has no input, so
  `?` opens the panel there too. `options()` lists the settings of the
  current view (the full view has no layout and no log scope) and `setOption`
  is the one place that changes one, for the panel and for the keys that kept
  a shortcut (`ctrl+t`, `ctrl+s`, `ctrl+a`). The renderer and the layout are
  chosen once, so they have no key: the panel is where they live. An earlier
  overlay that only restyled the key list was tried and dropped; this one
  earns its place with the options. The filter's `~word` hint lives on as a
  help-only binding.
- **Settings** (`layout`, `diff`, `renderer`, `whitespace`, `refs`,
  `split-rows`, `split-columns`)
  are one plain-text file each (`setting.go`, shared with the family) in the
  plugin's state dir (`stateDirFor`, the shared `statedir.go`): the one herdr
  injects, and the same directory worked out when the binary runs from a
  shell, so the popup and the shell share them. `migratePrefs` copies the
  ones from the old `${XDG_STATE_HOME:-~/.local/state}/asgitlog/` once.
- **Plugin pane cwd**: herdr starts plugin panes in the plugin root and
  resolves the manifest's `./asgitlog` against the pane's cwd, so the pane
  must NOT be opened with `--cwd`. Instead `enterPaneCwd` (only when
  `HERDR_PLUGIN_ENTRYPOINT_ID` is set) chdirs to `focused_pane_cwd` from
  `HERDR_PLUGIN_CONTEXT_JSON`. `HERDR_ACTIVE_PANE_CWD` only exists for custom
  `[[keys.command]]` commands, not for plugin actions.
- **Errors**: outside a work tree the tool does not start: the shared `fatal`
  (`fatal.go`) prints to stderr and exits 1, and in a plugin pane it holds the
  message until enter first (the popup closes with the process and would take
  stderr with it). There is no error mode inside the TUI. A `git log` failure
  (empty repo) is shown in the list in the error color (`logErr` through the
  shared `emptyList`), as in every tool of the family; render failures are
  shown in the preview, in the same color.
- **Never query the terminal behind bubbletea's back**; only the program owns
  stdin. Colors are ANSI 0-15, so they follow the terminal theme and no
  background detection is needed.
- **Getting back to the newest commit**: clearing a filter deliberately stays
  on the commit that was found, which can be thousands of rows down, so
  `home`/`end` jump to the newest/oldest commit (taken from the filter input's
  caret on purpose: `←`/`→` and `ctrl+e` still move it), `alt+↑`/`alt+↓` jump
  to the top/bottom of the list, which is the same thing (compact Mac keyboards
  have no home/end keys, only `fn+←/→`, which not every terminal passes on;
  this is the pair the panel lists), and the wheel over the list
  walks the history.
- **Alt screen and mouse mode** are declared per frame in `View()`; there is
  no `tea.WithAltScreen` program option in v2. The mouse is off while the `/`
  search or the `-S` prompt has the keys.
- **Mouse**: the wheel is routed by pointer position: over the list
  (`overList`) it moves the selection one row per report; anywhere else it scrolls
  the diff, as in every tool of the family. Trackpad inertia can drift an
  event onto the other section; the list needs a mouse way back up, and the
  stray event just moves the selection a row. A left click on a list row
  selects it and never opens it.
- bubbles' `help` skips bindings without keys, so help-only entries carry the
  `helpOnly` key; it also overflows its width when the ellipsis does not fit,
  so the shared `helpLine` (`helpfoot.go`) truncates.

## Testing

Unit tests cover parsing, decorations, numstat, URLs, log arguments,
filtering (substring, fuzzy, AND, non-ASCII, narrowing, byte offsets), row
layout (exact widths, column alignment, narrow fallback, special rows,
relative dates), settings, the header variants, file header detection, and
the model (geometry, toggles and persistence, list
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

- Conventional Commits: `type(scope): description` (feat, fix, chore, docs,
  style, refactor, test, perf).
- Never mention AI tooling in commits, PRs, or any repo-visible text as the
  author of changes.
- Default branch is `main`. Don't commit, tag, or push unless explicitly
  asked (releasing is an explicit, separate request).

## Releasing

`scripts/release.sh <X.Y.Z>`: clean-tree + vet/build/test gate, CHANGELOG
generation from commit subjects, manifest version sync, commit + tag + GitHub
release; CI (`.github/workflows/release.yml`) attaches
the `asgitlog-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64). Releasing never touches the linked plugin's
`./asgitlog`; rebuild locally to keep testing dev code.
