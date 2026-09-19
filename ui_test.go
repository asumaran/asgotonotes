package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// uiFixture builds a model over three groups with preset statuses (no git):
// one with existing notes on disk, one with only a missing note, and one
// with tracked code only (hidden in notes mode).
func uiFixture(t *testing.T) (model, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("ASGOTONOTES_OPENER", "/usr/bin/true")
	root := filepath.Join(dir, "wt", "shop", "fix-ESHOP-551")
	os.MkdirAll(root, 0o755)
	write := func(name, body string) string {
		p := filepath.Join(root, name)
		os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	handoff := write("HANDOFF.md", "# Handoff\n")
	plan := write("PLAN.md", "# Plan\n")
	code := write("main.go", "package main\n")
	now := time.Now()
	groups := []*group{
		{root: root, repo: "shop", branch: "fix/ESHOP-551", label: "ESHOP-551", last: now, files: []noteFile{
			{path: handoff, last: now, status: statusUntracked},
			{path: plan, last: now.Add(-time.Hour), status: statusUntracked},
			{path: code, last: now.Add(-2 * time.Hour), status: statusTracked},
		}},
		{root: filepath.Join(dir, "wt", "syn"), repo: "synapse", branch: "feat/FED-2283", label: "FED-2283",
			last: now.Add(-24 * time.Hour), files: []noteFile{
				{path: filepath.Join(dir, "gone-brief.md"), last: now.Add(-24 * time.Hour), status: statusGone},
			}},
		{root: filepath.Join(dir, "Developer", "tool"), repo: "tool", branch: "main", label: "tool/main",
			last: now.Add(-48 * time.Hour), files: []noteFile{
				{path: filepath.Join(dir, "Developer", "tool", "x.go"), last: now.Add(-48 * time.Hour), status: statusTracked},
			}},
	}
	m := newModel(groups, "")
	res, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	return res.(model), root
}

func press(t *testing.T, m model, keys ...tea.KeyPressMsg) (model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		res, c := m.Update(k)
		m, cmd = res.(model), c
	}
	return m, cmd
}

