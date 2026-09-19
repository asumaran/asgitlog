# shellcheck shell=bash
# scenario.sh — demo session for the README GIF, run by `herdr-demo record`
# (asumaran/herdr-demokit). Sourced by the kit; the helpers used below
# (demo_*) come from it.

DEMO_SESSION="asgitlogdemo"
DEMO_OUT="docs/demo.gif"
# asgitlog browses the repository of the focused pane: the demo starts on a
# personal project with a history worth browsing.
DEMO_START_CWD="$HOME/Developer/shopnest"

# Same sidebar as the other demos: personal repos only.
REPOS=(
  "$HOME/Developer/worktree-cli"
  "$HOME/Developer/asreviewer"
  "$HOME/Developer/aspage"
)
SPLITS=(
  "$HOME/Developer/shopnest"
)

demo_build() {
  local version
  version="$(sed -n 's/^version = "\(.*\)"/\1/p' herdr-plugin.toml)"
  go build -ldflags "-X main.version=v${version}" -o asgitlog .
}

demo_teardown() {
  go build -o asgitlog . 2>/dev/null || true
}

demo_setup() {
  local repo target
  demo_adopt_repo "$(demo_first_workspace)" "$DEMO_START_CWD"
  for repo in "${REPOS[@]}"; do demo_open_repo "$repo" >/dev/null; done
  for target in "${SPLITS[@]}"; do demo_split_below "$target" >/dev/null; done
}
