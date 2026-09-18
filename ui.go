package main

// The bubbletea model: a filter input on top, a two-column body (list left,
// preview right) and a help footer, in two views. View 1 lists the worktree
// groups and previews the files of the one under the cursor; view 2 lists
// the files of the chosen group and previews the one under the cursor.
// Modeled on gotopr: the input is focused before the program starts, every
// printable key filters, and the chosen files are handed to Zed AFTER the
// TUI exits (quitting is what closes the popup).

import (
	"fmt"
	"os"
	"os/exec"
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

func truncate(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}

// padRight truncates s to width and pads it with spaces up to width.
func padRight(s string, width int) string {
	s = truncate(s, width)
	if n := width - ansi.StringWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

// padLeft right-aligns s in width.
func padLeft(s string, width int) string {
	s = truncate(s, width)
	if n := width - ansi.StringWidth(s); n > 0 {
		s = strings.Repeat(" ", n) + s
	}
	return s
}

// ---- styles ----

var (
	stPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	stDev    = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	stSel    = lipgloss.NewStyle().Background(lipgloss.Color("8")).Bold(true)
	stMatch  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	stHeader = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stTitle  = lipgloss.NewStyle().Bold(true)
	stRepo   = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	stError  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	stMark   = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)

	// file statuses
	stGone      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	stPlan      = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	stTracked   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	stUntracked = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

func statusStyle(status string) lipgloss.Style {
	switch status {
	case statusGone:
		return stGone
	case statusPlan:
		return stPlan
	case statusTracked:
		return stTracked
	case statusUntracked:
		return stUntracked
	}
	return lipgloss.NewStyle()
}

// ---- key bindings ----

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Files    key.Binding // view 1: enter the group
	Open     key.Binding // view 2: open in Zed
	Mark     key.Binding
	OpenAll  key.Binding
	Toggle   key.Binding
	Back     key.Binding
	Quit     key.Binding
	PrevUp   key.Binding
	PrevDown key.Binding
	Filter   key.Binding

	files bool // which view the short help describes
}

func (k keyMap) ShortHelp() []key.Binding {
	if k.files {
		return []key.Binding{k.Open, k.Mark, k.OpenAll, k.Toggle, k.PrevDown, k.Back}
	}
	return []key.Binding{k.Filter, k.Files, k.Toggle, k.PrevDown, k.Quit}
}
func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

func defaultKeys() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑/^p", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓/^n", "down")),
		Files:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "files")),
		Open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open in Zed")),
		Mark:     key.NewBinding(key.WithKeys("tab", "space", "shift+tab"), key.WithHelp("tab", "multi")),
		OpenAll:  key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("^o", "open all")),
		Toggle:   key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("^a", "all files")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc/q", "quit")),
		PrevUp:   key.NewBinding(key.WithKeys("shift+up", "pgup"), key.WithHelp("⇧↑", "")),
		PrevDown: key.NewBinding(key.WithKeys("shift+down", "pgdown"), key.WithHelp("⇧↓", "scroll preview")),
		// Help-only entry: a binding without keys is disabled and the help
		// bubble would skip it. Nothing ever matches against it.
		Filter: key.NewBinding(key.WithKeys("type"), key.WithHelp("type", "filter")),
	}
}

// ---- model ----

type viewKind int

const (
	viewGroups viewKind = iota
	viewFiles
)

type model struct {
	// data
	groups   []*group // every indexed root, statuses resolved
	loadErr  string   // the index could not be read
	allFiles bool     // false: notes only (default); true: every indexed file
	home     string
	now      time.Time

	// view 1
	gRows   []groupRow
	gCursor int

	// view 2
	cur        *group // group being browsed
	fRows      []fileRow
	fCursor    int
	selected   map[string]bool // multi-select, by path
	groupQuery string          // view 1 query, restored on esc

	// ui
	view   viewKind
	notice string // transient footer message, cleared by the next key
	ti     textinput.Model
	listVP viewport.Model
	prevVP viewport.Model
	help   help.Model
	keys   keyMap
	width  int
	height int

	// preview render cache
	renders map[string]string
	prevKey string

	// previewStyle is the glamour standard style ("dark"/"light"). It starts
	// as "dark" and flips when the terminal answers RequestBackgroundColor.
	previewStyle string

	open    []string // files to hand to Zed after quit (nil = none)
	skipped int      // chosen files that no longer exist
}