func typed(s string) []tea.KeyPressMsg {
	var out []tea.KeyPressMsg
	for _, r := range s {
		out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return out
}

var (
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyTab   = tea.KeyPressMsg{Code: tea.KeyTab}
	keyCtrlA = tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
	keyCtrlO = tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
)

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestNotesModeHidesGroupsWithoutNotes(t *testing.T) {
	m, _ := uiFixture(t)
	if len(m.gRows) != 2 {
		t.Fatalf("got %d groups, want 2 (tool/main has no notes)", len(m.gRows))
	}
	m, _ = press(t, m, keyCtrlA)
	if len(m.gRows) != 3 || !m.allFiles {
		t.Fatalf("ctrl+a must show all 3 groups, got %d", len(m.gRows))
	}
	if !strings.Contains(ansi.Strip(m.listVP.View()), "3 files") {
		t.Errorf("all-files mode words the count as files:\n%s", ansi.Strip(m.listVP.View()))
	}
}

func TestEnterAndEscKeepTheGroup(t *testing.T) {
	m, _ := uiFixture(t)
	m, _ = press(t, m, typed("fed")...)
	if g := m.currentGroup(); g == nil || g.label != "FED-2283" {
		t.Fatalf("filter should land on FED-2283, got %+v", g)
	}
	m, _ = press(t, m, keyEnter)
	if m.view != viewFiles || m.cur.label != "FED-2283" || m.ti.Value() != "" {
		t.Fatalf("enter must open the group with an empty file filter: view=%v query=%q", m.view, m.ti.Value())
	}
	m, cmd := press(t, m, keyEsc)
	if isQuit(cmd) || m.view != viewGroups {
		t.Fatal("esc in view 2 goes back, it does not quit")
	}
	if m.ti.Value() != "fed" || m.currentGroup().label != "FED-2283" {
		t.Errorf("back must restore the query and the cursor: %q / %s", m.ti.Value(), m.currentGroup().label)
	}
	if _, cmd := press(t, m, keyEsc); !isQuit(cmd) {
		t.Error("esc in view 1 quits")
	}
}

func TestEscKeepsCursorWithoutQuery(t *testing.T) {
	m, _ := uiFixture(t)
	m, _ = press(t, m, keyDown, keyEnter, keyEsc)
	if g := m.currentGroup(); g == nil || g.label != "FED-2283" {
		t.Errorf("cursor must stay on the group that was open, got %+v", g)
	}
}

func TestToggleAllPersistsAcrossViews(t *testing.T) {
	m, _ := uiFixture(t)
	m, _ = press(t, m, keyEnter)
	if len(m.fRows) != 2 {
		t.Fatalf("notes mode lists 2 files, got %d", len(m.fRows))
	}
	m, _ = press(t, m, keyDown, keyCtrlA)
	if len(m.fRows) != 3 {
		t.Fatalf("all-files mode lists 3 files, got %d", len(m.fRows))
	}
	if f := m.currentFile(); f == nil || filepath.Base(f.path) != "PLAN.md" {
		t.Errorf("toggling must keep the cursor on the same file, got %+v", f)
	}
	if !strings.Contains(ansi.Strip(m.groupHeader()), "3 files") {
		t.Errorf("header = %q", ansi.Strip(m.groupHeader()))
	}
	m, _ = press(t, m, keyEsc)
	if !m.allFiles || len(m.gRows) != 3 {
		t.Errorf("the mode must survive going back: all=%v groups=%d", m.allFiles, len(m.gRows))
	}
}

func TestQQuitsOnlyWithEmptyFilter(t *testing.T) {
	m, _ := uiFixture(t)
	if _, cmd := press(t, m, typed("q")...); !isQuit(cmd) {
		t.Error("q with an empty filter quits")
	}
	m, cmd := press(t, m, typed("eq")...)
	if isQuit(cmd) || m.ti.Value() != "eq" {
		t.Errorf("q after text is text: quit=%v value=%q", isQuit(cmd), m.ti.Value())
	}
}

func TestEnterOpensTheFileUnderTheCursor(t *testing.T) {
	m, root := uiFixture(t)
	m, cmd := press(t, m, keyEnter, keyDown, keyEnter)
	if !isQuit(cmd) {
		t.Fatal("opening must quit so the popup closes first")
	}
	if len(m.open) != 1 || m.open[0] != filepath.Join(root, "PLAN.md") {
		t.Errorf("open = %v", m.open)
	}
}

func TestMultiSelectOpensInListOrder(t *testing.T) {
	m, root := uiFixture(t)
	m, _ = press(t, m, keyEnter, keyDown, keyTab) // marks PLAN.md; the cursor stays on the last row
	m, _ = press(t, m, tea.KeyPressMsg{Code: tea.KeyUp}, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if len(m.selected) != 2 || m.ti.Value() != "" {
		t.Fatalf("tab and space must mark without typing: selected=%v query=%q", m.selected, m.ti.Value())
	}
	if !strings.Contains(ansi.Strip(m.groupHeader()), "2 selected") {
		t.Errorf("header = %q", ansi.Strip(m.groupHeader()))
	}
	m, cmd := press(t, m, keyEnter)
	want := []string{filepath.Join(root, "HANDOFF.md"), filepath.Join(root, "PLAN.md")}
	if !isQuit(cmd) || strings.Join(m.open, ",") != strings.Join(want, ",") {
		t.Errorf("open = %v, want %v", m.open, want)
	}
}

func TestCtrlOOpensListedAndSkipsMissing(t *testing.T) {
	m, root := uiFixture(t)
	m, _ = press(t, m, keyCtrlA, keyEnter)
	os.Remove(filepath.Join(root, "PLAN.md")) // deleted after the popup loaded
	m, cmd := press(t, m, keyCtrlO)
	if !isQuit(cmd) || len(m.open) != 2 || m.skipped != 1 {
		t.Errorf("open=%v skipped=%d, want 2 files and 1 skipped", m.open, m.skipped)
	}
}

func TestOpeningOnlyMissingFilesStaysInThePopup(t *testing.T) {
	m, _ := uiFixture(t)
	m, cmd := press(t, m, keyDown, keyEnter, keyEnter) // FED-2283 → its gone note
	if isQuit(cmd) || m.open != nil {
		t.Fatalf("nothing to open: must not quit (open=%v)", m.open)
	}
	if !strings.Contains(m.notice, "no longer exists") {
		t.Errorf("notice = %q", m.notice)
	}
	m, _ = press(t, m, keyDown)
	if m.notice != "" {
		t.Error("the next key clears the notice")
	}
}

func TestViewsRenderFixedColumns(t *testing.T) {
	m, _ := uiFixture(t)
	for _, line := range strings.Split(m.listVP.View(), "\n") {
		if w := ansi.StringWidth(line); w > m.listW() {
			t.Errorf("group row overflows the list (%d > %d): %q", w, m.listW(), ansi.Strip(line))
		}
	}
	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"ESHOP-551", "shop", "2 notes", "FED-2283", "1 note", "HANDOFF.md", "untracked"} {
		if !strings.Contains(view, want) {
			t.Errorf("view 1 misses %q:\n%s", want, view)
		}
	}

	m, _ = press(t, m, keyEnter)
	view = ansi.Strip(m.View().Content)
	// The group header of view 2 lives on the frame's context line.
	context := strings.Split(view, "\n")[1]
	if !strings.HasPrefix(context, "│ ESHOP-551 · ~/wt/shop/fix-ESHOP-551 · 2 notes") {
		t.Errorf("view 2 context line = %q", context)
	}
	if h := strings.Count(view, "\n") + 1; h != m.height {
		t.Errorf("view 2 is %d lines tall, want %d", h, m.height)
	}
}

func TestPathCellsKeepsTheFileName(t *testing.T) {
	got := pathCells("~/wt/monorepo-front/fix-ESHOP-551-structured-data/HANDOFF.md", 10, nil, 20, true)
	plain := ansi.Strip(got)
	if ansi.StringWidth(plain) != 20 || !strings.HasPrefix(plain, "…") || !strings.HasSuffix(plain, "/HANDOFF.md") {
		t.Errorf("pathCells = %q", plain)
	}
	if short := pathCells("~/a.md", 0, nil, 20, false); short != "~/a.md" {
		t.Errorf("a path that fits is untouched: %q", short)
	}
}

func TestClickMovesTheCursorOnly(t *testing.T) {
	m, _ := uiFixture(t)
	res, _ := m.Update(tea.MouseClickMsg{X: 3, Y: listY(false) + 1, Button: tea.MouseLeft}) // second row
	got := res.(model)
	if got.gCursor != 1 || got.view != viewGroups {
		t.Errorf("click must select the row without entering it: cursor=%d view=%v", got.gCursor, got.view)
	}
	// The preview, the divider and the frame's own lines select nothing.
	for _, c := range [][2]int{{got.listW() + 10, listY(false)}, {got.listW() + 1, listY(false)}, {0, listY(false)}, {3, mainY(false)}, {3, 1}} {
		res, _ = got.Update(tea.MouseClickMsg{X: c[0], Y: c[1], Button: tea.MouseLeft})
		if res.(model).gCursor != 1 {
			t.Errorf("a click at %v must change nothing", c)
		}
	}
}

func TestBackgroundColorFlipsPreviewStyle(t *testing.T) {
	m, _ := uiFixture(t)
	m.renders["stale"] = "x"
	m.setPreviewStyle("light")
	if m.previewStyle != "light" || len(m.renders) != 0 {
		t.Errorf("style=%q renders=%d: the cache must be dropped with the palette", m.previewStyle, len(m.renders))
	}
}

func TestMissingIndexShowsTheReason(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	m := newModel(nil, "no index at ~/.claude/files-index/index.tsv")
	if !strings.Contains(ansi.Strip(m.View().Content), "no index at") {
		t.Errorf("view = %q", ansi.Strip(m.View().Content))
	}
	if _, cmd := press(t, m, keyEnter); isQuit(cmd) {
		t.Error("enter on an empty list does nothing")
	}
}

// TestFrameGeometry pins the single-frame layout in both views: exactly
// height lines, each exactly width cells, sections where the click math
// expects them.
func TestFrameGeometry(t *testing.T) {
	m, _ := uiFixture(t)
	check := func(m model, name string) {
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) != m.height {
			t.Errorf("%s: %d lines, want %d", name, len(lines), m.height)
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != m.width {
				t.Errorf("%s: line %d is %d cells, want %d: %q", name, i, w, m.width, ansi.Strip(l))
			}
		}
		plain := strings.Split(ansi.Strip(m.View().Content), "\n")
		if !strings.HasPrefix(plain[0], "╭") || !strings.HasPrefix(plain[len(plain)-1], "╰") ||
			!strings.Contains(plain[mainY(m.hasContext())], "┬") || !strings.HasPrefix(plain[listY(m.hasContext())], "│▌") {
			t.Errorf("%s: frame sections misplaced:\n%s", name, strings.Join(plain, "\n"))
		}
	}
	check(m, "view 1")
	// View 1 has no context line: the input sits right under the top border.
	if top := strings.Split(ansi.Strip(m.View().Content), "\n"); !strings.Contains(top[0], "2/2") || !strings.HasPrefix(top[1], "│ asgotonotes") {
		t.Errorf("view 1 head:\n%s\n%s", top[0], top[1])
	}
	if c := ansi.Strip(m.counter()); c != "2/2" {
		t.Errorf("view 1 counter = %q", c)
	}
	m, _ = press(t, m, keyEnter)
	check(m, "view 2")
	if c := ansi.Strip(m.counter()); c != "2/2" {
		t.Errorf("view 2 counter = %q", c)
	}
}

