package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func filterFixture() []*group {
	mk := func(label, repo, branch, root string, ts int64) *group {
		return &group{label: label, repo: repo, branch: branch, root: root, last: time.Unix(ts, 0)}
	}
	return []*group{
		mk("ESHOP-2707", "monorepo-front", "fix/ESHOP-2707-ssr-locale-race", "/Users/dev/wt/monorepo-front/fix-ESHOP-2707", 400),
		mk("FED-2283", "synapse", "feat/FED-2283-tables", "/Users/dev/wt/synapse/feat-FED-2283", 300),
		mk("dotfiles/main", "dotfiles", "main", "/Users/dev/Developer/dotfiles", 200),
		mk("ESHOP-551", "monorepo-front", "fix/ESHOP-551-structured-data", "/Users/dev/wt/monorepo-front/fix-ESHOP-551", 100),
	}
}

func rowLabels(rows []groupRow) string {
	var out []string
	for _, r := range rows {
		out = append(out, r.g.label)
	}
	return strings.Join(out, ",")
}

func TestFilterGroupsEmptyQueryKeepsEverything(t *testing.T) {
	rows := filterGroups(filterFixture(), "", testHome)
	if got := rowLabels(rows); got != "ESHOP-2707,FED-2283,dotfiles/main,ESHOP-551" {
		t.Errorf("rows = %s", got)
	}
}

// A query ranks the rows (rank.go); the list's own newest-first order only
// decides between matches that score the same, as these two do.
func TestFilterGroupsEqualScoresStayNewestFirst(t *testing.T) {
	rows := filterGroups(filterFixture(), "eshop", testHome)
	if got := rowLabels(rows); got != "ESHOP-2707,ESHOP-551" {
		t.Errorf("rows = %s, want both ESHOP groups, the newest first", got)
	}
	if rows[0].score != rows[1].score {
		t.Errorf("the fixture must give equal scores, got %d and %d", rows[0].score, rows[1].score)
	}
	if len(rows[0].labelIdx) != len("eshop") {
		t.Errorf("label match must carry highlight indexes, got %v", rows[0].labelIdx)
	}
}

func TestFilterGroupsMatchesRepoAndBranch(t *testing.T) {
	if got := rowLabels(filterGroups(filterFixture(), "synapse", testHome)); got != "FED-2283" {
		t.Errorf("repo query: rows = %s", got)
	}
	// "tables" only appears in the branch, which is not displayed.
	rows := filterGroups(filterFixture(), "tables", testHome)
	if got := rowLabels(rows); got != "FED-2283" {
		t.Errorf("branch query: rows = %s", got)
	}
	if len(rows[0].labelIdx) != 0 || len(rows[0].repoIdx) != 0 {
		t.Errorf("a hidden-corpus hit has nothing to highlight: %+v", rows[0])
	}
}

func TestFilteringPutsTheExactTicketFirst(t *testing.T) {
	rows := filterGroups(filterFixture(), "eshop-551", testHome)
	if len(rows) == 0 || rows[0].g.label != "ESHOP-551" {
		t.Errorf("rows = %s, want ESHOP-551 first", rowLabels(rows))
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].score > rows[i-1].score {
			t.Errorf("rows are not ranked: %s", rowLabels(rows))
		}
	}
}

func TestFilterFiles(t *testing.T) {
	files := []noteFile{
		{path: "/Users/dev/wt/shop/HANDOFF.md", status: statusUntracked},
		{path: "/Users/dev/.claude/plans/koala.md", status: statusPlan},
		{path: "/tmp/draft.md", status: statusGone},
	}
	rows := filterFiles(files, "koala", testHome)
	if len(rows) != 1 || rows[0].f.path != files[1].path {
		t.Fatalf("path query: %+v", rows)
	}
	if got := tildePath(rows[0].f.path, testHome); got[rows[0].idx[0]] != 'k' {
		t.Errorf("indexes must point into the ~ path, got %v in %q", rows[0].idx, got)
	}
	if rows := filterFiles(files, "gone", testHome); len(rows) != 1 || rows[0].f.status != statusGone {
		t.Errorf("status query: %+v", rows)
	}
	if rows := filterFiles(files, "", testHome); len(rows) != 3 {
		t.Errorf("empty query keeps all files, got %d", len(rows))
	}
}

// TestFilterFilesRanks: under a query the files are a search result, best
// match first; without one they keep the order they came in.
func TestFilterFilesRanks(t *testing.T) {
	files := []noteFile{
		{path: "/Users/dev/wt/shop/pull-and-rebase.md", status: statusUntracked}, // scattered
		{path: "/Users/dev/wt/shop/explanation.md", status: statusUntracked},     // inside a word
		{path: "/Users/dev/wt/shop/PLAN.md", status: statusUntracked},
	}
	base := func(rows []fileRow) string {
		var out []string
		for _, r := range rows {
			out = append(out, r.f.path[strings.LastIndex(r.f.path, "/")+1:])
		}
		return strings.Join(out, ",")
	}
	if got := base(filterFiles(files, "", testHome)); got != "pull-and-rebase.md,explanation.md,PLAN.md" {
		t.Errorf("no query: rows = %s, want the order they came in", got)
	}
	rows := filterFiles(files, "plan", testHome)
	if got := base(rows); got != "PLAN.md,explanation.md,pull-and-rebase.md" {
		t.Errorf("rows = %s, want the best match first and the scattered one last", got)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i].score > rows[i-1].score {
			t.Errorf("rows are not ranked: %s", base(rows))
		}
	}
}

func TestHighlightKeepsText(t *testing.T) {
	got := highlight("ESHOP-2707", []int{0, 1, 6}, stTitle)
	if ansi.Strip(got) != "ESHOP-2707" {
		t.Errorf("highlight changed the text: %q", ansi.Strip(got))
	}
	// multi-byte runes: indexes are byte offsets
	if s := ansi.Strip(highlight("ñandú-12", []int{0, 6}, stTitle)); s != "ñandú-12" {
		t.Errorf("multi-byte text mangled: %q", s)
	}
}