func newModel(groups []*group, loadErr string) model {
	m := model{
		groups:       groups,
		loadErr:      loadErr,
		home:         homeDir(),
		now:          time.Now(),
		selected:     map[string]bool{},
		ti:           newFilterInput(),
		listVP:       viewport.New(viewport.WithWidth(50), viewport.WithHeight(20)),
		prevVP:       viewport.New(viewport.WithWidth(40), viewport.WithHeight(20)),
		help:         help.New(),
		keys:         defaultKeys(),
		renders:      map[string]string{},
		previewStyle: "dark",
		width:        94,
		height:       24,
	}
	m.applyFilter()
	m.resize()
	m.renderList()
	m.updatePreview() // view 1 previews are synchronous: the first frame is complete
	return m
}

// newFilterInput builds the focused filter textinput with the gotonotes
// prompt. The prompt string already carries its colors, so the prompt style
// is left empty.
func newFilterInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = promptText()
	st := ti.Styles()
	st.Focused.Prompt = lipgloss.NewStyle()
	st.Blurred.Prompt = lipgloss.NewStyle()
	ti.SetStyles(st)
	ti.Focus()
	return ti
}

// promptText builds the textinput prompt, with an orange "(dev)" marker on
// non-release builds.
func promptText() string {
	if strings.HasPrefix(version, "v") {
		return stPrompt.Render("gotonotes ❯ ")
	}
	return stPrompt.Render("gotonotes (") + stDev.Render("dev") + stPrompt.Render(") ❯ ")
}

func (m *model) currentGroup() *group {
	if m.gCursor >= 0 && m.gCursor < len(m.gRows) {
		return m.gRows[m.gCursor].g
	}
	return nil
}

func (m *model) currentFile() *noteFile {
	if m.fCursor >= 0 && m.fCursor < len(m.fRows) {
		return &m.fRows[m.fCursor].f
	}
	return nil
}

// ---- layout ----

func (m *model) listW() int {
	w := (m.width - 3) / 2
	if w < 20 {
		w = 20
	}
	return w
}

func (m *model) prevW() int {
	w := m.width - m.listW() - 3
	if w < 10 {
		w = 10
	}
	return w
}

// listTop is the screen row where the list starts: below the input, and in
// view 2 also below the group header.
func (m *model) listTop() int {
	if m.view == viewFiles {
		return 2
	}
	return 1
}

func (m *model) bodyH() int {
	h := m.height - m.listTop() - 1 // footer
	if h < 1 {
		h = 1
	}
	return h
}

func (m *model) resize() {
	m.listVP.SetWidth(m.listW())
	m.listVP.SetHeight(m.bodyH())
	m.prevVP.SetWidth(m.prevW())
	m.prevVP.SetHeight(m.bodyH())
	m.help.SetWidth(m.width)
}

// ---- filtering ----

func (m *model) applyFilter() {
	q := m.ti.Value()
	if m.view == viewFiles {
		m.fRows = filterFiles(m.cur.visibleFiles(m.allFiles), q, m.home)
		m.fCursor = 0
		if q != "" {
			m.fCursor = bestIndex(len(m.fRows), func(i int) int { return m.fRows[i].score })
		}
		if len(m.fRows) == 0 {
			m.fCursor = -1
		}
		return
	}
	m.gRows = filterGroups(visibleGroups(m.groups, m.allFiles), q, m.home)
	m.gCursor = 0
	if q != "" {
		m.gCursor = bestIndex(len(m.gRows), func(i int) int { return m.gRows[i].score })
	}
	if len(m.gRows) == 0 {
		m.gCursor = -1
	}
}

func (m *model) keepCursorOnGroup(root string) {
	for i, r := range m.gRows {
		if r.g.root == root {
			m.gCursor = i
			return
		}
	}
}

func (m *model) keepCursorOnFile(path string) {
	for i, r := range m.fRows {
		if r.f.path == path {
			m.fCursor = i
			return
		}
	}
}

