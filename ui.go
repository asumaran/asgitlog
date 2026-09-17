package main

// The bubbletea model. Browsing (modeList): a filter input on top, the commit
// list and the preview (stacked in the rows layout, side by side in the
// columns layout), then the repo summary and the key help. Enter opens the
// same preview full screen (modeFull) with pager keys and a text search
// (modeSearch is its input line).

import (
	"context"
	"os"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func homeRel(p string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" && (p == h || strings.HasPrefix(p, h+"/")) {
		return "~" + strings.TrimPrefix(p, h)
	}
	return p
}

func truncate(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}

// ---- styles ----

var (
	selBg   = lipgloss.Color("8")
	matchFg = lipgloss.Color("13")

	stPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	stDev    = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	stCursor = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stTitle  = lipgloss.NewStyle().Bold(true)
	stError  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	stLabel  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	stFound  = lipgloss.NewStyle().Reverse(true)

	// list columns and preview header, after git's own palette
	stHash      = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	stAuthor    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	stDate      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stKey       = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	stAdded     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stDeleted   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	stRefHead   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	stRefBranch = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
	stRefRemote = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	stRefTag    = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	stRefOther  = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
)

// ---- key bindings ----

type listKeys struct {
	Filter   key.Binding
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding
	Open     key.Binding
	DiffMode key.Binding
	Layout   key.Binding
	PrevUp   key.Binding
	PrevDown key.Binding
	Quit     key.Binding
}

func (k listKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Filter, k.Up, k.Open, k.DiffMode, k.Layout, k.PrevDown, k.Quit}
}
func (k listKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

func defaultListKeys() listKeys {
	return listKeys{
		// Help-only entry; it needs a key to count as enabled.
		Filter:   key.NewBinding(key.WithKeys("type"), key.WithHelp("type", "filter")),
		Up:       key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑/↓", "move")),
		Down:     key.NewBinding(key.WithKeys("down", "ctrl+n")),
		PageUp:   key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "page down")),
		Open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "full diff")),
		DiffMode: key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "diff mode")),
		Layout:   key.NewBinding(key.WithKeys("ctrl+l"), key.WithHelp("^l", "layout")),
		PrevUp:   key.NewBinding(key.WithKeys("shift+up"), key.WithHelp("⇧↑", "")),
		PrevDown: key.NewBinding(key.WithKeys("shift+down"), key.WithHelp("⇧↑/⇧↓", "scroll diff")),
		Quit:     key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "quit")),
	}
}

type fullKeys struct {
	Scroll   key.Binding
	Page     key.Binding
	Ends     key.Binding
	Top      key.Binding
	Bottom   key.Binding
	DiffMode key.Binding
	Search   key.Binding
	Next     key.Binding
	Prev     key.Binding
	Back     key.Binding
	Quit     key.Binding
}

func (k fullKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Scroll, k.Page, k.Ends, k.Search, k.Next, k.DiffMode, k.Back}
}
func (k fullKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

func defaultFullKeys() fullKeys {
	return fullKeys{
		// Help-only entries (scrolling is the viewport's own key map). They
		// need a key to count as enabled, or the help skips them.
		Scroll:   key.NewBinding(key.WithKeys("help"), key.WithHelp("↑↓/jk", "scroll")),
		Page:     key.NewBinding(key.WithKeys("help"), key.WithHelp("space/b", "page")),
		Ends:     key.NewBinding(key.WithKeys("help"), key.WithHelp("g/G", "ends")),
		Top:      key.NewBinding(key.WithKeys("g", "home")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end")),
		DiffMode: key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "diff mode")),
		Search:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Next:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n/N", "match")),
		Prev:     key.NewBinding(key.WithKeys("N")),
		Back:     key.NewBinding(key.WithKeys("q", "esc", "enter"), key.WithHelp("q/esc", "back")),
		Quit:     key.NewBinding(key.WithKeys("ctrl+c")),
	}
}

// ---- model ----

type uiMode int

const (
	modeList uiMode = iota
	modeFull
	modeSearch
	modeFatal // startup error shown inside the TUI (see main)
)

type repoInfoMsg repoInfo

