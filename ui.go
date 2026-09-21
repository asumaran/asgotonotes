package main

// The bubbletea model: one frame (see frame.go) holding a context line, the
// filter input, the list next to the preview, and the help, in two views. View 1 lists the worktree
// groups and previews the files of the one under the cursor; view 2 lists
// the files of the chosen group and previews the one under the cursor.
// Modeled on asgotopr: the input is focused before the program starts, every
// printable key filters, and the chosen files are handed to Zed AFTER the
// TUI exits (quitting is what closes the popup).

import (
	"fmt"
	"os"
	"os/exec"
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
	stHeader = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stTitle  = lipgloss.NewStyle().Bold(true)
	stRepo   = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	stError  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	stMark   = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	stCount  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

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
	Nav      listNav
	Files    key.Binding // view 1: enter the group
	Open     key.Binding // view 2: open in Zed
	Mark     key.Binding
	OpenAll  key.Binding
	Toggle   key.Binding
	Copy     key.Binding
	Back     key.Binding
	Quit     key.Binding
	PrevUp   key.Binding
	PrevDown key.Binding
	Shrink   key.Binding
	Grow     key.Binding
	Filter   key.Binding
	Help     key.Binding

	files bool // which view the short help describes
}

// ShortHelp is the folded help line: the view's own actions, the help and the
// quit keys. Moving, scrolling and resizing are in the expanded help, so the
// line stays short enough for a narrow popup (a cut line loses the quit keys
// first).
func (k keyMap) ShortHelp() []key.Binding {
	if k.files {
		return []key.Binding{k.Open, k.Mark, k.OpenAll, k.Toggle, k.Help, k.Back}
	}
	return []key.Binding{k.Filter, k.Files, k.Toggle, k.Help, k.Quit}
}

// FullHelp is the panel's list of keys, one column per group: the
// filter and the preview, the list, the view's actions, help and quit.
func (k keyMap) FullHelp() [][]key.Binding {
	actions, leave := []key.Binding{k.Files, k.Toggle, k.Copy}, k.Quit
	if k.files {
		actions, leave = []key.Binding{k.Open, k.Mark, k.OpenAll, k.Toggle, k.Copy}, k.Back
	}
	return [][]key.Binding{
		{k.Filter, k.PrevUp, k.Shrink},
		{k.Nav.Up, k.Nav.PageUp, k.Nav.Top},
		actions,
		{k.Help, leave},
	}
}

func defaultKeys() keyMap {
	return keyMap{
		Nav:      defaultListNav(),
		Files:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "files")),
		Open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open in Zed")),
		Mark:     key.NewBinding(key.WithKeys("tab", "space", "shift+tab"), key.WithHelp("tab", "multi")),
		OpenAll:  key.NewBinding(key.WithKeys("ctrl+o"), key.WithHelp("^o", "open all")),
		Toggle:   key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("^a", "all files")),
		Copy:     key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("^y", "copy the path")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Quit:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc/q", "quit")),
		PrevUp:   key.NewBinding(key.WithKeys("shift+up"), key.WithHelp("⇧↑/⇧↓", "scroll preview")),
		PrevDown: key.NewBinding(key.WithKeys("shift+down")),
		Shrink:   key.NewBinding(key.WithKeys("shift+left"), key.WithHelp("⇧←/⇧→", "resize the list")),
		Grow:     key.NewBinding(key.WithKeys("shift+right")),
		// Help-only entry: a binding without keys is disabled and the help
		// bubble would skip it. Nothing ever matches against it.
		Filter: key.NewBinding(key.WithKeys("type"), key.WithHelp("type", "filter")),
		Help:   helpBinding(true),
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
	flash  flash  // confirmation on the help line (flash.go)
	panel  panel  // options and keys, over the frame while it is open (panel.go)
	ti     textinput.Model
	listVP viewport.Model
	prevVP viewport.Model
	help   help.Model
	keys   keyMap
	width  int
	height int
	split  int // the preview's share of the width, percent

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
		ti:           newFilterInput("asgotonotes", groupsPlaceholder),
		listVP:       viewport.New(viewport.WithWidth(50), viewport.WithHeight(20)),
		prevVP:       viewport.New(viewport.WithWidth(40), viewport.WithHeight(20)),
		help:         help.New(),
		keys:         defaultKeys(),
		split:        loadSplit(stateDir()),
		allFiles:     loadSetting(stateDir(), "files") == "all",
		renders:      map[string]string{},
		previewStyle: "dark",
		width:        94,
		height:       24,
	}
	m.syncHelp()
	m.applyFilter()
	m.resize()
	m.renderList()
	m.updatePreview() // view 1 previews are synchronous: the first frame is complete
	return m
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

