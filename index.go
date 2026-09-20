package main

// The files index: parsing, grouping by worktree root, group labels, file
// statuses and the note rule. Everything here is pure (no git, no
// filesystem beyond reading the index itself), so it is tested with
// fixtures. The index is written by a Claude Code PostToolUse hook: one
// tab-separated line per Write/Edit call, no header, no quoting, unsorted,
// with repeated (root, file) pairs:
//
//	<epoch> \t <worktree root> \t <repo> \t <branch> \t <file> \t <session>

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// indexPath resolves the index location; CLAUDE_FILES_INDEX overrides it
// (tests, development).
func indexPath() string {
	if p := os.Getenv("CLAUDE_FILES_INDEX"); p != "" {
		return p
	}
	return filepath.Join(homeDir(), ".claude", "files-index", "index.tsv")
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// record is one index line.
type record struct {
	ts      int64
	root    string
	repo    string
	branch  string
	path    string
	session string
}

// parseIndex reads index lines, skipping the ones it cannot use (too few
// fields, a non-numeric timestamp, an empty root or path). The session
// column is optional so older five-column lines still load.
func parseIndex(r io.Reader) []record {
	var recs []record
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) < 5 {
			continue
		}
		ts, err := strconv.ParseInt(strings.TrimSpace(f[0]), 10, 64)
		if err != nil || f[1] == "" || f[4] == "" {
			continue
		}
		rec := record{ts: ts, root: f[1], repo: f[2], branch: f[3], path: f[4]}
		if len(f) > 5 {
			rec.session = f[5]
		}
		recs = append(recs, rec)
	}
	return recs
}

// File statuses. statusOutside (empty) is a file that exists outside the
// group's worktree (harness dirs, /tmp) or whose root is not a git checkout.
const (
	statusPlan      = "plan"
	statusTracked   = "tracked"
	statusUntracked = "untracked"
	statusGone      = "gone"
	statusOutside   = ""
)

// noteFile is one unique file of a group with its newest write.
type noteFile struct {
	path   string
	last   time.Time
	status string
}

// group is one worktree root with every file written under its name.
type group struct {
	root   string
	repo   string
	branch string
	label  string
	last   time.Time  // newest write in the group
	files  []noteFile // unique paths, newest first
}

// buildGroups folds records into one group per root, newest group first,
// each with its unique files newest first. Repo and branch come from the
// newest record that has them: a main clone changes branch over time, and
// legacy lines may lack the repo. Statuses are filled in later (git.go).
func buildGroups(recs []record) []*group {
	type acc struct {
		g        *group
		last     map[string]int64
		repoTS   int64
		branchTS int64
	}
	accs := map[string]*acc{}
	var order []string
	for _, r := range recs {
		a, ok := accs[r.root]
		if !ok {
			a = &acc{g: &group{root: r.root}, last: map[string]int64{}, repoTS: -1, branchTS: -1}
			accs[r.root] = a
			order = append(order, r.root)
		}
		if ts, seen := a.last[r.path]; !seen || r.ts > ts {
			a.last[r.path] = r.ts
		}
		if r.repo != "" && r.ts > a.repoTS {
			a.g.repo, a.repoTS = r.repo, r.ts
		}
		if r.branch != "" && r.ts > a.branchTS {
			a.g.branch, a.branchTS = r.branch, r.ts
		}
	}

	groups := make([]*group, 0, len(order))
	for _, root := range order {
		a := accs[root]
		g := a.g
		for p, ts := range a.last {
			g.files = append(g.files, noteFile{path: p, last: time.Unix(ts, 0)})
		}
		sort.Slice(g.files, func(i, j int) bool {
			if !g.files[i].last.Equal(g.files[j].last) {
				return g.files[i].last.After(g.files[j].last)
			}
			return g.files[i].path < g.files[j].path
		})
		g.last = g.files[0].last
		g.label = groupLabel(g.root, g.repo, g.branch, homeDir())
		groups = append(groups, g)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if !groups[i].last.Equal(groups[j].last) {
			return groups[i].last.After(groups[j].last)
		}
		return groups[i].root < groups[j].root
	})
	return groups
}

