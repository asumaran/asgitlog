package main

// Persisted settings, one plain-text file per setting (setting.go) in the
// family's state directory (statedir.go): the layout (rows|columns), the diff
// mode (auto|sbs|single), the renderer (hunk|delta), the whitespace
// (show|ignore), the log's scope (current|all) and the preview's share of the
// screen in each layout. The popup and a plain shell run resolve the same
// directory, so both share them. migratePrefs copies the settings of the old
// location, ~/.local/state/asgitlog, once.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	layoutRows    = "rows"    // preview at the bottom, wide list
	layoutColumns = "columns" // preview on the right, compact list

	splitMin, splitMax, splitStep = 30, 85, 5
)

type prefs struct {
	layout       string
	diff         string
	tool         string
	splitRows    int  // preview height, percent of the body
	splitColumns int  // preview width, percent of the screen
	ignoreWS     bool // git's -w: changes in whitespace are left out of the diffs
	allRefs      bool // the log of every ref (--all), not only the current branch's
}

// prefsDir is where the settings live: the state directory every tool of the
// family uses (see statedir.go).
func prefsDir() string { return stateDirFor("asgitlog") }

// legacyPrefsDir is where they lived before asgitlog shared that directory.
func legacyPrefsDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(h, ".local", "state")
	}
	return filepath.Join(base, "asgitlog")
}

// migratePrefs copies the settings of the old location, once: only while the
// new directory holds none. Best effort, like savePref.
func migratePrefs() {
	from, to := legacyPrefsDir(), prefsDir()
	if from == "" || to == "" || from == to {
		return
	}
	old, err := os.ReadDir(from)
	if err != nil {
		return
	}
	if cur, err := os.ReadDir(to); err == nil && len(cur) > 0 {
		return
	}
	for _, e := range old {
		if !e.Type().IsRegular() {
			continue
		}
		if data, err := os.ReadFile(filepath.Join(from, e.Name())); err == nil {
			savePref(e.Name(), strings.TrimSpace(string(data)))
		}
	}
}

func readPref(name string) string { return loadSetting(prefsDir(), name) }

func savePref(name, value string) { saveSetting(prefsDir(), name, value) }

func readSplit(name string, def int) int {
	n, err := strconv.Atoi(readPref(name))
	if err != nil || n < splitMin || n > splitMax {
		return def
	}
	return n
}

func loadPrefs() prefs {
	migratePrefs()
	p := prefs{
		layout:       layoutRows,
		diff:         diffAuto,
		tool:         toolHunk, // the family's default; delta when hunk is missing (pickTool)
		splitRows:    readSplit("split-rows", 70),
		splitColumns: readSplit("split-columns", 75), // list 25%, details 75%
	}
	if readPref("layout") == layoutColumns {
		p.layout = layoutColumns
	}
	if d := readPref("diff"); d == diffSBS || d == diffSingle {
		p.diff = d
	}
	if readPref("renderer") == toolDelta {
		p.tool = toolDelta
	}
	p.ignoreWS = readPref("whitespace") == "ignore"
	p.allRefs = readPref("refs") == "all"
	return p
}