// TestMain sandboxes the state dir: tests must never touch the real one.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "asgotonotes-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// TestResizeList: shift+arrows move the divider, the frame still fits, and
// the position is there for the next run.
func TestResizeList(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	m, _ := uiFixture(t)
	m.split = splitDefault
	m.resize()
	w := m.listW()
	shift := func(code rune) {
		next, _ := m.Update(tea.KeyPressMsg{Code: code, Mod: tea.ModShift})
		m = next.(model)
	}

	shift(tea.KeyRight)
	if m.listW() <= w || m.split != splitDefault-splitStep || loadSplit(stateDir()) != m.split {
		t.Errorf("grow: list %d -> %d, split=%d, saved=%d", w, m.listW(), m.split, loadSplit(stateDir()))
	}
	if m.listVP.Width() != m.listW() || m.prevVP.Width() != m.prevW() {
		t.Errorf("viewports %d | %d, want %d | %d", m.listVP.Width(), m.prevVP.Width(), m.listW(), m.prevW())
	}
	for i, l := range strings.Split(m.View().Content, "\n") {
		if got := ansi.StringWidth(l); got != m.width {
			t.Errorf("line %d is %d cells after the resize, want %d", i, got, m.width)
		}
	}

	shift(tea.KeyLeft)
	shift(tea.KeyLeft)
	if m.listW() >= w || m.split != splitDefault+splitStep {
		t.Errorf("shrink: list %d -> %d, split=%d", w, m.listW(), m.split)
	}
	for range 10 {
		shift(tea.KeyLeft)
	}
	if m.split != splitMax {
		t.Errorf("split should clamp at %d, got %d", splitMax, m.split)
	}
}

// TestMouseWheelFollowsThePointer: over the list the wheel moves the
// selection, as in asgitlog; anywhere else it scrolls the preview.
func TestMouseWheelFollowsThePointer(t *testing.T) {
	m, _ := uiFixture(t)
	wheel := func(x int, b tea.MouseButton) {
		next, _ := m.Update(tea.MouseWheelMsg{X: x, Y: listY(m.hasContext()), Button: b})
		m = next.(model)
	}
	first := m.cursor()
	wheel(2, tea.MouseWheelDown)
	if m.cursor() <= first {
		t.Errorf("wheel down over the list: cursor %d -> %d", first, m.cursor())
	}
	wheel(2, tea.MouseWheelUp)
	if m.cursor() != first {
		t.Errorf("wheel up over the list: cursor = %d, want %d", m.cursor(), first)
	}
	m.prevVP.SetContent(strings.Repeat("line\n", 200))
	wheel(m.listW()+10, tea.MouseWheelDown)
	if m.cursor() != first || m.prevVP.YOffset() == 0 {
		t.Errorf("wheel over the preview: cursor = %d, preview at %d", m.cursor(), m.prevVP.YOffset())
	}
}
