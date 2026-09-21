package main

// The bubbletea model. Browsing (modeList): the repo summary, the commit list
// with the filter input and the preview box (stacked in the rows layout, side
// by side in the columns layout), then the key help. Enter opens the same
// preview full screen (modeFull) with pager keys, commit-to-commit navigation
// and a text search (modeSearch is its input line). modePickaxe is the input
// of the content search that rescopes the log. `?` expands the help line
// into bubbles' full help in place.

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ---- styles ----

var (
	stCursor = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stTitle  = lipgloss.NewStyle().Bold(true)
	stError  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	stLabel  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	stInfo   = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	stScope  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	stCount  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	stFound  = lipgloss.NewStyle().Reverse(true)

	// list columns and preview header, after git's own palette
	stHash      = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	stAuthor    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	stDate      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stKey       = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	stAdded     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stDeleted   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	stMerge     = lipgloss.NewStyle().Faint(true)
	stWorkTree  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Italic(true)
	stRefHead   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	stRefBranch = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	stRefRemote = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	stRefTag    = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	stRefOther  = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
)

// ---- key bindings ----

type listKeys struct {
	Filter   key.Binding
	Fuzzy    key.Binding
	Nav      listNav
	Open     key.Binding
	NextFile key.Binding
	PrevFile key.Binding
	PrevUp   key.Binding
	PrevDown key.Binding
	DiffMode key.Binding
	Shrink   key.Binding
	Grow     key.Binding
	All      key.Binding
	Pickaxe  key.Binding
	Space    key.Binding
	Copy     key.Binding
	Browse   key.Binding
	Help     key.Binding
	Quit     key.Binding
}

func (k listKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Filter, k.Nav.Up, k.Nav.Top, k.Open, k.DiffMode, k.Help, k.Quit}
}

// FullHelp is the panel's list of keys: bubbles lays each group out as a
// column.
func (k listKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Filter, k.Fuzzy, k.Nav.Up, k.Nav.PageUp, k.Nav.Top},
		{k.Open, k.NextFile, k.PrevFile, k.PrevUp},
		{k.Shrink, k.DiffMode, k.Space},
		{k.All, k.Pickaxe, k.Copy, k.Browse, k.Help, k.Quit},
	}
}

// helpOnly is the key of entries that only document something (typing,
// scrolling handled by the viewport): bubbles' help skips bindings without
// keys, so they need one that never matches.
const helpOnly = "help-only"

func defaultListKeys() listKeys {
	return listKeys{
		Filter:   key.NewBinding(key.WithKeys(helpOnly), key.WithHelp("type", "filter")),
		Fuzzy:    key.NewBinding(key.WithKeys(helpOnly), key.WithHelp("~word", "fuzzy filter word")),
		Nav:      defaultListNav(),
		Open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "full diff")),
		NextFile: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next file")),
		PrevFile: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧tab", "previous file")),
		PrevUp:   key.NewBinding(key.WithKeys("shift+up"), key.WithHelp("⇧↑/⇧↓", "scroll the diff")),
		PrevDown: key.NewBinding(key.WithKeys("shift+down")),
		DiffMode: key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "diff mode")),
		Shrink:   key.NewBinding(key.WithKeys("shift+left"), key.WithHelp("⇧←/⇧→", "resize the list")),
		Grow:     key.NewBinding(key.WithKeys("shift+right")),
		All:      key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("^a", "all refs / current branch")),
		Pickaxe:  key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("^g", "search the diffs (git log -S)")),
		Space:    key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("^s", "show / ignore whitespace")),
		Copy:     key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("^y", "copy the hash")),
		Browse:   key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("^o", "open the commit in the browser")),
		Help:     helpBinding(true),
		Quit:     key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc/q", "quit")),
	}
}

type fullKeys struct {
	Scroll   key.Binding
	Page     key.Binding
	Top      key.Binding
	Bottom   key.Binding
	Older    key.Binding
	Newer    key.Binding
	NextFile key.Binding
	PrevFile key.Binding
	Search   key.Binding
	Next     key.Binding
	Prev     key.Binding
	DiffMode key.Binding
	Space    key.Binding
	Copy     key.Binding
	Browse   key.Binding
	Help     key.Binding
	Back     key.Binding
	Quit     key.Binding
}

func (k fullKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Scroll, k.Older, k.NextFile, k.Search, k.Help, k.Back}
}

func (k fullKeys) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Scroll, k.Page, k.Top, k.Older},
		{k.NextFile, k.PrevFile, k.Search, k.Next},
		{k.DiffMode, k.Space, k.Copy},
		{k.Browse, k.Help, k.Back},
	}
}

func defaultFullKeys() fullKeys {
	return fullKeys{
		Scroll:   key.NewBinding(key.WithKeys(helpOnly), key.WithHelp("↑↓/jk", "scroll")),
		Page:     key.NewBinding(key.WithKeys(helpOnly), key.WithHelp("space/b, d/u", "page, half page")),
		Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g/G", "top / bottom")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end")),
		Older:    key.NewBinding(key.WithKeys("]", "right"), key.WithHelp("[/]", "newer/older")),
		Newer:    key.NewBinding(key.WithKeys("[", "left")),
		NextFile: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next file")),
		PrevFile: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧tab", "previous file")),
		Search:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Next:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n/N", "next / previous match")),
		Prev:     key.NewBinding(key.WithKeys("N")),
		DiffMode: key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "diff mode")),
		Space:    key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("^s", "show / ignore whitespace")),
		Copy:     key.NewBinding(key.WithKeys("y", "ctrl+y"), key.WithHelp("y", "copy the hash")),
		Browse:   key.NewBinding(key.WithKeys("o", "ctrl+o"), key.WithHelp("o", "open the commit in the browser")),
		// No input here, so `?` opens the panel too.
		Help: key.NewBinding(key.WithKeys("?", "f1"), key.WithHelp("?", "options")),
		Back: key.NewBinding(key.WithKeys("q", "esc", "enter"), key.WithHelp("q/esc", "back")),
		Quit: key.NewBinding(key.WithKeys("ctrl+c")),
	}
}