// refilter re-applies the query after a keystroke or a mode toggle. An empty
// query means nothing is being searched for, so the cursor stays on the row
// it was on instead of jumping to the top.
func (m *model) refilter() {
	var root, path string
	if g := m.currentGroup(); g != nil {
		root = g.root
	}
	if f := m.currentFile(); f != nil {
		path = f.path
	}
	m.applyFilter()
	if m.ti.Value() == "" {
		if m.view == viewFiles {
			m.keepCursorOnFile(path)
		} else {
			m.keepCursorOnGroup(root)
		}
	}
}

// ---- view switching ----

func (m *model) enterGroup(g *group) {
	m.cur = g
	m.view = viewFiles
	m.groupQuery = m.ti.Value()
	m.ti.SetValue("")
	m.selected = map[string]bool{}
	m.keys.files = true
	m.applyFilter()
	m.resize()
}

func (m *model) backToGroups() {
	root := m.cur.root
	m.view = viewGroups
	m.cur = nil
	m.fRows = nil
	m.ti.SetValue(m.groupQuery)
	m.ti.CursorEnd()
	m.keys.files = false
	m.applyFilter()
	m.keepCursorOnGroup(root)
	m.resize()
}

func (m *model) toggleAll() {
	m.allFiles = !m.allFiles
	label := "all files"
	if m.allFiles {
		label = "notes only"
	}
	m.keys.Toggle.SetHelp("^a", label)
	m.refilter()
}

// ---- list rendering ----

const (
	countColW = 10 // " 123 notes"
	ageColW   = 4
)

func (m *model) renderList() {
	w := m.listW()
	var b strings.Builder
	if m.view == viewFiles {
		for i, r := range m.fRows {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(m.fileLine(r, i == m.fCursor, true, w))
		}
	} else {
		for i, r := range m.gRows {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(m.groupLine(r, i == m.gCursor, w))
		}
	}
	m.listVP.SetContent(b.String())
	m.ensureVisible()
}

// groupLine renders one worktree row in fixed columns: label, repo, count,
// age. The selected row is padded to the full width before styling so its
// background spans the whole column.
func (m *model) groupLine(r groupRow, selected bool, width int) string {
	repoW := 18
	if width < 60 {
		repoW = 12
	}
	labelW := width - 2 - 1 - repoW - 1 - countColW - 1 - ageColW
	if labelW < 8 {
		labelW = 8
	}
	repo := r.g.repo
	if repo == "" {
		repo = "-"
	}
	count := padLeft(countLabel(r.g.count(m.allFiles), m.allFiles), countColW)
	age := padLeft(compactAge(r.g.last, m.now), ageColW)
	if selected {
		line := "▌ " + padRight(r.g.label, labelW) + " " + padRight(repo, repoW) + " " + count + " " + age
		return stSel.Render(padRight(line, width))
	}
	label := padRight(highlight(r.g.label, r.labelIdx, stTitle), labelW)
	repoCol := padRight(highlight(repo, r.repoIdx, stRepo), repoW)
	return truncate("  "+label+" "+repoCol+" "+count+" "+stDim.Render(age), width)
}

// fileLine renders one file row: status, last write, path. cursorCol adds
// the two-cell gutter (cursor bar, multi-select mark) of view 2; the view 1
// preview reuses the row without it.
func (m *model) fileLine(r fileRow, selected, cursorCol bool, width int) string {
	const statusW, dateW = 9, 11
	gutter := ""
	if cursorCol {
		mark := " "
		if m.selected[r.f.path] {
			mark = "●"
		}
		if selected {
			gutter = "▌" + mark
		} else {
			gutter = " " + stMark.Render(mark)
		}
	}
	pathW := width - ansi.StringWidth(gutter) - statusW - 1 - dateW - 2
	if pathW < 8 {
		pathW = 8
	}
	display := tildePath(r.f.path, m.home)
	dim := 0
	if m.cur != nil && insideRoot(m.cur.root, r.f.path) {
		dim = len(tildePath(m.cur.root, m.home)) + 1
	} else if m.cur == nil {
		if g := m.currentGroup(); g != nil && insideRoot(g.root, r.f.path) {
			dim = len(tildePath(g.root, m.home)) + 1
		}
	}
	date := r.f.last.Format("02/01 15:04")
	if selected {
		line := gutter + padRight(r.f.status, statusW) + " " + date + "  " + pathCells(display, 0, nil, pathW, false)
		return stSel.Render(padRight(line, width))
	}
	status := statusStyle(r.f.status).Render(padRight(r.f.status, statusW))
	return truncate(gutter+status+" "+stDim.Render(date)+"  "+pathCells(display, dim, r.idx, pathW, true), width)
}

