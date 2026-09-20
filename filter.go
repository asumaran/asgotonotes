package main

// Fuzzy filtering for both views. Rows keep their newest-first order while
// filtering (the list is a timeline, not a ranking); the score only decides
// where the cursor lands. Matched positions are byte offsets into the
// displayed string, as sahilm/fuzzy reports them.

import ()

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
	hits := findFields(q, labels, repos, hidden)
	for i := range groups {
		if h, ok := hits[i]; ok {
			rows = append(rows, groupRow{g: groups[i], score: h.Score, labelIdx: h.Any[0], repoIdx: h.Any[1]})
		}
	}
	return rank(rows, func(r groupRow) int { return r.score }, nil) // a search result: best match first
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
	hits := findFields(q, paths, statuses)
	for i := range files {
		if h, ok := hits[i]; ok {
			rows = append(rows, fileRow{f: files[i], score: h.Score, idx: h.Any[0]})
		}
	}
	return rank(rows, func(r fileRow) int { return r.score }, nil) // a search result: best match first
}
