package main

// Persisted settings: the layout (rows|columns) and the diff mode
// (sbs|single), one plain-text file per setting under
// ${XDG_STATE_HOME:-~/.local/state}/asgitlog. The location is deliberately not
// the herdr plugin state dir: the same binary runs as a popup and from a plain
// shell, and both share the settings.

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	layoutRows    = "rows"    // preview at the bottom, wide list
	layoutColumns = "columns" // preview on the right, compact list
	diffSBS       = "sbs"
	diffSingle    = "single"
)

type prefs struct {
	layout string
	diff   string
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

func loadPrefs() prefs {
	p := prefs{layout: layoutRows, diff: diffSBS}
	if readPref("layout") == layoutColumns {
		p.layout = layoutColumns
	}
	if readPref("diff") == diffSingle {
		p.diff = diffSingle
	}
	return p
}

func diffLabel(mode string) string {
	if mode == diffSingle {
		return "single column"
	}
	return "side-by-side"
}