type model struct {
	// data
	commits []commit
	hashW   int // widest short hash loaded so far
	loading bool
	logErr  string
	logCh   <-chan logBatch
	stopLog context.CancelFunc
	info    string // repo summary line

	// filter
	query string // the query hits were computed for
	hits  []hit  // meaningful when the query has terms

	// ui
	mode     uiMode
	fatal    string
	prefs    prefs
	cursor   int // index into the visible rows
	top      int // first visible row
	ti       textinput.Model
	si       textinput.Model // full-view search input
	prevVP   viewport.Model
	fullVP   viewport.Model
	help     help.Model
	keys     listKeys
	fullKeys fullKeys
	width    int
	height   int
	sized    bool // a WindowSizeMsg arrived; renders before that would use a made-up width

	// preview
	deltaBin     string
	renders      map[string]string
	details      map[string]detail
	wantKey      string // render the active viewport should show
	shownKey     string // render whose final content it does show
	inflight     string
	cancelRender context.CancelFunc
	renderErr    string

	// full-view search
	searchTerm string
	matchLines []int
	matchIdx   int
}

func newModel(p prefs, deltaBin string) *model {
	m := &model{
		loading:  true,
		prefs:    p,
		ti:       newInput(promptText()),
		si:       newInput(stPrompt.Render("/")),
		prevVP:   viewport.New(viewport.WithWidth(80), viewport.WithHeight(10)),
		fullVP:   viewport.New(viewport.WithWidth(80), viewport.WithHeight(20)),
		help:     help.New(),
		keys:     defaultListKeys(),
		fullKeys: defaultFullKeys(),
		width:    120,
		height:   40,
		deltaBin: deltaBin,
		renders:  map[string]string{},
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

// promptText builds the filter prompt, with an orange "(dev)" marker on
// non-release builds.
func promptText() string {
	if strings.HasPrefix(version, "v") {
		return stPrompt.Render("asgitlog ❯ ")
	}
	return stPrompt.Render("asgitlog (") + stDev.Render("dev") + stPrompt.Render(") ❯ ")
}

// ---- visible rows ----

func (m *model) filtering() bool { return len(queryTerms(m.query)) > 0 }

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

// applyQuery recomputes the hits for the input's current value. The selection
// stays on the same commit while it still matches (so deleting the query, even
// one character at a time, ends on the commit that was found: a search doubles
// as "take me there") and falls to the first hit otherwise, like fzf.
func (m *model) applyQuery() {
	q := m.ti.Value()
	if q == m.query {
		return
	}
	keep := m.commitIndex()
	terms := queryTerms(q)
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
	if keep >= 0 {
		if !m.filtering() {
			m.cursor = keep
		} else if i, ok := slices.BinarySearchFunc(m.hits, keep, func(h hit, idx int) int { return h.idx - idx }); ok {
			m.cursor = i // hits are in log order, so sorted by idx
		}
	}
	m.clampCursor()
}

func (m *model) addCommits(batch []commit) {
	from := len(m.commits)
	m.commits = append(m.commits, batch...)
	for i := range batch {
		if w := len(batch[i].short()); w > m.hashW {
			m.hashW = w
		}
	}
	if m.filtering() {
		m.hits = append(m.hits, filterCommits(m.commits, nil, from, queryTerms(m.query))...)
	}
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
	m.cursor += delta
	m.clampCursor()
	return m.updatePreview()
}

// ---- geometry ----

const (
	previewRowsPct    = 70
	previewColumnsPct = 75
)

// minColumnsW is the narrowest terminal the side-by-side layout is used on;
// below it the list share would not even fit the hash and date.
const minColumnsW = 60

// columns reports the effective layout: the columns setting falls back to
// rows on a narrow terminal, without touching the saved setting.
func (m *model) columns() bool { return m.prefs.layout == layoutColumns && m.width >= minColumnsW }

// bodyH is what is left between the input line and the two footer lines.
func (m *model) bodyH() int { return max(3, m.height-3) }

func (m *model) listH() int {
	if m.columns() {
		return m.bodyH()
	}
	return max(1, m.bodyH()-m.previewBlockH())
}

// previewBlockH is the rows-layout preview height, label line included.
func (m *model) previewBlockH() int {
	return min(max(2, m.bodyH()*previewRowsPct/100), m.bodyH()-1)
}

func (m *model) listW() int {
	if m.columns() {
		return max(10, m.width-m.prevW()-3)
	}
	return m.width
}

func (m *model) prevW() int {
	if m.columns() {
		return max(10, m.width*previewColumnsPct/100)
	}
	return max(10, m.width)
}

func (m *model) resize() {
	m.prevVP.SetWidth(m.prevW())
	if m.columns() {
		m.prevVP.SetHeight(max(1, m.bodyH()-1))
	} else {
		m.prevVP.SetHeight(max(1, m.previewBlockH()-1))
	}
	m.fullVP.SetWidth(max(10, m.width))
	m.fullVP.SetHeight(max(1, m.height-2))
	m.help.SetWidth(m.width)
	m.clampCursor()
}

// ---- preview ----

func (m *model) activeVP() *viewport.Model {
	if m.mode == modeFull || m.mode == modeSearch {
		return &m.fullVP
	}
	return &m.prevVP
}

// updatePreview points the active viewport at the selected commit: from the
// render cache when possible, otherwise the instant header plus a placeholder
// while delta runs. Only one render is in flight at a time; a selection that
// moved on cancels it and the newest one starts when the cancelled render
// reports back, so holding an arrow key never piles up delta processes.
func (m *model) updatePreview() tea.Cmd {
	if !m.sized {
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
	k := previewKey(c.hash, w, m.prefs.diff)
	if k != m.wantKey {
		m.wantKey = k
		m.renderErr = ""
		m.clearSearch()
		vp.GotoTop()
	}
	if content, ok := m.renders[k]; ok {
		if m.shownKey != k {
			m.shownKey = k
			vp.SetContent(content)
		}
		return nil
	}
	m.shownKey = ""
	var d *detail
	if known, ok := m.details[c.hash]; ok {
		d = &known
	}
	vp.SetContent(previewHeader(c, d, w) + "\n\n" + stDim.Render("rendering…"))
	if m.inflight == k {
		return nil
	}
	if m.inflight != "" {
		m.cancelRender()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.inflight, m.cancelRender = k, cancel
	return renderPreviewCmd(ctx, *c, w, m.prefs.diff, m.deltaBin)
}

// maxRenders bounds the render cache; it is simply dropped when full (the
// current commit re-renders in milliseconds).
const maxRenders = 128

func (m *model) handlePreview(msg previewMsg) tea.Cmd {
	if msg.key == m.inflight {
		m.inflight = ""
		m.cancelRender()
	}
	switch {
	case msg.cancelled:
		// Fall through to updatePreview, which starts the render wanted now
		// (possibly this same one again, after an A, B, A selection).
	case msg.err == nil:
		if len(m.renders) >= maxRenders {
			m.renders = map[string]string{}
			m.details = map[string]detail{}
		}
		m.renders[msg.key] = msg.content
		m.details[msg.hash] = msg.detail
	case msg.key == m.wantKey:
		// A failure of the render still wanted (not a cancelled one).
		m.renderErr = msg.err.Error()
		m.shownKey = msg.key
		c := m.current()
		m.activeVP().SetContent(previewHeader(c, nil, m.activeVP().Width()) + "\n\n" + stError.Render(m.renderErr))
		return nil
	}
	return m.updatePreview()
}

// resetPreview forces the active viewport to be filled again (mode switch:
// the two viewports can share a key when they are equally wide).
func (m *model) resetPreview() tea.Cmd {
	m.wantKey, m.shownKey = "", ""
	return m.updatePreview()
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
	content, ok := m.renders[m.wantKey]
	if term == "" || !ok {
		if ok {
			m.fullVP.SetContent(content)
			m.shownKey = m.wantKey
		}
		return
	}
	m.searchTerm = term
	needle := strings.ToLower(term)
	lines := strings.Split(content, "\n")
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

// ---- settings ----

func (m *model) toggleLayout() tea.Cmd {
	if m.columns() {
		m.prefs.layout = layoutRows
	} else {
		m.prefs.layout = layoutColumns
	}
	savePref("layout", m.prefs.layout)
	m.resize()
	return m.updatePreview()
}

func (m *model) toggleDiffMode() tea.Cmd {
	if m.prefs.diff == diffSBS {
		m.prefs.diff = diffSingle
	} else {
		m.prefs.diff = diffSBS
	}
	savePref("diff", m.prefs.diff)
	return m.updatePreview()
}

// ---- bubbletea ----

func waitLog(ch <-chan logBatch) tea.Cmd {
	return func() tea.Msg {
		b, ok := <-ch
		if !ok {
			return logBatch{done: true}
		}
		return b
	}
}

func (m *model) Init() tea.Cmd {
	if m.mode == modeFatal {
		return nil
	}
	cmds := []tea.Cmd{textinput.Blink, func() tea.Msg { return repoInfoMsg(loadRepoInfo()) }}
	if m.logCh != nil {
		cmds = append(cmds, waitLog(m.logCh))
	}
	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height, m.sized = msg.Width, msg.Height, true
		m.resize()
		return m, m.updatePreview()

	case repoInfoMsg:
		m.info = repoInfo(msg).String()
		return m, nil

	case logBatch:
		first := len(m.commits) == 0
		m.addCommits(msg.commits)
		if msg.err != nil {
			m.logErr = msg.err.Error()
		}
		var cmds []tea.Cmd
		if msg.done {
			m.loading = false
		} else {
			cmds = append(cmds, waitLog(m.logCh))
		}
		if first || m.current() != nil && m.wantKey == "" {
			m.clampCursor()
			cmds = append(cmds, m.updatePreview())
		}
		return m, tea.Batch(cmds...)

	case previewMsg:
		return m, m.handlePreview(msg)

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseWheelMsg:
		// The wheel always scrolls the diff, wherever the pointer is; the list
		// is driven by the keys and by clicking a row.
		if m.mode == modeList || m.mode == modeFull {
			vp := m.activeVP()
			*vp, _ = vp.Update(msg)
		}
		return m, nil

	case tea.MouseClickMsg:
		return m, m.handleClick(msg)
	}

	var cmd tea.Cmd
	if m.mode == modeSearch {
		m.si, cmd = m.si.Update(msg)
	} else {
		m.ti, cmd = m.ti.Update(msg)
	}
	return m, cmd
}

func (m *model) quit() tea.Cmd {
	if m.stopLog != nil {
		m.stopLog()
	}
	if m.cancelRender != nil {
		m.cancelRender()
	}
	return tea.Quit
}

func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeFatal:
		return m, tea.Quit
	case modeFull:
		return m, m.handleFullKey(msg)
	case modeSearch:
		return m, m.handleSearchKey(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, m.quit()
	case key.Matches(msg, m.keys.Open):
		if m.current() == nil {
			return m, nil
		}
		m.mode = modeFull
		m.ti.Blur()
		return m, m.resetPreview()
	case key.Matches(msg, m.keys.Layout):
		return m, m.toggleLayout()
	case key.Matches(msg, m.keys.DiffMode):
		return m, m.toggleDiffMode()
	case key.Matches(msg, m.keys.Up):
		return m, m.moveCursor(-1)
	case key.Matches(msg, m.keys.Down):
		return m, m.moveCursor(+1)
	case key.Matches(msg, m.keys.PageUp):
		return m, m.moveCursor(-m.listH())
	case key.Matches(msg, m.keys.PageDown):
		return m, m.moveCursor(+m.listH())
	case key.Matches(msg, m.keys.PrevUp):
		m.prevVP.ScrollUp(3)
		return m, nil
	case key.Matches(msg, m.keys.PrevDown):
		m.prevVP.ScrollDown(3)
		return m, nil
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
		return tea.Batch(m.ti.Focus(), m.resetPreview())
	case key.Matches(msg, m.fullKeys.DiffMode):
		return m.toggleDiffMode()
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

// handleClick moves the selection to the list row under a left click. It
// never opens the full diff: that stays on enter. Screen row 0 is the input;
// the list starts on row 1.
func (m *model) handleClick(msg tea.MouseClickMsg) tea.Cmd {
	if m.mode != modeList || msg.Button != tea.MouseLeft {
		return nil
	}
	if msg.Y < 1 || msg.Y > m.listH() || msg.X >= m.listW() {
		return nil
	}
	i := m.top + msg.Y - 1
	if i >= m.rowCount() || i == m.cursor {
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
	v := tea.NewView(s)
	v.AltScreen = true
	if m.mode == modeList || m.mode == modeFull {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func (m *model) listView() string {
	var body string
	if m.columns() {
		sep := stDim.Render(strings.TrimRight(strings.Repeat("│\n", m.bodyH()), "\n"))
		right := m.labelRule(m.prevW()) + "\n" + m.prevVP.View()
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.listBlock(), " ", sep, " ", right)
	} else {
		body = m.listBlock() + "\n" + m.labelRule(m.width) + "\n" + m.prevVP.View()
	}
	return m.inputLine() + "\n" + body + "\n" + m.infoLine() + "\n" + m.helpLine(m.keys)
}

// inputLine is the filter input with the match counter on the right.
func (m *model) inputLine() string {
	count := strconv.Itoa(m.rowCount())
	if m.filtering() {
		count += "/" + strconv.Itoa(len(m.commits))
	}
	if m.loading {
		count += " loading…"
	}
	left := m.ti.View()
	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(count)
	if gap < 1 {
		return truncate(left, m.width)
	}
	return left + strings.Repeat(" ", gap) + stDim.Render(count)
}

func (m *model) listBlock() string {
	h, w := m.listH(), m.listW()
	lines := make([]string, 0, h)
	for i := m.top; i < m.top+h && i < m.rowCount(); i++ {
		c, matched := m.rowAt(i)
		sel := i == m.cursor
		lines = append(lines, renderSegs(rowSegs(c, w, m.hashW, m.columns(), sel), matched, sel))
	}
	if len(lines) == 0 {
		msg := ""
		switch {
		case m.logErr != "":
			msg = stError.Render(truncate(m.logErr, w))
		case m.filtering():
			msg = stDim.Render("no matching commits")
		case !m.loading:
			msg = stDim.Render("no commits")
		}
		lines = append(lines, msg)
	}
	blank := strings.Repeat(" ", w)
	for len(lines) < h {
		lines = append(lines, blank)
	}
	return strings.Join(lines, "\n")
}

// labelRule is the line above the preview carrying the diff mode, the
// equivalent of fzf's preview label.
func (m *model) labelRule(w int) string {
	label := " " + diffLabel(m.prefs.diff) + " "
	if m.deltaBin == "" {
		label = " delta not found: plain git colors "
	}
	left := 2
	right := max(0, w-left-ansi.StringWidth(label))
	return truncate(stDim.Render(strings.Repeat("─", left))+stLabel.Render(label)+stDim.Render(strings.Repeat("─", right)), w)
}

// helpLine truncates the help itself: bubbles' help keeps appending items
// past its width when the ellipsis does not fit.
func (m *model) helpLine(keys help.KeyMap) string {
	return truncate(m.help.View(keys), m.width)
}

func (m *model) infoLine() string {
	return truncate(stDim.Render(m.info), m.width)
}

func (m *model) fullView() string {
	title := ""
	if c := m.current(); c != nil {
		title = stHash.Render(c.short()) + " " + truncate(c.subject(), max(10, m.width/2))
	}
	pos := strconv.Itoa(int(m.fullVP.ScrollPercent()*100)) + "%"
	head := title + "  " + stLabel.Render(diffLabel(m.prefs.diff)) + "  " + stDim.Render(pos)
	if m.searchTerm != "" {
		found := "no matches"
		if n := len(m.matchLines); n > 0 {
			found = "match " + strconv.Itoa(m.matchIdx+1) + "/" + strconv.Itoa(n)
		}
		head += "  " + stDim.Render("/"+m.searchTerm+" ("+found+")")
	}
	foot := m.helpLine(m.fullKeys)
	if m.mode == modeSearch {
		foot = m.si.View()
	}
	return truncate(head, m.width) + "\n" + m.fullVP.View() + "\n" + foot
}
