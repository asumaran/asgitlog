# Demo recording

Scenario for re-recording the README demo GIF (`docs/demo.gif`) with
[herdr-demokit](https://github.com/asumaran/herdr-demokit):

```bash
herdr-demo record            # from the repo root; writes docs/demo.gif
herdr-demo doctor            # check the toolchain first
```

- `scenario.sh` — the isolated herdr session (`asgitlogdemo`): it starts on
  `~/Developer/shopnest`, a personal project, because asgitlog browses the
  repository of the focused pane; the sidebar holds personal repos only.
  `demo_build` stamps `./asgitlog` with the manifest version; `demo_teardown`
  restores the dev build. The renderer, layout and split are the user's saved
  preferences (`~/.local/state/asgitlog`), not set by the scenario.
- `keys.json` — `ctrl+alt+l` (the plugin's chord; it has no prefix variant, so
  the kit's `ctrl+alt+<letter>` key is used) -> popup -> down, down -> type
  `cart` -> scroll the diff -> enter (full diff) -> `q` (back) -> esc.

Besides the kit's toolchain, this needs the `asumaran.asgitlog` plugin
registered with the `ctrl+alt+l` `plugin_action` keybind and
`~/Developer/shopnest` to exist.