// pathCells fits a path into width. A path that does not fit loses its
// head, not its tail, so the file name is always visible. The first dim
// bytes (the worktree root prefix) are dimmed and the matched bytes
// highlighted when styled is set.
func pathCells(path string, dim int, idx []int, width int, styled bool) string {
	type cell struct {
		r   rune
		off int
	}
	cells := make([]cell, 0, len(path))
	for off, r := range path {
		cells = append(cells, cell{r, off})
	}
	cut := false
	if len(cells) > width && width > 1 {
		cells = cells[len(cells)-(width-1):]
		cut = true
	}
	if !styled {
		var b strings.Builder
		if cut {
			b.WriteString("…")
		}
		for _, c := range cells {
			b.WriteRune(c.r)
		}
		return b.String()
	}
	matched := make(map[int]bool, len(idx))
	for _, i := range idx {
		matched[i] = true
	}
	var b, run strings.Builder
	runDim := false
	flush := func() {
		if run.Len() == 0 {
			return
		}
		if runDim {
			b.WriteString(stDim.Render(run.String()))
		} else {
			b.WriteString(run.String())
		}
		run.Reset()
	}
	if cut {
		b.WriteString(stDim.Render("…"))
	}
	for _, c := range cells {
		if matched[c.off] {
			flush()
			b.WriteString(stMatch.Render(string(c.r)))
			continue
		}
		if d := c.off < dim; d != runDim {
			flush()
			runDim = d
		}
		run.WriteRune(c.r)
	}
	flush()
	return b.String()
}

func (m *model) cursor() int {
	if m.view == viewFiles {
		return m.fCursor
	}
	return m.gCursor
}

func (m *model) rowCount() int {
	if m.view == viewFiles {
		return len(m.fRows)
	}
	return len(m.gRows)
}

func (m *model) setCursor(i int) {
	if i < 0 || i >= m.rowCount() {
		return
	}
	if m.view == viewFiles {
		m.fCursor = i
	} else {
		m.gCursor = i
	}
}

func (m *model) ensureVisible() {
	h, c := m.listVP.Height(), m.cursor()
	if h <= 0 || c < 0 {
		m.listVP.SetYOffset(0)
		return
	}
	if c < m.listVP.YOffset() {
		m.listVP.SetYOffset(c)
	} else if c >= m.listVP.YOffset()+h {
		m.listVP.SetYOffset(c - h + 1)
	}
}

// ---- preview ----

// updatePreview refreshes the right column: the group's files in view 1
// (instant), the file under the cursor in view 2 (rendered off the update
// loop and cached).
func (m *model) updatePreview() tea.Cmd {
	if m.view == viewGroups {
		g := m.currentGroup()
		if g == nil {
			m.prevKey = ""
			m.prevVP.SetContent("")
			return nil
		}
		key := fmt.Sprintf("group|%s|%t|%d", g.root, m.allFiles, m.prevW())
		if key == m.prevKey {
			return nil
		}
		m.prevKey = key
		m.prevVP.GotoTop()
		var b strings.Builder
		for i, f := range g.visibleFiles(m.allFiles) {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(m.fileLine(fileRow{f: f}, false, false, m.prevW()))
		}
		m.prevVP.SetContent(b.String())
		return nil
	}

	f := m.currentFile()
	if f == nil {
		m.prevKey = ""
		m.prevVP.SetContent("")
		return nil
	}
	key := previewKey(f.path, m.prevW())
	if key == m.prevKey {
		return nil
	}
	m.prevKey = key
	m.prevVP.GotoTop()
	if c, ok := m.renders[key]; ok {
		m.prevVP.SetContent(c)
		return nil
	}
	m.prevVP.SetContent(stDim.Render("rendering…"))
	return renderPreviewCmd(f.path, key, m.prevW(), m.previewStyle)
}

