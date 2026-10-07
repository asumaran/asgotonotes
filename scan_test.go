package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestAdopt covers who a note found on disk belongs to.
func TestAdopt(t *testing.T) {
	const (
		harness = testHome + "/.claude/harness"
		plans   = testHome + "/.claude/plans"
		wtA     = testHome + "/wt/shop/fix-ESHOP-551"
		wtB     = testHome + "/wt/shop/fix-ESHOP-2382"
		clone   = testHome + "/Developer/dotfiles"
	)
	dirs := []string{harness, plans}
	recs := []record{
		{ts: 100, root: wtA, repo: "shop", branch: "fix/ESHOP-551", path: harness + "/eshop-551/HANDOFF.md"},
		{ts: 200, root: wtA, repo: "shop", branch: "fix/ESHOP-551", path: harness + "/eshop-551/HANDOFF.md"},
		{ts: 900, root: wtB, repo: "shop", branch: "fix/ESHOP-2382", path: harness + "/eshop-551/reply.md"},
		{ts: 300, root: wtA, path: harness + "/tie/a.md"},
		{ts: 400, root: wtB, path: harness + "/tie/b.md"},
		{ts: 500, root: clone, path: plans + "/loose-koala.md"},
	}
	found := []scanned{
		{path: harness + "/eshop-551/HANDOFF.md", mtime: 9999},  // indexed already
		{path: harness + "/eshop-551/allow.txt", mtime: 1000},   // most lines in its folder
		{path: harness + "/eshop-551/old/deep/x.md", mtime: 10}, // no lines there: the folder above
		{path: harness + "/tie/c.md", mtime: 20},                // a tie: the newest line
		{path: plans + "/loose-badger.md", mtime: 30},           // the scanned folder gives no owner
		{path: plans + "/cfron-105-reports/sub/pilot.md", mtime: 40},
		{path: testHome + "/elsewhere/notes.md", mtime: 50}, // not under a scanned folder
	}

	got := map[string]record{}
	for _, r := range adopt(recs, found, dirs) {
		got[r.path] = r
	}
	want := map[string]string{
		harness + "/eshop-551/allow.txt":          wtA,
		harness + "/eshop-551/old/deep/x.md":      wtA,
		harness + "/tie/c.md":                     wtB,
		plans + "/loose-badger.md":                plans,
		plans + "/cfron-105-reports/sub/pilot.md": plans + "/cfron-105-reports",
	}
	if len(got) != len(want) {
		t.Errorf("adopted %d files, want %d: %+v", len(got), len(want), got)
	}
	for p, root := range want {
		if got[p].root != root {
			t.Errorf("%s went to %q, want %q", p, got[p].root, root)
		}
	}
	if r := got[harness+"/eshop-551/allow.txt"]; r.ts != 1000 || r.repo != "" || r.branch != "" {
		t.Errorf("an adopted record carries the mtime and no repo or branch: %+v", r)
	}
}

// TestAdoptKeepsTheGroup checks a found file joins its group without touching
// the repo and the branch the index gave it, and that an orphan folder is
// labeled by the ticket in its name.
func TestAdoptKeepsTheGroup(t *testing.T) {
	t.Setenv("HOME", testHome)
	const harness = testHome + "/.claude/harness"
	dirs := []string{harness}
	recs := []record{{ts: 100, root: testHome + "/Developer/dotfiles", repo: "dotfiles", branch: "main",
		path: harness + "/parity/HANDOFF.md"}}
	found := []scanned{
		{path: harness + "/parity/allow.txt", mtime: 700},
		{path: harness + "/orphan-9/notes.md", mtime: 50},
	}
	groups := buildGroups(append(recs, adopt(recs, found, dirs)...))
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	g := groups[0]
	if g.label != "dotfiles/main" || len(g.files) != 2 || g.files[0].path != harness+"/parity/allow.txt" || g.last.Unix() != 700 {
		t.Errorf("group = %+v, want dotfiles/main with the found file first", g)
	}
	if o := groups[1]; o.label != "ORPHAN-9" || o.root != harness+"/orphan-9" {
		t.Errorf("orphan group = %+v", o)
	}
}