// copyPath is what ctrl+y copies: the file under the cursor in view 2, the
// root of the worktree under it in view 1, "" with nothing under it.
func (m *model) copyPath() string {
	if m.view == viewFiles {
		if f := m.currentFile(); f != nil {
			return f.path
		}
		return ""
	}
	if g := m.currentGroup(); g != nil {
		return g.root
	}
	return ""
}

// ---- layout ----

// innerW is the width inside the frame's sides.
func (m *model) innerW() int { return max(20, m.width-2) }

// detailsW is the preview's share of the main section, including the cell of
// padding on each side; prevW is the text width inside it.
func (m *model) detailsW() int { _, w := splitWidths(m.innerW(), m.split); return w }
func (m *model) prevW() int    { return max(10, m.detailsW()-2) }

// listW is what the divider leaves for the list.
func (m *model) listW() int { w, _ := splitWidths(m.innerW(), m.split); return w }

// hasContext reports whether the frame carries its context line: only view 2
// does (the group header), so the two views differ by two lines.
func (m *model) hasContext() bool { return m.view == viewFiles }

// bodyH is the height of the main section: everything but the frame's own
// lines and the help.
func (m *model) bodyH() int { return max(1, m.height-frameRows(m.hasContext())-1) }

func (m *model) resize() {
	m.listVP.SetWidth(m.listW())
	m.listVP.SetHeight(m.bodyH())
	m.prevVP.SetWidth(m.prevW())
	m.prevVP.SetHeight(m.bodyH())
	m.help.SetWidth(max(0, m.width-4))
	sizeInput(&m.ti, m.width-4)
}

// resizeList moves the divider between the list and the preview by one step.
func (m *model) resizeList(grow bool) tea.Cmd {
	m.split = stepSplit(m.split, grow)
	saveSplit(stateDir(), m.split)
	m.resize()
	m.renderList()
	return m.updatePreview()
}

// what the filter searches in each view
const (
	groupsPlaceholder = "Search by ticket, repo, branch…"
	filesPlaceholder  = "Search by path or status…"
)

// ---- filtering ----

func (m *model) applyFilter() {
	q := m.ti.Value()
	if m.view == viewFiles {
		m.fRows = filterFiles(m.cur.visibleFiles(m.allFiles), q, m.home)
		m.fCursor = 0
		if len(m.fRows) == 0 {
			m.fCursor = -1
		}
		return
	}
	m.gRows = filterGroups(visibleGroups(m.groups, m.allFiles), q, m.home)
	m.gCursor = 0
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
	m.ti.Placeholder = filesPlaceholder
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
	m.ti.Placeholder = groupsPlaceholder
	m.cur = nil
	m.fRows = nil
	m.ti.SetValue(m.groupQuery)
	m.ti.CursorEnd()
	m.keys.files = false
	m.applyFilter()
	m.keepCursorOnGroup(root)
	m.resize()
}

// syncHelp makes the toggle's help name what the next press does.
func (m *model) syncHelp() {
	label := "all files"
	if m.allFiles {
		label = "notes only"
	}
	m.keys.Toggle.SetHelp("^a", label)
}

// options is what the panel offers: what is listed, which keeps ctrl+a.
func (m *model) options() []option {
	cur := 0
	if m.allFiles {
		cur = 1
	}
	return []option{{id: "files", label: "Files", values: []string{"notes only", "all files"}, cur: cur, key: "^a"}}
}

// setOption changes what is listed and remembers it. The key and the panel
// both come through here.
func (m *model) setOption(id string, v int) tea.Cmd {
	if id != "files" {
		return nil
	}
	m.allFiles = v == 1
	value := "notes"
	if m.allFiles {
		value = "all"
	}
	saveSetting(stateDir(), "files", value)
	m.syncHelp()
	m.refilter()
	m.renderList()
	return m.updatePreview()
}

// ---- list rendering ----