// ---- model ----

type uiMode int

const (
	modeList uiMode = iota
	modeFull
	modeSearch
	modePickaxe
	modeFatal // startup error shown inside the TUI (see main)
)

type repoInfoMsg repoInfo

type model struct {
	// data
	opts     logOpts
	commits  []commit
	hashW    int // widest short hash loaded so far
	authorW  int // widest author name loaded so far, capped
	loading  bool
	logErr   string
	logGen   int
	logCh    <-chan logBatch
	stopLog  context.CancelFunc
	seekHash string // commit to land on again after the log restarted
	info     string // repo summary line
	webURL   string

	// filter
	query string // the query hits were computed for
	hits  []hit  // meaningful when the query has terms

	// ui
	mode     uiMode
	fatal    string
	flash    flash // a short-lived status in place of the help line (flash.go)
	panel    panel // options and keys, over the screen while it is open (panel.go)
	prefs    prefs
	cursor   int // index into the visible rows; 0 is the newest commit
	top      int // first row of the visible window
	ti       textinput.Model
	si       textinput.Model // full-view search input
	pi       textinput.Model // content search (pickaxe) input
	prevVP   viewport.Model
	fullVP   viewport.Model
	help     help.Model
	keys     listKeys
	fullKeys fullKeys
	width    int
	height   int
	sized    bool // a WindowSizeMsg arrived; renders before that would use a made-up width

	// preview
	deltaBin string
	hunkBin  string
	renders  map[string]render
	failed   map[string]string // render errors, so they are shown once instead of retried
	details  map[string]detail
	wantKey  string // render the active viewport should show
	shownKey string // render whose final content it does show
	inflight map[string]*pipeline
	dir      int // direction of the last move: the prefetch looks further that way

	// full-view search
	searchTerm string
	matchLines []int
	matchIdx   int
}

func newModel(p prefs, deltaBin string, opts logOpts) *model {
	// The remembered scope is for the plain log: revisions given on the
	// command line say what to list themselves.
	if p.allRefs && len(opts.revs) == 0 {
		opts.all = true
	}
	m := &model{
		opts:     opts,
		loading:  true,
		prefs:    p,
		ti:       newFilterInput("asgitlog", "Search by subject, author, hash, ref…"),
		si:       newInput(stPrompt.Render("/")),
		pi:       newInput(stPrompt.Render("search the diffs (-S) ❯ ")),
		prevVP:   viewport.New(viewport.WithWidth(80), viewport.WithHeight(10)),
		fullVP:   viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		help:     help.New(),
		keys:     defaultListKeys(),
		fullKeys: defaultFullKeys(),
		width:    120,
		height:   40,
		deltaBin: deltaBin,
		inflight: map[string]*pipeline{},
		renders:  map[string]render{},
		failed:   map[string]string{},
		details:  map[string]detail{},
	}
	m.ti.Focus()
	m.resize()
	return m
}

// newInput builds a textinput whose prompt already carries its colors.
func newInput(prompt string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = prompt
	st := ti.Styles()
	st.Focused.Prompt = lipgloss.NewStyle()
	st.Blurred.Prompt = lipgloss.NewStyle()
	ti.SetStyles(st)
	return ti
}

// ---- log stream ----