// TestScanNotes walks a real tree: notes by extension only, hidden and cache
// folders left alone, a missing folder no error. A symlink to a regular note
// counts (a `home: repo` task links TASK.md from its work dir); a broken one
// does not.
func TestScanNotes(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		"a.md", "sub/b.TXT", "sub/deep/c.markdown",
		"sub/script.py", "sub/data.json", ".hidden/d.md", "sub/__pycache__/e.txt", "node_modules/f.md",
	} {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{
		"TASK.md":   filepath.Join(dir, "a.md"),
		"broken.md": filepath.Join(dir, "gone.md"),
	} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, f := range scanNotes([]string{filepath.Join(dir, "missing"), dir}) {
		rel, _ := filepath.Rel(dir, f.path)
		got = append(got, rel)
		if f.mtime == 0 {
			t.Errorf("%s has no mtime", rel)
		}
	}
	sort.Strings(got)
	want := []string{"TASK.md", "a.md", "sub/b.TXT", "sub/deep/c.markdown"}
	if len(got) != len(want) {
		t.Fatalf("found %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("found %v, want %v", got, want)
		}
	}
	if scanDirs("") != nil {
		t.Error("no home, nothing to scan")
	}
}

// TestTaskWork groups a task's state by its KEY along both paths: an indexed
// file written from some worktree and a file found on disk that the index
// never saw. A ticket KEY labels uppercased; a slug stays as named.
func TestTaskWork(t *testing.T) {
	t.Setenv("HOME", testHome)
	const (
		work = testHome + "/.claude/work"
		wt   = testHome + "/wt/shop/feat-platform-headers"
	)
	dirs := []string{work}
	recs := reroot([]record{
		{ts: 100, root: wt, repo: "shop", branch: "feat/platform-headers", path: work + "/eshop-1270/reports/F.md"},
		{ts: 200, root: wt, repo: "shop", branch: "feat/platform-headers", path: wt + "/main.go"},
	}, testHome)
	found := []scanned{
		{path: work + "/eshop-1270/plan.md", mtime: 300},
		{path: work + "/eshop-1270/reports/F-r2.md", mtime: 400},
		{path: work + "/quiz-login-rework/TASK.md", mtime: 50},
		{path: work + "/loose.md", mtime: 60},
	}
	groups := buildGroups(append(recs, reroot(adopt(recs, found, dirs), testHome)...))
	labels := map[string]int{}
	for _, g := range groups {
		labels[g.label] = len(g.files)
	}
	want := map[string]int{"ESHOP-1270": 3, "quiz-login-rework": 1, "shop/feat/platform-headers": 1, "~/.claude/work": 1}
	for l, n := range want {
		if labels[l] != n {
			t.Errorf("group %q has %d files, want %d (groups %v)", l, labels[l], n, labels)
		}
	}
	for _, g := range groups {
		if g.label == "ESHOP-1270" && (g.root != work+"/eshop-1270" || g.repo != "" || g.branch != "") {
			t.Errorf("ESHOP-1270 group = %+v, want the work folder with no repo or branch", g)
		}
	}
}

func TestWorkRoot(t *testing.T) {
	const work = testHome + "/.claude/work"
	cases := map[string]string{
		work + "/eshop-1270/TASK.md":         work + "/eshop-1270",
		work + "/eshop-1270/reports/F.md":    work + "/eshop-1270",
		work + "/loose.md":                   "",
		work + "-old/eshop-1270/x.md":        "",
		testHome + "/.claude/plans/a/b/c.md": "",
	}
	for p, want := range cases {
		if got := workRoot(p, testHome); got != want {
			t.Errorf("workRoot(%q) = %q, want %q", p, got, want)
		}
	}
	if got := workRoot(work+"/eshop-1270/x.md", ""); got != "" {
		t.Errorf("workRoot with no home = %q, want empty", got)
	}
}
