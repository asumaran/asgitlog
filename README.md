# asgitlog

Browse the git history of a repository from the terminal: a fuzzy filterable
commit list with a preview of the selected commit. The diff in the preview is
rendered by [delta](https://github.com/dandavison/delta). It runs as a
[herdr](https://github.com/asumaran/herdr) plugin popup, on the repository of
the pane you were in, and as a plain command in any shell.

- The list shows hash, author, subject, refs and date; on a narrow list it
  switches to hash, date and subject.
- The preview has the commit header (hash, refs, author, date, files changed
  with `+added -removed`, full message) followed by the diff, side by side or
  in a single column.
- `enter` opens the diff full screen, with pager keys and text search.
- The layout (preview below or to the right) and the diff mode are remembered
  across runs.

## Install

```
herdr plugin install asumaran/asgitlog
```

The manifest's `[[build]]` runs `scripts/fetch-binary.sh`, which downloads the
release binary matching the manifest version and falls back to `go build`
(`ASGITLOG_BUILD_FROM_SOURCE=1` skips the download). Requires herdr >= 0.7.5
and `git`. Install `delta` too: without it the diff falls back to git's own
colors. macOS arm64 binaries only; other platforms build from
source.

Bind a key to the `open` action in `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = ["ctrl+alt+l"]
type = "plugin_action"
command = "asumaran.asgitlog.open"
description = "asgitlog (git log browser)"
```

To use it outside herdr, put the binary on your `PATH` (a symlink to the
plugin's `asgitlog` works) and run `asgitlog` inside any repository.

## Usage

The filter input is focused on open, so just type. The filter is fuzzy over
everything in the row (hash, author, email, subject, refs, date), terms
separated by spaces must all match, and commits always stay in log order.

| Key | Action |
| --- | --- |
| `↑`/`↓`, `ctrl+p`/`ctrl+n` | move the selection |
| `pgup`/`pgdn` | move a page |
| `shift+↑`/`shift+↓`, mouse wheel | scroll the preview |
| left click | select a commit |
| `enter` | open the diff full screen |
| `ctrl+t` | side-by-side / single column diff |
| `ctrl+l` | preview below (rows) / to the right (columns) |
| `esc`, `ctrl+c` | quit |

In the full-screen diff: `↑`/`↓`/`j`/`k` scroll, `space`/`b` (or
`pgdn`/`pgup`) page, `d`/`u` half page, `g`/`G` top/bottom, `/` searches
(`n`/`N` next/previous match, `esc` clears), `ctrl+t` switches the diff mode,
`q` or `esc` goes back to the list.

## Behavior notes

- As a herdr popup it browses the repository of the pane that was focused
  when it opened; from a shell, the one of the current directory. Outside a
  repository it says so and exits.
- The history is streamed, so a large repository is usable while the rest
  loads. The counter on the input line shows `matches/total`.
- The selection survives layout changes, resizes and filter edits: deleting
  the query leaves you on the commit you found, with its neighbors around.
- delta is called with an explicit `--width`, and with `--side-by-side` or
  not according to the mode; everything else (theme, line numbers, ...) comes
  from your own delta configuration.
- Settings live in `${XDG_STATE_HOME:-~/.local/state}/asgitlog/` (`layout`:
  `rows`|`columns`, `diff`: `sbs`|`single`), shared by the popup and the
  shell command.
- `ASGITLOG_POPUP_WIDTH` / `ASGITLOG_POPUP_HEIGHT` (e.g. `95%`) override the
  popup size from the manifest (85% x 80%).

## Development

```bash
go build -o asgitlog .     # local build (plugin runs ./asgitlog from the repo root)
./asgitlog -dump           # repo summary, settings and the first rows (no TTY)
./asgitlog -dump -query fix -n 0          # rows matching a query
./asgitlog -dump -show HEAD -width 140    # a commit's preview, header + delta
go vet ./... && go test ./...
scripts/pty-check.py ./asgitlog           # end-to-end on a pty (python3 + pyte)
herdr plugin link ~/Developer/asgitlog    # register the working copy (no build step)
```

## Releasing

`scripts/release.sh <X.Y.Z>` gates on a clean tree + green vet/build/test,
generates the CHANGELOG entry from commit subjects, syncs the manifest
version, commits, tags and publishes the GitHub release; CI then attaches
`asgitlog-darwin-arm64`, the asset `fetch-binary.sh` downloads on installs.
