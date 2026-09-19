package main

// Fuzzy filtering for both views. Rows keep their newest-first order while
// filtering (the list is a timeline, not a ranking); the score only decides
// where the cursor lands. Matched positions are byte offsets into the
// displayed string, as sahilm/fuzzy reports them.

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/sahilm/fuzzy"
)

// groupRow is one worktree in view 1.
type groupRow struct {
	g        *group
	score    int
	labelIdx []int // matched bytes in the label
	repoIdx  []int // matched bytes in the repo column
}

// fileRow is one file in view 2 (and in the view 1 preview).
type fileRow struct {
	f     noteFile
	score int
	idx   []int // matched bytes in the ~ path
}

// filterGroups matches the query against each group's label, repo and a
// hidden corpus (branch and root), keeping the best score per group.
func filterGroups(groups []*group, q, home string) []groupRow {
	rows := make([]groupRow, 0, len(groups))
	if q == "" {
		for _, g := range groups {
			rows = append(rows, groupRow{g: g})
		}
		return rows
	}
	labels := make([]string, len(groups))
	repos := make([]string, len(groups))
	hidden := make([]string, len(groups))
	for i, g := range groups {
		labels[i] = g.label
		repos[i] = g.repo
		hidden[i] = g.branch + " " + tildePath(g.root, home)
	}
	hits := map[int]*groupRow{}
	hit := func(i, score int) *groupRow {
		r, ok := hits[i]
		if !ok {
			r = &groupRow{g: groups[i], score: score}
			hits[i] = r
		} else if score > r.score {
			r.score = score
		}
		return r
	}
	for _, mt := range fuzzy.Find(q, labels) {
		hit(mt.Index, mt.Score).labelIdx = append([]int(nil), mt.MatchedIndexes...)
	}
	for _, mt := range fuzzy.Find(q, repos) {
		hit(mt.Index, mt.Score).repoIdx = append([]int(nil), mt.MatchedIndexes...)
	}
	for _, mt := range fuzzy.Find(q, hidden) {
		hit(mt.Index, mt.Score)
	}
	for i := range groups {
		if r, ok := hits[i]; ok {
			rows = append(rows, *r)
		}
	}
	return rows
}

// filterFiles matches the query against the status and the ~ path of each
// file. Only path matches are highlighted.
func filterFiles(files []noteFile, q, home string) []fileRow {
	rows := make([]fileRow, 0, len(files))
	if q == "" {
		for _, f := range files {
			rows = append(rows, fileRow{f: f})
		}
		return rows
	}
	paths := make([]string, len(files))
	statuses := make([]string, len(files))
	for i, f := range files {
		paths[i] = tildePath(f.path, home)
		statuses[i] = f.status
	}
	hits := map[int]*fileRow{}
	for _, mt := range fuzzy.Find(q, paths) {
		hits[mt.Index] = &fileRow{f: files[mt.Index], score: mt.Score, idx: append([]int(nil), mt.MatchedIndexes...)}
	}
	for _, mt := range fuzzy.Find(q, statuses) {
		if r, ok := hits[mt.Index]; !ok {
			hits[mt.Index] = &fileRow{f: files[mt.Index], score: mt.Score}
		} else if mt.Score > r.score {
			r.score = mt.Score
		}
	}
	for i := range files {
		if r, ok := hits[i]; ok {
			rows = append(rows, *r)
		}
	}
	return rows
}

// bestIndex returns the position of the highest score; ties go to the first
// (newest) row. -1 for an empty list.
func bestIndex(n int, score func(int) int) int {
	best := -1
	for i := 0; i < n; i++ {
		if best == -1 || score(i) > score(best) {
			best = i
		}
	}
	return best
}

// highlight styles the matched bytes of s and applies base to the rest. A
// match keeps what base says (the selected row's background, a bold title)
// and adds the match color and the underline, as asgitlog does.
func highlight(s string, idx []int, base lipgloss.Style) string {
	if len(idx) == 0 {
		return base.Render(s)
	}
	set := make(map[int]bool, len(idx))
	for _, i := range idx {
		set[i] = true
	}
	var b, run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(base.Render(run.String()))
			run.Reset()
		}
	}
	for i, r := range s {
		if set[i] {
			flush()
			b.WriteString(matchOver(base).Render(string(r)))
		} else {
			run.WriteRune(r)
		}
	}
	flush()
	return b.String()
}

// matchOver is the match style on top of base.
func matchOver(base lipgloss.Style) lipgloss.Style {
	return base.Foreground(stMatch.GetForeground()).Underline(true)
}
