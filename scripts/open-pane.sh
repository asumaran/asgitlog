#!/bin/sh
# Entry point for the "open" plugin action: opens the log pane (placement and
# size come from the [[panes]] entry in herdr-plugin.toml). ASGITLOG_POPUP_WIDTH
# / ASGITLOG_POPUP_HEIGHT (e.g. "60%") override the manifest size. Plugin
# commands are argv arrays with no shell expansion, so this wrapper exists to
# resolve HERDR_BIN_PATH and the overrides at runtime.
#
# The pane is deliberately NOT opened with --cwd: herdr resolves the manifest's
# "./asgitlog" against the pane's working directory, so it has to stay the
# plugin root. The binary moves to the focused pane's directory itself, from
# the HERDR_PLUGIN_CONTEXT_JSON herdr hands to the pane (see enterPaneCwd).
set -eu
exec "${HERDR_BIN_PATH:-herdr}" plugin pane open --plugin asumaran.asgitlog --entrypoint log \
  ${ASGITLOG_POPUP_WIDTH:+--width "$ASGITLOG_POPUP_WIDTH"} \
  ${ASGITLOG_POPUP_HEIGHT:+--height "$ASGITLOG_POPUP_HEIGHT"}
