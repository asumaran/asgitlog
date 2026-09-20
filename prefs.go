package main

// Persisted settings, one plain-text file per setting under
// ${XDG_STATE_HOME:-~/.local/state}/asgitlog: the layout (rows|columns), the
// diff mode (auto|sbs|single) and the preview's share of the screen in each
// layout. The location is deliberately not the herdr plugin state dir: the
// same binary runs as a popup and from a plain shell, and both share them.

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

func readPref(name string) string {
	data, err := os.ReadFile(filepath.Join(prefsDir(), name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// savePref is best effort: a read-only state dir only costs the persistence.
func savePref(name, value string) {
	dir := prefsDir()
	if dir == "" || os.MkdirAll(dir, 0o755) != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0o644)
}

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
		tool:         toolDelta,
		splitRows:    readSplit("split-rows", 70),
		splitColumns: readSplit("split-columns", 75), // list 25%, details 75%
	}
	if readPref("layout") == layoutColumns {
		p.layout = layoutColumns
	}
	if d := readPref("diff"); d == diffSBS || d == diffSingle {
		p.diff = d
	}
	if readPref("renderer") == toolHunk {
		p.tool = toolHunk
	}
	p.ignoreWS = readPref("whitespace") == "ignore"
	return p
}
