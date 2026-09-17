# CLAUDE.md

Guidance for working in this repository.

## What this is

`asgitlog` browses the git history of the current repository: a fuzzy
filterable commit list with a preview of the selected commit (a native header
with hash, refs, author, date, stat and message, then the diff rendered by
[delta](https://github.com/dandavison/delta)). Enter opens the diff full
screen. It replaces the `fl` zsh/fzf function from the dotfiles and runs two
ways from the same binary: as a herdr plugin popup (on the repository of the
pane that was focused) and from a plain shell (on the current directory).

Distributed as a herdr plugin (`herdr plugin install asumaran/asgitlog`; the
manifest's `[[build]]` runs `scripts/fetch-binary.sh`). Each GitHub Release
attaches `asgitlog-darwin-arm64`. There is no published library. Modeled on
`gotopr` (siblings: `gotojira`, `herdr-goto`).

## Stack & layout

Go single module, single `package main`, static binary. TUI: Bubble Tea v2 +
bubbles v2 (`textinput`, `viewport`, `key`, `help`), lipgloss v2,
`sahilm/fuzzy` for matching. The charm v2 modules are imported under their
canonical `charm.land/<name>/v2` paths (the
`github.com/charmbracelet/<name>/v2` spelling is rejected by `go get`). Files
are split by concern but everything stays in `package main`:

- `main.go`: flags (`-version`, `-dump`, `-query`, `-show`, `-n`, `-width`),
  `enterPaneCwd` (plugin pane → focused pane's directory), work tree check,
  `tea.NewProgram`, `runDump`.
- `git.go`: `runGit`, repo summary (`repoInfo`), the streamed `git log`
  (`streamLog`, `parseCommit`, decorations), per-commit `detail` (body +
  shortstat).
- `filter.go`: fuzzy hits in log order, ANDed terms, narrowing.
- `list.go`: row segments, the wide and compact formats, match highlighting.
- `preview.go`: native header, `git show | delta` as a `tea.Cmd`, output cap.
- `prefs.go`: persisted layout and diff mode.
- `ui.go`: the bubbletea model/Update/View, modes, geometry, single-flight
  preview rendering, full view and its search, mouse, styles.
- `scripts/pty-check.py`: end-to-end driver (see Testing).

## Build & run

```bash
go build -o asgitlog .          # plugin runs ./asgitlog from the repo root
./asgitlog                      # TUI on the repository of the current directory
./asgitlog -dump                # repo summary, settings, first rows; no TTY
./asgitlog -dump -query x -n 0  # every row matching a query
./asgitlog -dump -show HEAD~2 -width 140   # one commit's preview (header + delta)
go vet ./... && go test ./...
scripts/pty-check.py ./asgitlog
herdr plugin link ~/Developer/asgitlog   # link does NOT run [[build]]; go build yourself
```

Keybinding (user config): `plugin_action` `asumaran.asgitlog.open` →
`scripts/open-pane.sh` → `herdr plugin pane open` (popup, 85% x 80%).

## Behaviour / decisions

- **Data**: one `git log -z` with unit-separated fields
  (`%H %h %an %ae %ad %D %s`, `--decorate=full` so refs can be classified,
  `origin/HEAD` excluded), streamed in batches (200 commits first, then 4000)
  so large histories show up immediately. Each commit is ONE string, the
  search corpus (`short author <email> subject refs date`); every displayed
  field is a slice of it. That keeps a big history at one allocation per
  commit and gives each field's byte offset in the corpus, which is what the
  fuzzy matcher reports, so matches are highlighted in place.
  `sahilm/fuzzy` reports BYTE offsets, not rune indexes.
- **Layout is computed at render time** from structs, so a resize or a layout
  change never re-runs git and the cursor trivially stays on the same commit.
  Only the visible window of rows is rendered. Rows layout: wide list on top
  (30%), preview below (70%). Columns layout: compact list left (25%), preview
  right (75%); it falls back to rows under 60 columns without touching the
  saved setting. The wide format drops the refs column, then the author, on
  narrow terminals.
- **Filter**: whitespace-separated terms are ANDed, each a fuzzy subsequence
  match; hits keep log order (fzf `--no-sort`). Extending the query narrows
  the previous hits instead of rescanning. The selection stays on the same
  commit while it still matches, else the first hit; so deleting the query
  ends on the commit that was found. Commits streamed in while a query is
  active are filtered as they arrive.
- **Diff rendering is delegated to delta**, never reimplemented:
  `git show --format= <hash> | delta --width=N --paging=never
  [--side-by-side]`, ANSI output straight into a `viewport`. delta ignores
  `COLUMNS` and assumes 80 columns when stdout is not a tty, so `--width` is
  always explicit. The mode is explicit too (`delta` is called directly, not
  the dotfiles' `delta-pager` wrapper). Without delta in `PATH` the diff
  falls back to `git show --color=always` and the preview label says so.
  Output is capped at 16 MiB.
- **Preview header is native** (lipgloss): `commit <hash> (refs)`, Author,
  Date (`dd/mm/yyyy HH:MM`), Stat (`N files, +A -D` / `no changes`), bold
  subject, indented body. It is part of the viewport content and scrolls with
  the diff, like the `git show` output `fl` previewed. It renders instantly
  from list data; stat and body need a `git show --shortstat` and fill in with
  the render (cached per hash, so mode toggles keep them).
- **Single-flight renders**: previews render as a `tea.Cmd`, cached per
  (commit, width, diff mode). At most one git|delta pipeline runs; moving on
  cancels it (context) and the newest wanted render starts when the cancelled
  one reports back, so holding an arrow key never piles up processes. Nothing
  renders before the first `WindowSizeMsg`.
- **Full view** (enter): same cached content on a full-screen viewport with
  pager keys, `ctrl+t`, and `/` search (`n`/`N`, `esc` clears it first). The
  search highlights with `lipgloss.StyleRanges` per line; the viewport's own
  `SetHighlights` is NOT used because it miscounts lines on ANSI content.
- **Settings** (`layout`, `diff`) are one plain-text file each under
  `${XDG_STATE_HOME:-~/.local/state}/asgitlog/`, not the herdr plugin state
  dir: the popup and the shell binary share them.
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
- **Mouse**: the wheel always scrolls the diff, wherever the pointer is (see
  gotopr for why routing by pointer position was dropped); a left click on a
  list row selects it and never opens it. Mouse mode and alt screen are
  declared per frame in `View()`.
- bubbles' `help` skips bindings without keys, so help-only entries carry a
  dummy key; it also overflows its width when the ellipsis does not fit, so
  `helpLine` truncates.

## Testing

Unit tests cover parsing, decorations, shortstat, filtering (order, AND,
narrowing, byte offsets), row layout (exact widths, column alignment, narrow
fallbacks), settings, the header, and the model (geometry, layout/diff toggles
and persistence, filter flow, single-flight previews, full view + search,
click, error states). Integration tests build a throwaway repository and run
the real `git log`/`git show`/`delta` paths (delta tests skip when it is not
installed).

`scripts/pty-check.py ./asgitlog` (python3 + `pyte`) is the end-to-end check:
it spawns the binary on a pty inside a generated repository with a sandboxed
`XDG_STATE_HOME`, answers the terminal queries, replays keys, wheel bursts,
clicks and resizes, and asserts on pyte-rendered frames, including the plugin
pane environment and the outside-a-repo paths.

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
