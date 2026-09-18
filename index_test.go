package main

import (
	"strings"
	"testing"
	"time"
)

const testHome = "/Users/dev"

// fixtureIndex mixes what the real index contains: repeated (root, file)
// pairs, unsorted timestamps, a legacy root without repo, a root outside git
// without branch, a five-column line and lines that must be skipped.
const fixtureIndex = "" +
	"1000\t/Users/dev/wt/shop/fix-ESHOP-2707-ssr\tshop\tfix/ESHOP-2707-ssr\t/Users/dev/wt/shop/fix-ESHOP-2707-ssr/PLAN.md\ts1\n" +
	"3000\t/Users/dev/wt/shop/fix-ESHOP-2707-ssr\tshop\tfix/ESHOP-2707-ssr\t/Users/dev/wt/shop/fix-ESHOP-2707-ssr/PLAN.md\ts2\n" +
	"2000\t/Users/dev/wt/shop/fix-ESHOP-2707-ssr\tshop\tfix/ESHOP-2707-ssr\t/Users/dev/.claude/plans/ssr-koala.md\ts1\n" +
	"5000\t/Users/dev/Developer/dotfiles\tdotfiles\tmain\t/Users/dev/Developer/dotfiles/README.md\ts3\n" +
	"4000\t/Users/dev/Developer/dotfiles\tdotfiles\told-branch\t/Users/dev/Developer/dotfiles/install.sh\ts3\n" +
	"500\t/Users/dev/wt/fed-2283-legacy\t\t\t/tmp/draft.md\ts4\n" +
	"400\t/Users/dev/scratch\t\t\t/Users/dev/scratch/notes.txt\n" +
	"not-a-number\t/x\tr\tb\t/x/a.md\ts\n" +
	"123\ttoo\tfew\n" +
	"\n" +
	"600\t\trepo\tmain\t/no/root.md\ts\n"

func fixtureGroups(t *testing.T) []*group {
	t.Helper()
	t.Setenv("HOME", testHome)
	return buildGroups(parseIndex(strings.NewReader(fixtureIndex)))
}

func TestParseIndex(t *testing.T) {
	recs := parseIndex(strings.NewReader(fixtureIndex))
	if len(recs) != 7 {
		t.Fatalf("got %d records, want 7 (malformed lines skipped)", len(recs))
	}
	first := recs[0]
	if first.ts != 1000 || first.repo != "shop" || first.branch != "fix/ESHOP-2707-ssr" ||
		first.path != "/Users/dev/wt/shop/fix-ESHOP-2707-ssr/PLAN.md" || first.session != "s1" {
		t.Errorf("first record = %+v", first)
	}
	legacy := recs[5]
	if legacy.repo != "" || legacy.branch != "" || legacy.path != "/tmp/draft.md" {
		t.Errorf("empty repo/branch columns must be kept empty: %+v", legacy)
	}
	if recs[6].session != "" {
		t.Errorf("five-column line should load without a session: %+v", recs[6])
	}
}

func TestBuildGroupsOrderAndDedup(t *testing.T) {
	groups := fixtureGroups(t)
	var roots []string
	for _, g := range groups {
		roots = append(roots, g.root)
	}
	want := []string{
		"/Users/dev/Developer/dotfiles",
		"/Users/dev/wt/shop/fix-ESHOP-2707-ssr",
		"/Users/dev/wt/fed-2283-legacy",
		"/Users/dev/scratch",
	}
	if strings.Join(roots, ",") != strings.Join(want, ",") {
		t.Fatalf("group order = %v, want newest first %v", roots, want)
	}

	shop := groups[1]
	if len(shop.files) != 2 {
		t.Fatalf("PLAN.md written twice must be one file, got %d files", len(shop.files))
	}
	if shop.files[0].path != "/Users/dev/wt/shop/fix-ESHOP-2707-ssr/PLAN.md" || shop.files[0].last.Unix() != 3000 {
		t.Errorf("newest write wins and sorts first: %+v", shop.files[0])
	}
	if shop.last.Unix() != 3000 {
		t.Errorf("group last = %d, want 3000", shop.last.Unix())
	}
}

func TestBuildGroupsUsesNewestRepoAndBranch(t *testing.T) {
	groups := fixtureGroups(t)
	dot := groups[0]
	if dot.branch != "main" || dot.repo != "dotfiles" {
		t.Errorf("branch/repo = %q/%q, want the newest record's main/dotfiles", dot.branch, dot.repo)
	}
	if dot.label != "dotfiles/main" {
		t.Errorf("label = %q", dot.label)
	}
}