const (
	countColW = 10 // " 123 notes"
	ageColW   = 4

	// file rows: the date keeps its full form while the path gets
	// filePathFullW cells, its compact form while it gets filePathAgeW
	fileDateLayout = "02/01 15:04"
	filePathFullW  = 28
	filePathAgeW   = 16
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
// age. A list too narrow for all four drops the repo, which the label and the
// preview's paths already hint at. The selected row is padded to the full
// width before styling so its background spans the whole column.
func (m *model) groupLine(r groupRow, selected bool, width int) string {
	repoW := 18
	if width < 60 {
		repoW = 12
	}
	labelW := width - 2 - 1 - repoW - 1 - countColW - 1 - ageColW
	if labelW < 12 {
		repoW, labelW = 0, max(8, labelW+repoW+1)
	}
	repo := r.g.repo
	if repo == "" {
		repo = "-"
	}
	count := padLeft(countLabel(r.g.count(m.allFiles), m.allFiles), countColW)
	age := padLeft(compactAge(r.g.last, m.now), ageColW)
	if selected {
		line := stSel.Render("▌ ") + highlight(padRight(r.g.label, labelW), r.labelIdx, stSel) + stSel.Render(" ")
		if repoW > 0 {
			line += highlight(padRight(repo, repoW), r.repoIdx, stSel) + stSel.Render(" ")
		}
		return selPad(truncate(line+stSel.Render(count+" "+age), width), width)
	}
	line := "  " + padRight(highlight(r.g.label, r.labelIdx, stTitle), labelW) + " "
	if repoW > 0 {
		line += padRight(highlight(repo, r.repoIdx, stRepo), repoW) + " "
	}
	return truncate(line+count+" "+stDim.Render(age), width)
}

// fileLine renders one file row: status, last write, path. cursorCol adds
// the two-cell gutter (cursor bar, multi-select mark) of view 2; the view 1
// preview reuses the row without it. The path is what the row is for, so a
// narrow list takes the room from the date: it shrinks to the compact age of
// view 1 and then goes away.
func (m *model) fileLine(r fileRow, selected, cursorCol bool, width int) string {
	const statusW, minPathW = 9, 8
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
	room := width - ansi.StringWidth(gutter) - statusW - 1
	date := r.f.last.Format(fileDateLayout)
	switch {
	case room-len(fileDateLayout)-2 >= filePathFullW:
	case room-ageColW-2 >= filePathAgeW:
		date = padLeft(compactAge(r.f.last, m.now), ageColW)
	default:
		date = ""
	}
	sep := ""
	if date != "" {
		sep = "  "
	}
	pathW := max(minPathW, room-ansi.StringWidth(date)-len(sep))
	display := tildePath(r.f.path, m.home)
	dim := 0
	if m.cur != nil && insideRoot(m.cur.root, r.f.path) {
		dim = len(tildePath(m.cur.root, m.home)) + 1
	} else if m.cur == nil {
		if g := m.currentGroup(); g != nil && insideRoot(g.root, r.f.path) {
			dim = len(tildePath(g.root, m.home)) + 1
		}
	}
	if selected {
		line := stSel.Render(gutter+padRight(r.f.status, statusW)+" "+date+sep) + pathCells(display, 0, r.idx, pathW, true)
		return selPad(truncate(line, width), width)
	}
	status := statusStyle(r.f.status).Render(padRight(r.f.status, statusW))
	return truncate(gutter+status+" "+stDim.Render(date)+sep+pathCells(display, dim, r.idx, pathW, false), width)
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
	c := m.cursor()
	m.listVP.SetYOffset(scrollTo(m.listVP.YOffset(), m.listVP.Height(), m.rowCount(), c, c))
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

// openerPath resolves the editor CLI. ASGOTONOTES_OPENER replaces it (the pty
// driver points it at a logging stub). The popup may run with a shorter
// PATH than an interactive shell, so the usual install locations of the zed
// CLI are tried as well.
func openerPath() (string, error) {
	if b := os.Getenv("ASGOTONOTES_OPENER"); b != "" {
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
		fmt.Fprintf(os.Stderr, "asgotonotes: skipped %d file(s) that no longer exist\n", skipped)
	}
	if len(files) == 0 {
		return
	}
	bin, err := openerPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "asgotonotes:", err)
		return
	}
	if err := exec.Command(bin, append([]string{"-n"}, files...)...).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "asgotonotes:", err)
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
		return m, m.setPreviewStyle(glamourStyle(msg))

	case flashMsg:
		return m, m.flash.set(string(msg))

	case clearFlashMsg:
		m.flash.clear(msg)
		return m, nil

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
		if m.panel.open {
			return m, nil
		}
		// Over the list the wheel moves the selection, as in asgitlog; anywhere
		// else it scrolls the preview.
		if m.overList(msg.X, msg.Y) {
			if k, ok := wheelKey(msg); ok {
				return m.handleKey(k)
			}
			return m, nil
		}
		m.prevVP, _ = m.prevVP.Update(msg)
		return m, nil

	case tea.MouseClickMsg:
		if m.panel.open {
			return m, nil
		}
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
	case m.panel.open:
		// The panel takes every key: esc closes it before anything else.
		if a := m.panel.update(msg, m.options()); a.id != "" {
			return m, m.setOption(a.id, a.value)
		}
		return m, nil
	case isHelpKey(msg):
		m.panel.toggle()
		return m, nil
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
		return m, m.setOption("files", nextValue(m.options(), "files"))
	case key.Matches(msg, m.keys.Copy):
		// The flash shows the ~ form; the clipboard gets the absolute path.
		p := m.copyPath()
		return m, copyCmd("asgotonotes", tildePath(p, m.home), p)
	case m.keys.Nav.matches(msg):
		m.setCursor(m.keys.Nav.move(msg, m.cursor(), m.rowCount(), m.listVP.Height(), nil))
		m.renderList()
		return m, m.updatePreview()
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