// startLog (re)starts the log for the current scope. Batches of a previous
// stream are recognized by their generation and dropped.
func (m *model) startLog() tea.Cmd {
	if m.stopLog != nil {
		m.stopLog()
	}
	if c := m.current(); c != nil {
		m.seekHash = c.hash
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.logGen++
	m.logCh, m.stopLog = streamLog(ctx, m.opts, m.logGen), cancel
	m.commits, m.hits, m.hashW, m.authorW = nil, nil, 0, 0
	m.cursor, m.top = 0, 0
	m.loading, m.logErr = true, ""
	return waitLog(m.logCh)
}

func waitLog(ch <-chan logBatch) tea.Cmd {
	return func() tea.Msg {
		b, ok := <-ch
		if !ok {
			return nil
		}
		return b
	}
}

func (m *model) addCommits(batch []commit) {
	from := len(m.commits)
	m.commits = append(m.commits, batch...)
	for i := range batch {
		m.hashW = max(m.hashW, len(batch[i].short()))
		m.authorW = max(m.authorW, min(maxAuthorW, ansi.StringWidth(batch[i].author())))
	}
	if m.filtering() {
		m.hits = append(m.hits, filterCommits(m.commits, nil, from, queryTerms(m.query, false))...)
	}
	if m.seekHash != "" {
		for i := range batch {
			if batch[i].hash == m.seekHash {
				m.seekHash = ""
				m.selectCommit(from + i)
				break
			}
		}
	}
}

// ---- visible rows ----

func (m *model) filtering() bool { return len(queryTerms(m.query, false)) > 0 }

func (m *model) rowCount() int {
	if m.filtering() {
		return len(m.hits)
	}
	return len(m.commits)
}

// rowAt returns the commit on visible row i and its matched corpus offsets.
func (m *model) rowAt(i int) (*commit, []int) {
	if i < 0 || i >= m.rowCount() {
		return nil, nil
	}
	if m.filtering() {
		h := m.hits[i]
		return &m.commits[h.idx], h.matched
	}
	return &m.commits[i], nil
}

func (m *model) current() *commit {
	c, _ := m.rowAt(m.cursor)
	return c
}

// commitIndex is the position in m.commits of the commit under the cursor,
// the identity that survives a filter change.
func (m *model) commitIndex() int {
	if m.cursor < 0 || m.cursor >= m.rowCount() {
		return -1
	}
	if m.filtering() {
		return m.hits[m.cursor].idx
	}
	return m.cursor
}

// selectCommit moves the cursor to the commit at m.commits[idx] when it is
// visible, and reports whether it was.
func (m *model) selectCommit(idx int) bool {
	if idx < 0 {
		return false
	}
	if !m.filtering() {
		m.cursor = idx
		return true
	}
	// hits are in log order, so sorted by idx
	i, ok := slices.BinarySearchFunc(m.hits, idx, func(h hit, idx int) int { return h.idx - idx })
	if ok {
		m.cursor = i
	}
	return ok
}

// applyQuery recomputes the hits for the input's current value. The selection
// stays on the same commit while it still matches (so deleting the query, even
// one character at a time, ends on the commit that was found: a search doubles
// as "take me there") and falls to the first hit otherwise.
func (m *model) applyQuery() {
	q := m.ti.Value()
	if q == m.query {
		return
	}
	keep := m.commitIndex()
	terms := queryTerms(q, false)
	switch {
	case len(terms) == 0:
		m.hits = nil
	case m.filtering() && narrows(m.query, q):
		among := make([]int, len(m.hits))
		for i, h := range m.hits {
			among[i] = h.idx
		}
		m.hits = filterCommits(m.commits, among, 0, terms)
	default:
		m.hits = filterCommits(m.commits, nil, 0, terms)
	}
	m.query = q
	m.cursor, m.top = 0, 0
	m.selectCommit(keep)
	m.clampCursor()
}

func (m *model) clampCursor() {
	n := m.rowCount()
	m.cursor = max(0, min(m.cursor, n-1))
	h := m.listH()
	if m.cursor < m.top {
		m.top = m.cursor
	} else if m.cursor >= m.top+h {
		m.top = m.cursor - h + 1
	}
	m.top = max(0, min(m.top, max(0, n-h)))
}

func (m *model) moveCursor(delta int) tea.Cmd {
	if delta != 0 {
		m.dir = delta / max(delta, -delta)
	}
	m.cursor += delta
	m.clampCursor()
	return m.updatePreview()
}

// ---- geometry ----

// minColumnsW is the narrowest terminal the side-by-side layout is used on;
// below it the list share would not even fit the hash and date.
const minColumnsW = 60

// columns reports the effective layout: the columns setting falls back to
// rows on a narrow terminal, without touching the saved setting.
func (m *model) columns() bool { return m.prefs.layout == layoutColumns && m.width >= minColumnsW }

// The screen is one frame of four sections split by shared edges: the repo
// summary, the filter input, the main section (list and details, split by a
// divider) and the help.
const (
	counterY = 2 // the edge over the filter input, which carries the counter
	mainY    = 4 // the edge over the main section
)

// innerW is the width inside the frame's sides.
func (m *model) innerW() int { return max(20, m.width-2) }

// mainH is the height between the main section's edges.
func (m *model) mainH() int { return max(4, m.height-mainY-2-2) } // minus the help line and the bottom edge

// detailsH and detailsW are the area of the commit details inside the main
// section, including the cell of padding on each side.
func (m *model) detailsH() int {
	if m.columns() {
		return m.mainH()
	}
	return min(max(2, m.mainH()*m.prefs.splitRows/100), m.mainH()-2)
}

func (m *model) detailsW() int {
	if m.columns() {
		return max(14, m.innerW()*m.prefs.splitColumns/100)
	}
	return m.innerW()
}

func (m *model) listH() int {
	if m.columns() {
		return m.mainH()
	}
	return max(1, m.mainH()-1-m.detailsH()) // minus the divider
}

func (m *model) listW() int {
	if m.columns() {
		return max(10, m.innerW()-1-m.detailsW()) // minus the divider
	}
	return m.innerW()
}

// listY is the first screen line of the list, right under the main section's
// top edge.
func (m *model) listY() int { return mainY + 1 }

// overList reports whether a screen cell is inside the list.
func (m *model) overList(x, y int) bool {
	return inList(x, y, m.listY(), m.listW(), m.listH())
}

func (m *model) resize() {
	m.prevVP.SetWidth(max(10, m.detailsW()-2))
	m.prevVP.SetHeight(max(1, m.detailsH()))
	m.fullVP.SetWidth(max(10, m.width))
	m.fullVP.SetHeight(max(1, m.height-3))
	m.help.SetWidth(max(0, m.width-4))
	sizeInput(&m.ti, m.width-4)
	m.clampCursor()
}

func (m *model) rowLayout() rowLayout {
	return rowLayout{width: m.listW(), hashW: m.hashW, authorW: m.authorW, compact: m.columns(), now: time.Now().Unix()}
}

// ---- preview ----

func (m *model) inFull() bool { return m.mode == modeFull || m.mode == modeSearch }

func (m *model) activeVP() *viewport.Model {
	if m.inFull() {
		return &m.fullVP
	}
	return &m.prevVP
}

// A pipeline is a render in flight. A cancelled one is dying: it still counts
// until it reports back.
type pipeline struct {
	cancel context.CancelFunc
	dying  bool
}

const (
	// maxPipelines bounds the renders running at once, dying ones included,
	// so holding an arrow key never piles up processes: a selection that moved
	// on cancels what it no longer needs and starts when that reports back.
	maxPipelines = 3
	// prefetchAhead is how many rows are rendered ahead in the direction of
	// travel; the row behind the cursor is rendered too.
	prefetchAhead = 4
)

// window is the rows worth having rendered besides the selected one, the most
// useful first.
func (m *model) window() []int {
	d := m.dir
	if d == 0 {
		d = 1 // a fresh list is read downwards
	}
	rows := []int{m.cursor + d, m.cursor - d}
	for i := 2; i <= prefetchAhead; i++ {
		rows = append(rows, m.cursor+i*d)
	}
	return rows
}

// updatePreview points the active viewport at the selected commit: from the
// render cache when possible, otherwise the instant header plus a placeholder
// while the diff renders. Renders the selection left behind are cancelled
// unless they are still in the window around it. Once the selection is served
// (a partial render counts), the window is rendered ahead, in parallel, so
// that moving through it is instant.
func (m *model) updatePreview() tea.Cmd {
	if !m.sized || m.mode == modeFatal {
		return nil
	}
	vp := m.activeVP()
	c := m.current()
	if c == nil {
		m.wantKey, m.shownKey = "", ""
		vp.SetContent("")
		return nil
	}
	w := vp.Width()
	mode := effectiveDiff(m.prefs.diff, w)
	k := previewKey(c.hash, w, m.tool(), mode)
	if k != m.wantKey {
		m.wantKey = k
		m.clearSearch()
		vp.GotoTop()
	}
	m.cancelStale(k, w, mode)
	if r, ok := m.renders[k]; ok {
		if m.shownKey != k {
			m.shownKey = k
			vp.SetContent(r.content)
		}
		return m.prefetch(w, mode)
	}
	var d *detail
	if known, ok := m.details[c.hash]; ok {
		d = &known
	}
	if msg, ok := m.failed[k]; ok {
		m.shownKey = k
		vp.SetContent(previewHeader(c, d, w) + "\n\n" + stError.Render(msg))
		return nil
	}
	m.shownKey = ""
	vp.SetContent(previewHeader(c, d, w) + "\n\n" + stDim.Render("rendering…"))
	return m.startRender(c, w, mode, true)
}

// windowKeys are the render keys of the selection and its window, the most
// wanted first.
func (m *model) windowKeys(want string, w int, mode string) []string {
	keys := []string{want}
	for _, i := range m.window() {
		if c, _ := m.rowAt(i); c != nil {
			keys = append(keys, previewKey(c.hash, w, m.tool(), mode))
		}
	}
	return keys
}

// cancelStale cancels the renders nobody is waiting for anymore.
func (m *model) cancelStale(want string, w int, mode string) {
	if len(m.inflight) == 0 {
		return
	}
	keys := m.windowKeys(want, w, mode)
	for k, p := range m.inflight {
		if !p.dying && !slices.Contains(keys, k) {
			p.cancel()
			p.dying = true
		}
	}
}

// startRender starts a render unless it is in flight already (a dying one
// starts again when it reports back: updatePreview runs on every report).
// When every slot is taken the wanted render takes the least wanted one's,
// once that has reported back; a prefetch just waits for a free slot.
func (m *model) startRender(c *commit, w int, mode string, wanted bool) tea.Cmd {
	k := previewKey(c.hash, w, m.tool(), mode)
	if m.inflight[k] != nil {
		return nil
	}
	if len(m.inflight) >= maxPipelines {
		dying := false
		for _, p := range m.inflight {
			dying = dying || p.dying
		}
		if wanted && !dying { // a dying render frees its slot in a moment
			keys := m.windowKeys(k, w, mode)
			for i := len(keys) - 1; i > 0; i-- {
				if p := m.inflight[keys[i]]; p != nil && !p.dying {
					p.cancel()
					p.dying = true
					break
				}
			}
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.inflight[k] = &pipeline{cancel: cancel}
	return renderPreviewCmd(ctx, *c, w, mode, m.tool(), m.opts.paths)
}

// prefetch renders the window around the cursor in the free slots.
func (m *model) prefetch(w int, mode string) tea.Cmd {
	var cmds []tea.Cmd
	for _, i := range m.window() {
		if len(m.inflight) >= maxPipelines {
			break
		}
		c, _ := m.rowAt(i)
		if c == nil {
			continue
		}
		k := previewKey(c.hash, w, m.tool(), mode)
		if _, ok := m.renders[k]; ok {
			continue
		}
		if _, ok := m.failed[k]; ok {
			continue
		}
		cmds = append(cmds, m.startRender(c, w, mode, false))
	}
	return tea.Batch(cmds...)
}

// maxRenders bounds the render cache; it is simply dropped when full (the
// current commit re-renders in milliseconds).
const maxRenders = 128

func (m *model) handlePreview(msg previewMsg) tea.Cmd {
	// A partial render is shown while its pipeline goes on: it stays in flight
	// until what follows reports back.
	if p := m.inflight[msg.key]; p != nil && msg.next == nil {
		p.cancel()
		delete(m.inflight, msg.key)
	}
	if msg.key == m.shownKey {
		m.shownKey = "" // showing the partial render this one replaces
	}
	partial := m.renders[msg.key].partial
	switch {
	case msg.cancelled:
		// updatePreview starts whatever is wanted now (possibly this same
		// render again, after an A, B, A selection). A partial render is not
		// worth keeping: it would never get refined.
		if partial {
			delete(m.renders, msg.key)
		}
	case msg.err != nil && partial:
		// hunk died on the way: what it had drawn is as good as it gets.
		r := m.renders[msg.key]
		r.partial = false
		m.renders[msg.key] = r
	case msg.err != nil:
		m.failed[msg.key] = msg.err.Error()
	default:
		if len(m.renders) >= maxRenders {
			m.renders, m.details, m.failed = map[string]render{}, map[string]detail{}, map[string]string{}
			m.shownKey = ""
		}
		m.renders[msg.key] = msg.render
		m.details[msg.hash] = msg.detail
	}
	return tea.Batch(msg.next, m.updatePreview())
}

// resetPreview forces the active viewport to be filled again (mode switch:
// the two viewports can share a key when they are equally wide).
func (m *model) resetPreview() tea.Cmd {
	m.wantKey, m.shownKey = "", ""
	return m.updatePreview()
}

// jumpFile scrolls the active viewport to the next (or previous) file of the
// diff.
func (m *model) jumpFile(dir int) {
	r, ok := m.renders[m.wantKey]
	if !ok || len(r.files) == 0 {
		return
	}
	vp := m.activeVP()
	y := vp.YOffset()
	if dir > 0 {
		for _, l := range r.files {
			if l > y {
				vp.SetYOffset(l)
				return
			}
		}
		return
	}
	for i := len(r.files) - 1; i >= 0; i-- {
		if r.files[i] < y {
			vp.SetYOffset(r.files[i])
			return
		}
	}
	vp.GotoTop()
}

// ---- full-view search ----

func (m *model) clearSearch() {
	if m.searchTerm == "" {
		return
	}
	m.searchTerm, m.matchLines, m.matchIdx = "", nil, 0
	m.shownKey = "" // repaint without the highlights
}

// runSearch highlights every case-insensitive occurrence of term in the full
// view and jumps to the first one at or below the current position. The
// viewport's own highlight support is not used: it tracks line breaks on the
// raw content with offsets from the stripped one, which breaks on ANSI input
// like delta's.
func (m *model) runSearch(term string) {
	m.clearSearch()
	r, ok := m.renders[m.wantKey]
	if term == "" || !ok {
		if ok {
			m.fullVP.SetContent(r.content)
			m.shownKey = m.wantKey
		}
		return
	}
	m.searchTerm = term
	needle := strings.ToLower(term)
	lines := strings.Split(r.content, "\n")
	for i, line := range lines {
		plain := strings.ToLower(ansi.Strip(line))
		var ranges []lipgloss.Range
		for from := 0; ; {
			j := strings.Index(plain[from:], needle)
			if j < 0 {
				break
			}
			start := ansi.StringWidth(plain[:from+j])
			ranges = append(ranges, lipgloss.NewRange(start, start+ansi.StringWidth(needle), stFound))
			from += j + len(needle)
		}
		if len(ranges) > 0 {
			lines[i] = lipgloss.StyleRanges(line, ranges...)
			m.matchLines = append(m.matchLines, i)
		}
	}
	m.fullVP.SetContent(strings.Join(lines, "\n"))
	m.shownKey = m.wantKey
	m.matchIdx = 0
	for i, l := range m.matchLines {
		if l >= m.fullVP.YOffset() {
			m.matchIdx = i
			break
		}
	}
	m.showMatch()
}

func (m *model) showMatch() {
	if len(m.matchLines) == 0 {
		return
	}
	line := m.matchLines[m.matchIdx]
	if line < m.fullVP.YOffset() || line >= m.fullVP.YOffset()+m.fullVP.Height() {
		m.fullVP.SetYOffset(max(0, line-m.fullVP.Height()/3))
	}
}

func (m *model) stepMatch(d int) {
	if n := len(m.matchLines); n > 0 {
		m.matchIdx = (m.matchIdx + d + n) % n
		m.showMatch()
	}
}

// ---- settings and actions ----

// tool is what renders the diffs: hunk when it was asked for and is installed,
// else delta (or plain git, without delta).
func (m *model) tool() diffTool {
	return pickTool(m.prefs.tool, m.deltaBin, m.hunkBin, m.prefs.ignoreWS)
}

// options is what the panel offers in the current view. The renderer and the
// layout are chosen there and nowhere else; the rest keep their keys.
func (m *model) options() []option {
	cur := func(on bool) int {
		if on {
			return 1
		}
		return 0
	}
	diff := map[string]int{diffAuto: 0, diffSBS: 1, diffSingle: 2}
	opts := []option{
		{id: "renderer", label: "Diff renderer", values: []string{toolDelta, toolHunk}, cur: cur(m.prefs.tool == toolHunk)},
		{id: "diff", label: "Diff mode", values: []string{"auto", "side-by-side", "single column"}, cur: diff[m.prefs.diff], key: "^t"},
		{id: "whitespace", label: "Whitespace", values: []string{"show", "ignore"}, cur: cur(m.prefs.ignoreWS), key: "^s"},
	}
	if m.inFull() {
		return opts
	}
	return append(opts,
		option{id: "layout", label: "Layout", values: []string{layoutRows, layoutColumns}, cur: cur(m.prefs.layout == layoutColumns)},
		option{id: "refs", label: "History", values: []string{"current branch", "all refs"}, cur: cur(m.opts.all), key: "^a"},
	)
}

// setOption changes a setting, remembers it and says so. The keys and the panel both come through here.
func (m *model) setOption(id string, v int) tea.Cmd {
	switch id {
	case "renderer":
		if m.hunkBin == "" {
			return m.setFlash("hunk not found")
		}
		m.prefs.tool = []string{toolDelta, toolHunk}[v]
		savePref("renderer", m.prefs.tool)
		return tea.Batch(m.updatePreview(), m.setFlash("diffs by "+m.prefs.tool))
	case "diff":
		m.prefs.diff = []string{diffAuto, diffSBS, diffSingle}[v]
		savePref("diff", m.prefs.diff)
		if m.inFull() {
			return m.updatePreview() // the full view's title names the mode
		}
		label := "diff: " + diffLabel(m.prefs.diff, m.prevVP.Width())
		if m.deltaBin == "" {
			label = "delta not found: plain git colors"
		}
		return tea.Batch(m.updatePreview(), m.setFlash(label))
	case "whitespace":
		m.prefs.ignoreWS = v == 1
		value, label := "show", "whitespace: shown"
		if m.prefs.ignoreWS {
			value, label = "ignore", "whitespace: ignored"
		}
		savePref("whitespace", value)
		return tea.Batch(m.updatePreview(), m.setFlash(label))
	case "layout":
		m.prefs.layout = []string{layoutRows, layoutColumns}[v]
		savePref("layout", m.prefs.layout)
		m.resize()
		return m.updatePreview()
	case "refs":
		m.opts.all = v == 1
		value := "current"
		if m.opts.all {
			value = "all"
		}
		savePref("refs", value)
		return m.startLog()
	}
	return nil
}

// cycle moves a setting to its next value, for the keys that do so directly.
func (m *model) cycle(id string) tea.Cmd { return m.setOption(id, nextValue(m.options(), id)) }

// resizeList moves the divider between list and preview by one step.
func (m *model) resizeList(grow bool) tea.Cmd {
	split, name := &m.prefs.splitRows, "split-rows"
	if m.columns() {
		split, name = &m.prefs.splitColumns, "split-columns"
	}
	step := splitStep
	if grow {
		step = -splitStep // the setting is the preview's share
	}
	*split = max(splitMin, min(splitMax, *split+step))
	savePref(name, strconv.Itoa(*split))
	m.resize()
	return m.updatePreview()
}

func (m *model) setFlash(s string) tea.Cmd { return m.flash.set(s) }

func (m *model) copyHash() tea.Cmd {
	c := m.current()
	if c == nil || c.wt {
		return m.setFlash("nothing to copy")
	}
	return copyCmd("asgitlog", "", c.hash)
}

func (m *model) browse() tea.Cmd {
	c := m.current()
	switch {
	case c == nil || c.wt:
		return m.setFlash("nothing to open")
	case m.webURL == "":
		return m.setFlash("no remote with a web URL")
	}
	url := commitURL(m.webURL, c.hash)
	return func() tea.Msg {
		openURL("asgitlog", url)
		return flashMsg("opened " + url)
	}
}

// ---- bubbletea ----

func (m *model) Init() tea.Cmd {
	if m.mode == modeFatal {
		return nil
	}
	return tea.Batch(textinput.Blink, func() tea.Msg { return repoInfoMsg(loadRepoInfo()) }, m.startLog())
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height, m.sized = msg.Width, msg.Height, true
		m.resize()
		return m, m.updatePreview()

	case repoInfoMsg:
		m.info, m.webURL = repoInfo(msg).String(), msg.WebURL
		return m, nil

	case logBatch:
		if msg.gen != m.logGen {
			return m, nil // a stream that was replaced
		}
		first := len(m.commits) == 0
		m.addCommits(msg.commits)
		if msg.err != nil {
			m.logErr = msg.err.Error()
		}
		var cmds []tea.Cmd
		if msg.done {
			m.loading, m.seekHash = false, ""
		} else {
			cmds = append(cmds, waitLog(m.logCh))
		}
		m.clampCursor()
		if first || m.wantKey == "" || msg.done {
			cmds = append(cmds, m.updatePreview())
		}
		return m, tea.Batch(cmds...)

	case previewMsg:
		return m, m.handlePreview(msg)

	case flashMsg:
		return m, m.setFlash(string(msg))

	case clearFlashMsg:
		m.flash.clear(msg)
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseWheelMsg:
		// Over the list the wheel moves the selection (the way to get back up
		// a long history without the keyboard); anywhere else it scrolls the
		// diff.
		if m.panel.open {
			return m, nil
		}
		if m.mode == modeList && m.overList(msg.X, msg.Y) {
			if k, ok := wheelKey(msg); ok {
				return m, m.moveCursor(m.keys.Nav.move(k, m.cursor, m.rowCount(), m.listH(), nil) - m.cursor)
			}
			return m, nil
		}
		if m.mode == modeList || m.mode == modeFull {
			vp := m.activeVP()
			*vp, _ = vp.Update(msg)
		}
		return m, nil

	case tea.MouseClickMsg:
		if m.panel.open {
			return m, nil
		}
		return m, m.handleClick(msg)
	}

	var cmd tea.Cmd
	switch m.mode {
	case modeSearch:
		m.si, cmd = m.si.Update(msg)
	case modePickaxe:
		m.pi, cmd = m.pi.Update(msg)
	default:
		m.ti, cmd = m.ti.Update(msg)
	}
	return m, cmd
}

func (m *model) quit() tea.Cmd {
	if m.stopLog != nil {
		m.stopLog()
	}
	for _, p := range m.inflight {
		p.cancel()
	}
	return tea.Quit
}

func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.panel.open {
		// The panel takes every key: esc closes it before anything else.
		if msg.String() == "ctrl+c" {
			return m, m.quit()
		}
		if a := m.panel.update(msg, m.options()); a.id != "" {
			return m, m.setOption(a.id, a.value)
		}
		return m, nil
	}
	switch m.mode {
	case modeFatal:
		return m, tea.Quit
	case modeFull:
		return m, m.handleFullKey(msg)
	case modeSearch:
		return m, m.handleSearchKey(msg)
	case modePickaxe:
		return m, m.handlePickaxeKey(msg)
	}

	// The list reads top-down in both layouts, newest commit first, right
	// under the filter input: up the screen is up the log.
	switch {
	case m.keys.Nav.matches(msg):
		return m, m.moveCursor(m.keys.Nav.move(msg, m.cursor, m.rowCount(), m.listH(), nil) - m.cursor)
	case key.Matches(msg, m.keys.Quit), msg.String() == "q" && m.ti.Value() == "":
		// q quits only while the filter is empty; otherwise it is text.
		return m, m.quit()
	case isHelpKey(msg):
		m.panel.toggle()
		return m, nil
	case key.Matches(msg, m.keys.Open):
		if m.current() == nil {
			return m, nil
		}
		m.mode = modeFull
		m.ti.Blur()
		return m, m.resetPreview()
	case key.Matches(msg, m.keys.DiffMode):
		return m, m.cycle("diff")
	case key.Matches(msg, m.keys.Space):
		return m, m.cycle("whitespace")
	case key.Matches(msg, m.keys.Shrink):
		return m, m.resizeList(false)
	case key.Matches(msg, m.keys.Grow):
		return m, m.resizeList(true)
	case key.Matches(msg, m.keys.PrevUp):
		m.prevVP.ScrollUp(3)
		return m, nil
	case key.Matches(msg, m.keys.PrevDown):
		m.prevVP.ScrollDown(3)
		return m, nil
	case key.Matches(msg, m.keys.NextFile):
		m.jumpFile(+1)
		return m, nil
	case key.Matches(msg, m.keys.PrevFile):
		m.jumpFile(-1)
		return m, nil
	case key.Matches(msg, m.keys.Copy):
		return m, m.copyHash()
	case key.Matches(msg, m.keys.Browse):
		return m, m.browse()
	case key.Matches(msg, m.keys.All):
		return m, m.cycle("refs")
	case key.Matches(msg, m.keys.Pickaxe):
		m.mode = modePickaxe
		m.ti.Blur()
		m.pi.SetValue(m.opts.pickaxe)
		m.pi.CursorEnd()
		return m, m.pi.Focus()
	}

	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	m.applyQuery()
	return m, tea.Batch(cmd, m.updatePreview())
}

func (m *model) handleFullKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.fullKeys.Quit):
		return m.quit()
	case key.Matches(msg, m.fullKeys.Back):
		// esc first drops an active search, then leaves the full view.
		if m.searchTerm != "" && msg.String() == "esc" {
			m.clearSearch()
			return m.updatePreview()
		}
		m.mode = modeList
		m.clearSearch()
		m.resize()
		return tea.Batch(m.ti.Focus(), m.resetPreview())
	case key.Matches(msg, m.fullKeys.Help):
		m.panel.toggle()
	case key.Matches(msg, m.fullKeys.DiffMode):
		return m.cycle("diff")
	case key.Matches(msg, m.fullKeys.Space):
		return m.cycle("whitespace")
	case key.Matches(msg, m.fullKeys.Older):
		return m.moveCursor(+1)
	case key.Matches(msg, m.fullKeys.Newer):
		return m.moveCursor(-1)
	case key.Matches(msg, m.fullKeys.NextFile):
		m.jumpFile(+1)
	case key.Matches(msg, m.fullKeys.PrevFile):
		m.jumpFile(-1)
	case key.Matches(msg, m.fullKeys.Copy):
		return m.copyHash()
	case key.Matches(msg, m.fullKeys.Browse):
		return m.browse()
	case key.Matches(msg, m.fullKeys.Top):
		m.fullVP.GotoTop()
	case key.Matches(msg, m.fullKeys.Bottom):
		m.fullVP.GotoBottom()
	case key.Matches(msg, m.fullKeys.Search):
		m.mode = modeSearch
		m.si.SetValue("")
		return m.si.Focus()
	case key.Matches(msg, m.fullKeys.Next):
		m.stepMatch(+1)
	case key.Matches(msg, m.fullKeys.Prev):
		m.stepMatch(-1)
	default:
		m.fullVP, _ = m.fullVP.Update(msg)
	}
	return nil
}