// setPreviewStyle switches the glamour style once the terminal background is
// known. Cached renders carry the old palette, so they are dropped and the
// current preview is rendered again.
func (m *model) setPreviewStyle(style string) tea.Cmd {
	if style == m.previewStyle {
		return nil
	}
	m.previewStyle = style
	m.renders = map[string]string{}
	m.prevKey = ""
	return m.updatePreview()
}

// ---- opening ----

// chosenFiles is what enter opens: the multi-selection in list order (even
// when the filter hides part of it), or the file under the cursor.
func (m *model) chosenFiles() []string {
	if len(m.selected) > 0 {
		var out []string
		for _, f := range m.cur.files {
			if m.selected[f.path] {
				out = append(out, f.path)
			}
		}
		return out
	}
	if f := m.currentFile(); f != nil {
		return []string{f.path}
	}
	return nil
}

// listedFiles is what ctrl+o opens: every row currently listed.
func (m *model) listedFiles() []string {
	out := make([]string, 0, len(m.fRows))
	for _, r := range m.fRows {
		out = append(out, r.f.path)
	}
	return out
}

// queueOpen keeps the files that still exist and quits so they can be opened
// after the TUI is gone. With nothing left to open, or no Zed to open it
// with, it stays in the popup and says why: anything printed after the popup
// closes is lost.
func (m *model) queueOpen(paths []string) tea.Cmd {
	var existing []string
	for _, p := range paths {
		if fileExists(p) {
			existing = append(existing, p)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	if len(existing) == 0 {
		m.notice = "nothing to open: the file no longer exists"
		if len(paths) > 1 {
			m.notice = "nothing to open: the files no longer exist"
		}
		return nil
	}
	if _, err := openerPath(); err != nil {
		m.notice = err.Error()
		return nil
	}
	m.open = existing
	m.skipped = len(paths) - len(existing)
	return tea.Quit
}

// openerPath resolves the editor CLI. GOTONOTES_OPENER replaces it (the pty
// driver points it at a logging stub). The popup may run with a shorter
// PATH than an interactive shell, so the usual install locations of the zed
// CLI are tried as well.
func openerPath() (string, error) {
	if b := os.Getenv("GOTONOTES_OPENER"); b != "" {
		return b, nil
	}
	if p, err := exec.LookPath("zed"); err == nil {
		return p, nil
	}
	for _, p := range []string{
		"/usr/local/bin/zed",
		"/opt/homebrew/bin/zed",
		homeDir() + "/.local/bin/zed",
		"/Applications/Zed.app/Contents/MacOS/cli",
	} {
		if fileExists(p) {
			return p, nil
		}
	}
	return "", fmt.Errorf("zed CLI not found (install it from Zed: cli: install)")
}

// runOpen hands the chosen files to Zed in one new window. It runs after the
// TUI exits; the only output is a one-line note about skipped files.
func runOpen(files []string, skipped int) {
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "gotonotes: skipped %d file(s) that no longer exist\n", skipped)
	}
	if len(files) == 0 {
		return
	}
	bin, err := openerPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "gotonotes:", err)
		return
	}
	if err := exec.Command(bin, append([]string{"-n"}, files...)...).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gotonotes:", err)
	}
}