// ticketRe matches a Jira-style ticket key: 2+ letters, a dash and digits
// (ESHOP-2707, fed-2283).
var ticketRe = regexp.MustCompile(`[A-Za-z]{2,}-[0-9]+`)

// groupLabel names a group: the ticket id found in the branch or in the
// root's last path segment (first match, uppercased), else repo/branch,
// else the root with ~.
func groupLabel(root, repo, branch, home string) string {
	if t := ticketRe.FindString(branch + " " + filepath.Base(root)); t != "" {
		return strings.ToUpper(t)
	}
	if branch != "" {
		if repo == "" {
			return branch
		}
		return repo + "/" + branch
	}
	return tildePath(root, home)
}

// tildePath abbreviates the home directory prefix to ~.
func tildePath(p, home string) string {
	if home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

// insideRoot reports whether path lives under root.
func insideRoot(root, path string) bool {
	return strings.HasPrefix(path, strings.TrimRight(root, "/")+"/")
}

// fileStatus classifies one file of a group. tracked holds the absolute
// paths git knows about; rootInGit is false when the root is not a git
// checkout (or no longer exists).
func fileStatus(root, path, home string, exists bool, tracked map[string]bool, rootInGit bool) string {
	switch {
	case !exists:
		return statusGone
	case insideRoot(filepath.Join(home, ".claude", "plans"), path):
		return statusPlan
	case tracked[path]:
		return statusTracked
	case rootInGit && insideRoot(root, path):
		return statusUntracked
	}
	return statusOutside
}

// isMemoryFile reports whether path is one of Claude's own memory files:
// anything under ~/.claude/projects/<project>/memory/.
func isMemoryFile(path, home string) bool {
	projects := filepath.Join(home, ".claude", "projects")
	if !insideRoot(projects, path) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(path, projects+"/"), "/")
	return len(parts) >= 3 && parts[1] == "memory"
}

// isNote is the default filter. Everything Claude wrote is indexed, but the
// repo's own files are noise here: a note is not tracked by git and is
// either .md/.markdown/.txt or lives outside the worktree (plans, harness
// dirs, /tmp drafts). Untracked code and deleted specs stay out, and so do
// Claude's memory files: they match the shape but are not ticket notes.
func isNote(path, status, home string) bool {
	if status == statusTracked || isMemoryFile(path, home) {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".txt":
		return true
	}
	return status == statusOutside || status == statusPlan
}

// visibleFiles returns the group's files for the current mode: notes only,
// or every indexed file.
func (g *group) visibleFiles(all bool) []noteFile {
	if all {
		return g.files
	}
	home := homeDir()
	var out []noteFile
	for _, f := range g.files {
		if isNote(f.path, f.status, home) {
			out = append(out, f)
		}
	}
	return out
}

// count is the number of files the group shows in the current mode.
func (g *group) count(all bool) int {
	if all {
		return len(g.files)
	}
	home, n := homeDir(), 0
	for _, f := range g.files {
		if isNote(f.path, f.status, home) {
			n++
		}
	}
	return n
}

// visibleGroups drops the groups with nothing to show in the current mode.
func visibleGroups(groups []*group, all bool) []*group {
	var out []*group
	for _, g := range groups {
		if g.count(all) > 0 {
			out = append(out, g)
		}
	}
	return out
}

// countLabel words a count for the mode: "3 notes", "1 file".
func countLabel(n int, all bool) string {
	word := "note"
	if all {
		word = "file"
	}
	if n != 1 {
		word += "s"
	}
	return fmt.Sprintf("%d %s", n, word)
}

// stateDir is where asgotonotes keeps its runtime state.
func stateDir() string { return stateDirFor("asgotonotes") }