func (m *model) handleSearchKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		m.mode = modeFull
		m.si.Blur()
		return nil
	case "enter":
		m.mode = modeFull
		m.si.Blur()
		m.runSearch(m.si.Value())
		return nil
	}
	var cmd tea.Cmd
	m.si, cmd = m.si.Update(msg)
	return cmd
}

// handlePickaxeKey drives the content search input: enter rescopes the log to
// the commits that add or remove the text (an empty text lifts the scope).
func (m *model) handlePickaxeKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc", "enter":
		m.mode = modeList
		m.pi.Blur()
		cmds := []tea.Cmd{m.ti.Focus()}
		if text := strings.TrimSpace(m.pi.Value()); msg.String() == "enter" && text != m.opts.pickaxe {
			m.opts.pickaxe = text
			cmds = append(cmds, m.startLog())
		}
		return tea.Batch(cmds...)
	}
	var cmd tea.Cmd
	m.pi, cmd = m.pi.Update(msg)
	return cmd
}

// handleClick moves the selection to the list row under a left click. It
// never opens the full diff: that stays on enter.
func (m *model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	if m.mode != modeList || msg.Button != tea.MouseLeft {
		return nil
	}
	if !m.overList(msg.X, msg.Y) {
		return nil
	}
	i, ok := rowUnder(msg.Y, m.listY(), m.top, m.rowCount())
	if !ok || i == m.cursor {
		return nil
	}
	m.cursor = i
	return m.updatePreview()
}