// ---- bubbletea ----

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, tea.RequestBackgroundColor)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		m.renderList()
		return m, m.updatePreview()

	case tea.BackgroundColorMsg:
		style := "dark"
		if !msg.IsDark() {
			style = "light"
		}
		return m, m.setPreviewStyle(style)

	case previewMsg:
		if msg.style != m.previewStyle { // rendered before the style flipped
			return m, nil
		}
		m.renders[msg.key] = msg.content
		if msg.key == m.prevKey {
			m.prevVP.SetContent(msg.content)
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseWheelMsg:
		// The wheel always scrolls the preview, wherever the pointer is; the
		// list is driven by the keys and by clicking a row (see gotopr).
		m.prevVP, _ = m.prevVP.Update(msg)
		return m, nil

	case tea.MouseClickMsg:
		return m.handleClick(msg)

	default:
		var cmd tea.Cmd
		m.ti, cmd = m.ti.Update(msg)
		return m, cmd
	}
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.notice = ""
	switch {
	case msg.String() == "ctrl+c":
		return m, tea.Quit
	case msg.String() == "q" && m.ti.Value() == "":
		// q quits only while the filter is empty; otherwise it is text.
		return m, tea.Quit
	case key.Matches(msg, m.keys.Back):
		if m.view == viewGroups {
			return m, tea.Quit
		}
		m.backToGroups()
		m.renderList()
		return m, m.updatePreview()
	case key.Matches(msg, m.keys.Toggle):
		m.toggleAll()
		m.renderList()
		return m, m.updatePreview()
	case key.Matches(msg, m.keys.Up):
		m.setCursor(m.cursor() - 1)
		m.renderList()
		return m, m.updatePreview()
	case key.Matches(msg, m.keys.Down):
		m.setCursor(m.cursor() + 1)
		m.renderList()
		return m, m.updatePreview()
	case key.Matches(msg, m.keys.PrevUp):
		m.prevVP.ScrollUp(3)
		return m, nil
	case key.Matches(msg, m.keys.PrevDown):
		m.prevVP.ScrollDown(3)
		return m, nil
	}

	if m.view == viewGroups {
		if key.Matches(msg, m.keys.Files) {
			if g := m.currentGroup(); g != nil {
				m.enterGroup(g)
				m.renderList()
				return m, m.updatePreview()
			}
			return m, nil
		}
	} else {
		switch {
		case key.Matches(msg, m.keys.Open):
			return m, m.queueOpen(m.chosenFiles())
		case key.Matches(msg, m.keys.OpenAll):
			return m, m.queueOpen(m.listedFiles())
		case key.Matches(msg, m.keys.Mark):
			if f := m.currentFile(); f != nil {
				if m.selected[f.path] {
					delete(m.selected, f.path)
				} else {
					m.selected[f.path] = true
				}
				switch msg.String() {
				case "tab":
					m.setCursor(m.fCursor + 1)
				case "shift+tab":
					m.setCursor(m.fCursor - 1)
				}
			}
			m.renderList()
			return m, m.updatePreview()
		}
	}

	before := m.ti.Value()
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	if m.ti.Value() != before {
		m.refilter()
		m.renderList()
	}
	return m, tea.Batch(cmd, m.updatePreview())
}

// handleClick moves the cursor to the row under a left click on the list. It
// never opens anything: that stays on enter.
func (m model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || msg.X >= m.listW()+2 || msg.Y < m.listTop() {
		return m, nil
	}
	i := msg.Y - m.listTop() + m.listVP.YOffset()
	if i < 0 || i >= m.rowCount() || i == m.cursor() {
		return m, nil
	}
	m.setCursor(i)
	m.renderList()
	return m, m.updatePreview()
}

func (m model) View() tea.View {
	sep := stDim.Render(strings.TrimRight(strings.Repeat("│\n", m.bodyH()), "\n"))
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.leftColumn(), " ", sep, " ", m.prevVP.View())
	top := m.ti.View()
	if m.view == viewFiles {
		top = m.groupHeader() + "\n" + top
	}
	v := tea.NewView(top + "\n" + body + "\n" + m.footer())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// leftColumn is the list, or the reason there is nothing to list.
func (m model) leftColumn() string {
	if m.rowCount() > 0 {
		return m.listVP.View()
	}
	msg := "No matches"
	switch {
	case m.loadErr != "":
		msg = m.loadErr
	case m.ti.Value() != "":
	case m.allFiles:
		msg = "Nothing indexed yet"
	default:
		msg = "No notes (ctrl+a: all files)"
	}
	return lipgloss.NewStyle().Width(m.listW()).Height(m.bodyH()).
		Render(stDim.Render(truncate(msg, m.listW())))
}

// groupHeader is the first line of view 2: label · root · count.
func (m model) groupHeader() string {
	dot := stDim.Render(" · ")
	h := stHeader.Render(m.cur.label) + dot + stDim.Render(tildePath(m.cur.root, m.home)) +
		dot + countLabel(m.cur.count(m.allFiles), m.allFiles)
	if n := len(m.selected); n > 0 {
		h += dot + stMark.Render(fmt.Sprintf("%d selected", n))
	}
	return truncate(h, m.width)
}

func (m model) footer() string {
	f := m.help.View(m.keys)
	if m.notice != "" {
		f += "  " + stError.Render(truncate(m.notice, m.width/2))
	}
	return f
}