func TestGroupLabel(t *testing.T) {
	cases := []struct {
		name, root, repo, branch, want string
	}{
		{"ticket in branch", "/Users/dev/wt/shop/x", "shop", "fix/ESHOP-2707-ssr-locale-race", "ESHOP-2707"},
		{"ticket uppercased", "/Users/dev/wt/syn/x", "synapse", "feat/fed-2283-thing", "FED-2283"},
		{"ticket in root dir", "/Users/dev/wt/fed-2283-legacy", "", "", "FED-2283"},
		{"branch wins over root", "/Users/dev/wt/AB-1", "r", "fix/CD-2", "CD-2"},
		{"first match", "/x/y", "r", "fix/AB-1-and-CD-2", "AB-1"},
		{"single letter is not a ticket", "/x/y", "r", "fix/a-1", "r/fix/a-1"},
		{"repo/branch", "/Users/dev/Developer/dotfiles", "dotfiles", "main", "dotfiles/main"},
		{"branch without repo", "/x/y", "", "main", "main"},
		{"root with tilde", "/Users/dev/scratch", "", "", "~/scratch"},
		{"root outside home", "/opt/stuff", "", "", "/opt/stuff"},
	}
	for _, c := range cases {
		if got := groupLabel(c.root, c.repo, c.branch, testHome); got != c.want {
			t.Errorf("%s: groupLabel = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTildePath(t *testing.T) {
	cases := map[string]string{
		"/Users/dev":          "~",
		"/Users/dev/a/b.md":   "~/a/b.md",
		"/Users/developer/x":  "/Users/developer/x",
		"/tmp/draft.md":       "/tmp/draft.md",
		"/private/Users/dev/": "/private/Users/dev/",
	}
	for in, want := range cases {
		if got := tildePath(in, testHome); got != want {
			t.Errorf("tildePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFileStatus(t *testing.T) {
	root := "/Users/dev/wt/shop/fix"
	tracked := map[string]bool{root + "/README.md": true}
	cases := []struct {
		name, path string
		exists     bool
		inGit      bool
		want       string
	}{
		{"missing wins over everything", root + "/README.md", false, true, statusGone},
		{"missing plan", testHome + "/.claude/plans/x.md", false, true, statusGone},
		{"plan", testHome + "/.claude/plans/x.md", true, true, statusPlan},
		{"tracked", root + "/README.md", true, true, statusTracked},
		{"untracked", root + "/HANDOFF.md", true, true, statusUntracked},
		{"harness dir", testHome + "/.claude/harness/x/brief.md", true, true, statusOutside},
		{"tmp", "/tmp/draft.md", true, true, statusOutside},
		{"root outside git", root + "/notes.md", true, false, statusOutside},
		{"sibling dir sharing the prefix", root + "-other/a.md", true, true, statusOutside},
	}
	for _, c := range cases {
		if got := fileStatus(root, c.path, testHome, c.exists, tracked, c.inGit); got != c.want {
			t.Errorf("%s: fileStatus = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIsNote(t *testing.T) {
	cases := []struct {
		path, status string
		want         bool
	}{
		// the handoff's passing examples
		{"/Users/dev/.claude/plans/x.md", statusPlan, true},
		{"/wt/HANDOFF.md", statusUntracked, true},
		{"/wt/PLAN.md", statusUntracked, true},
		{"/tmp/draft.md", statusOutside, true},
		{"/Users/dev/.claude/harness/x/brief.md", statusOutside, true},
		{"/Users/dev/.claude/harness/x/script.py", statusOutside, true},
		// and the failing ones
		{"/wt/README.md", statusTracked, false},
		{"/wt/src/app.ts", statusTracked, false},
		{"/wt/src/new.ts", statusUntracked, false},
		{"/wt/.github/ci.yml", statusUntracked, false},
		{"/wt/src/old.spec.ts", statusGone, false},
		// edges
		{"/wt/notes.TXT", statusUntracked, true},
		{"/wt/doc.markdown", statusUntracked, true},
		{"/tmp/old-draft.md", statusGone, true},
		{"/Users/dev/.claude/plans/no-extension", statusPlan, true},
	}
	for _, c := range cases {
		if got := isNote(c.path, c.status); got != c.want {
			t.Errorf("isNote(%q, %q) = %v, want %v", c.path, c.status, got, c.want)
		}
	}
}

func TestVisibleGroupsAndCounts(t *testing.T) {
	g1 := &group{root: "/a", files: []noteFile{
		{path: "/a/PLAN.md", status: statusUntracked},
		{path: "/a/main.go", status: statusTracked},
	}}
	g2 := &group{root: "/b", files: []noteFile{{path: "/b/main.go", status: statusTracked}}}
	groups := []*group{g1, g2}

	if got := visibleGroups(groups, false); len(got) != 1 || got[0] != g1 {
		t.Errorf("notes mode must hide groups without notes, got %d groups", len(got))
	}
	if got := visibleGroups(groups, true); len(got) != 2 {
		t.Errorf("all-files mode shows every group, got %d", len(got))
	}
	if g1.count(false) != 1 || g1.count(true) != 2 {
		t.Errorf("counts = %d notes / %d files, want 1 / 2", g1.count(false), g1.count(true))
	}
	if files := g1.visibleFiles(false); len(files) != 1 || files[0].path != "/a/PLAN.md" {
		t.Errorf("visibleFiles(notes) = %+v", files)
	}
}

func TestCountLabel(t *testing.T) {
	cases := []struct {
		n    int
		all  bool
		want string
	}{
		{1, false, "1 note"}, {2, false, "2 notes"}, {0, false, "0 notes"},
		{1, true, "1 file"}, {20, true, "20 files"},
	}
	for _, c := range cases {
		if got := countLabel(c.n, c.all); got != c.want {
			t.Errorf("countLabel(%d, %v) = %q, want %q", c.n, c.all, got, c.want)
		}
	}
}

func TestCompactAge(t *testing.T) {
	now := time.Unix(10_000_000, 0)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{0, "0m"},
		{5 * time.Minute, "5m"},
		{59*time.Minute + 59*time.Second, "59m"},
		{time.Hour, "1h"},
		{23 * time.Hour, "23h"},
		{24 * time.Hour, "1d"},
		{6 * 24 * time.Hour, "6d"},
		{7 * 24 * time.Hour, "1w"},
		{45 * 24 * time.Hour, "6w"},
		{-time.Minute, "0m"}, // clock skew
	}
	for _, c := range cases {
		if got := compactAge(now.Add(-c.ago), now); got != c.want {
			t.Errorf("compactAge(%s ago) = %q, want %q", c.ago, got, c.want)
		}
	}
}