// ---- view ----

func (m *model) View() tea.View {
	var s string
	switch m.mode {
	case modeFatal:
		s = "\n  " + stError.Render(m.fatal) + "\n\n  " + stDim.Render("press any key to close")
	case modeFull, modeSearch:
		s = m.fullView()
	default:
		s = m.listView()
	}
	if m.panel.open {
		var keys help.KeyMap = m.keys
		if m.inFull() {
			keys = m.fullKeys
		}
		lines := strings.Split(s, "\n")
		box := panelLines(m.options(), m.panel.cursor, keyLines(m.help, keys, m.width-10), m.width-4, len(lines)-2)
		s = strings.Join(overlay(lines, box, m.width), "\n")
	}
	v := tea.NewView(s)
	v.AltScreen = true
	if m.mode == modeList || m.mode == modeFull {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// listView stacks the four sections in one frame: repo summary, filter input
// (the edge over it carries the matches/total counter and the log's scope),
// the main section and the help. Neighbours share an edge, so no line is spent
// on a border of their own.
func (m *model) listView() string {
	w := m.width
	input := m.ti.View()
	if m.mode == modePickaxe {
		input = m.pi.View()
	}
	out := []string{
		hline(w, "╭", "╮", "", ""),
		framed(w, stInfo.Render(m.info)),
		hline(w, "├", "┤", "", withDevMark(m.status())),
		framed(w, input),
	}
	out = append(out, m.mainLines()...)
	out = append(out, framed(w, m.footLine(m.keys)), hline(w, "╰", "╯", "", ""))
	return strings.Join(out, "\n")
}

// scope describes what the log is limited to, beyond the current branch.
func (m *model) scope() string {
	var parts []string
	if m.opts.all {
		parts = append(parts, "all refs")
	}
	if len(m.opts.revs) > 0 {
		parts = append(parts, strings.Join(m.opts.revs, " "))
	}
	if len(m.opts.paths) > 0 {
		parts = append(parts, "-- "+strings.Join(m.opts.paths, " "))
	}
	if m.opts.pickaxe != "" {
		parts = append(parts, "-S"+strconv.Quote(m.opts.pickaxe))
	}
	return strings.Join(parts, "  ")
}

// counter is the matches/total count, for the edge under the list.
func (m *model) counter() string {
	return stCount.Render(strconv.Itoa(m.rowCount()) + "/" + strconv.Itoa(len(m.commits)))
}

// status is the scope and the loading mark, for the edge over the input.
func (m *model) status() string {
	var parts []string
	if scope := m.scope(); scope != "" {
		parts = append(parts, stScope.Render("["+truncate(scope, max(10, m.width/2))+"]"))
	}
	if m.loading {
		parts = append(parts, stDim.Render("loading…"))
	}
	return strings.Join(parts, " ")
}

// listLines are the visible rows, exactly listH lines of listW cells.
func (m *model) listLines() []string {
	h, l := m.listH(), m.rowLayout()
	lines := make([]string, 0, h)
	for i := m.top; i < m.top+h && i < m.rowCount(); i++ {
		c, matched := m.rowAt(i)
		sel := i == m.cursor
		lines = append(lines, renderSegs(rowSegs(c, l, sel), matched, sel))
	}
	if len(lines) == 0 {
		msg := ""
		switch {
		case m.logErr != "":
			msg = " " + stError.Render(truncate(m.logErr, l.width-1))
		case m.filtering() || m.opts.pickaxe != "" && !m.loading:
			msg = stDim.Render("  no matching commits")
		case !m.loading:
			msg = stDim.Render("  no commits")
		}
		lines = append(lines, fit(msg, l.width))
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", l.width))
	}
	return lines
}

// mainLines is the list and the commit details between the edges shared with
// the filter input and the help, split by a divider: horizontal in the rows
// layout, vertical in the columns layout. The edge over the details is a plain
// line; the bottom edge carries the scroll position.
func (m *model) mainLines() []string {
	w, side := m.width, stDim.Render("│")
	pos := diffEdge(m.prefs.ignoreWS, scrollPos(&m.prevVP))
	list := m.listLines()
	details := strings.Split(m.prevVP.View(), "\n")
	dw := m.detailsW()

	var out []string
	if m.columns() {
		lw := m.listW()
		out = append(out, stDim.Render("├"+strings.Repeat("─", lw))+hline(dw+2, "┬", "┤", "", ""))
		for i := range list {
			d := ""
			if i < len(details) {
				d = details[i]
			}
			out = append(out, side+list[i]+side+fit(" "+d, dw)+side)
		}
		return append(out, hline(lw+1, "├", "", "", m.counter())+hline(dw+2, "┴", "┤", "", pos))
	}
	out = append(out, hline(w, "├", "┤", "", ""))
	for _, l := range list {
		out = append(out, side+l+side)
	}
	out = append(out, hline(w, "├", "┤", "", m.counter()))
	for _, d := range details {
		out = append(out, side+fit(" "+d, dw)+side)
	}
	return append(out, hline(w, "├", "┤", "", pos))
}

// footLine is the help line (bubbles' short help, cut to the width), or a
// confirmation while one is showing.
func (m *model) footLine(keys help.KeyMap) string {
	if m.flash.text != "" {
		return m.flash.view(m.width - 4)
	}
	return helpLine(m.help, keys, m.width-4)
}

func (m *model) fullView() string {
	title := ""
	if c := m.current(); c != nil {
		title = stDim.Render(strconv.Itoa(m.cursor+1)+"/"+strconv.Itoa(m.rowCount())) + "  " +
			stHash.Render(c.short()) + " " + truncate(c.subject(), max(10, m.width/2))
	}
	pos := strconv.Itoa(int(m.fullVP.ScrollPercent()*100)) + "%"
	mode := stLabel.Render(diffLabel(m.prefs.diff, m.fullVP.Width()))
	if m.prefs.ignoreWS {
		mode += "  " + stScope.Render("[-w]")
	}
	head := title + "  " + mode + "  " + stDim.Render(pos)
	if m.searchTerm != "" {
		found := "no matches"
		if n := len(m.matchLines); n > 0 {
			found = "match " + strconv.Itoa(m.matchIdx+1) + "/" + strconv.Itoa(n)
		}
		head += "  " + stDim.Render("/"+m.searchTerm+" ("+found+")")
	}
	foot := "  " + m.footLine(m.fullKeys)
	if m.mode == modeSearch {
		foot = m.si.View()
	}
	rule := stDim.Render(strings.Repeat("─", m.width))
	return truncate("  "+head, m.width) + "\n" + rule + "\n" + m.fullVP.View() + "\n" + foot
}