// overList reports whether a screen cell is inside the list.
func (m *model) overList(x, y int) bool {
	return inList(x, y, listY(m.hasContext()), m.listW(), m.bodyH())
}

// handleClick moves the cursor to the row under a left click on the list. It
// never opens anything: that stays on enter.
func (m model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || !m.overList(msg.X, msg.Y) {
		return m, nil
	}
	i, ok := rowUnder(msg.Y, listY(m.hasContext()), m.listVP.YOffset(), m.rowCount())
	if !ok || i == m.cursor() {
		return m, nil
	}
	m.setCursor(i)
	m.renderList()
	return m, m.updatePreview()
}

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// render stacks the sections in one frame (see frame.go).
func (m model) render() string {
	w := m.width
	out := frameHead(w, m.context(), devMark(), m.ti.View())
	out = append(out, splitMain(m.listLines(), strings.Split(m.prevVP.View(), "\n"),
		m.listW(), m.detailsW(), m.counter(), scrollPos(&m.prevVP))...)
	out = append(out, framed(w, m.footLine()), hline(w, "╰", "╯", "", ""))
	if m.panel.open {
		keys := keyLines(m.help, m.keys, w-10)
		out = overlay(out, panelLines(m.options(), m.panel.cursor, keys, w-4, len(out)-2), w)
	}
	return strings.Join(out, "\n")
}

// context is the frame's optional top line. View 1 needs none; view 2 says
// which group is being browsed, which nothing else on screen does.
func (m model) context() string {
	if m.view == viewFiles {
		return m.groupHeader()
	}
	return ""
}

// counter is the matches/total count of the current view and mode, for the
// edge under the list.
func (m model) counter() string {
	total := len(visibleGroups(m.groups, m.allFiles))
	if m.view == viewFiles {
		total = len(m.cur.visibleFiles(m.allFiles))
	}
	return stCount.Render(strconv.Itoa(m.rowCount()) + "/" + strconv.Itoa(total))
}

// listLines is the list as exactly bodyH lines of listW cells.
func (m model) listLines() []string {
	lines := strings.Split(m.leftColumn(), "\n")
	for len(lines) < m.bodyH() {
		lines = append(lines, "")
	}
	lines = lines[:m.bodyH()]
	for i, l := range lines {
		lines[i] = fit(l, m.listW())
	}
	return lines
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
	return stDim.Render(truncate(" "+msg, m.listW()))
}

// groupHeader is the first line of view 2: label · root · count.
func (m model) groupHeader() string {
	dot := stDim.Render(" · ")
	h := stHeader.Render(m.cur.label) + dot + stDim.Render(tildePath(m.cur.root, m.home)) +
		dot + countLabel(m.cur.count(m.allFiles), m.allFiles)
	if n := len(m.selected); n > 0 {
		h += dot + stMark.Render(fmt.Sprintf("%d selected", n))
	}
	return h
}

// footer is the key help, or the notice while one is showing.
// footMsg is what takes the help's place while there is something to say.
func (m model) footMsg() string {
	switch {
	case m.flash.text != "":
		return m.flash.view(m.width - 4)
	case m.notice != "":
		return stError.Render(truncate(m.notice, max(0, m.width-4)))
	}
	return ""
}

func (m model) footLine() string {
	if msg := m.footMsg(); msg != "" {
		return msg
	}
	return helpLine(m.help, m.keys, m.width-4)
}
