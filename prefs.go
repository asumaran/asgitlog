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
	diffAuto      = "auto"    // side by side when the preview is wide enough
	diffSBS       = "sbs"
	diffSingle    = "single"

	// autoSBSMinW is the preview width from which the auto mode goes side by
	// side (the threshold the dotfiles' delta-pager wrapper uses).
	autoSBSMinW = 120

	splitMin, splitMax, splitStep = 30, 85, 5
)

type prefs struct {
	layout       string
	diff         string
	splitRows    int // preview height, percent of the body
	splitColumns int // preview width, percent of the screen
}

func prefsDir() string {
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
	p := prefs{
		layout:       layoutRows,
		diff:         diffAuto,
		splitRows:    readSplit("split-rows", 70),
		splitColumns: readSplit("split-columns", 75), // list 25%, details 75%
	}
	if readPref("layout") == layoutColumns {
		p.layout = layoutColumns
	}
	if d := readPref("diff"); d == diffSBS || d == diffSingle {
		p.diff = d
	}
	return p
}

// effectiveDiff resolves the auto mode for a preview of the given width.
func effectiveDiff(mode string, width int) string {
	if mode != diffAuto {
		return mode
	}
	if width >= autoSBSMinW {
		return diffSBS
	}
	return diffSingle
}

func diffLabel(mode string, width int) string {
	label := "side-by-side"
	if effectiveDiff(mode, width) == diffSingle {
		label = "single column"
	}
	if mode == diffAuto {
		return "auto: " + label
	}
	return label
}
