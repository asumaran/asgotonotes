// asgotonotes: a herdr plugin popup that browses the notes Claude Code wrote
// (plans, handoffs, drafts, reports), grouped by worktree, and opens the
// chosen ones in Zed.
//
// The data comes from ~/.claude/files-index/index.tsv, appended by a Claude
// Code PostToolUse hook on every Write/Edit, plus the notes found on disk in
// Claude's own folders that the hook never saw (scan.go). asgotonotes only
// reads: it never edits, deletes or commits anything.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
)

// version is the release tag; overridden at build time via
// -ldflags "-X main.version=vX.Y.Z" (see scripts/release.sh and CI).
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the embedded version")
	dump := flag.Bool("dump", false, "print the groups and their files (no TUI)")
	all := flag.Bool("all", false, "every indexed file instead of notes only (not remembered)")
	query := flag.String("query", "", "with -dump: print the matches and their scores instead of the list")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: asgotonotes [flags]")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	start := time.Now()
	groups, found, err := loadGroups(indexPath())
	loadErr := ""
	if err != nil {
		loadErr = err.Error()
	}

	if *dump {
		if err != nil {
			fmt.Fprintln(os.Stderr, "asgotonotes:", err)
			os.Exit(1)
		}
		runDump(os.Stdout, groups, found, showAll(*all), *query, time.Since(start))
		return
	}

	// Alt screen and mouse mode are declared per frame by View().
	res, err := tea.NewProgram(newModel(groups, loadErr, *all)).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "asgotonotes:", err)
		os.Exit(1)
	}
	final := res.(model)
	if err := runOpen(final.open, final.skipped); err != nil {
		fmt.Fprintln(os.Stderr, "asgotonotes:", err)
		os.Exit(1)
	}
}

// loadGroups reads the index, adds the notes on disk it does not have and
// resolves every file's status. found is how many the disk added.
func loadGroups(path string) (groups []*group, found int, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, fmt.Errorf("no index at %s (files-index hook not installed, or nothing written yet)",
				tildePath(path, homeDir()))
		}
		return nil, 0, err
	}
	defer f.Close()
	recs := parseIndex(f)
	dirs := scanDirs(homeDir())
	added := adopt(recs, scanNotes(dirs), dirs)
	groups = buildGroups(append(recs, added...))
	resolveStatuses(groups)
	return groups, len(added), nil
}

// runDump prints what the popup would show, without a TTY: the groups of
// view 1 and, under each, the files of view 2. With -query it prints the
// filtered groups and their scores instead. found is how many of the files
// came from the disk and not from the index.
func runDump(w io.Writer, groups []*group, found int, all bool, query string, took time.Duration) {
	home, now := homeDir(), time.Now()
	visible := visibleGroups(groups, all)
	total := 0
	for _, g := range visible {
		total += g.count(all)
	}
	files := "notes"
	if all {
		files = "all"
	}
	fmt.Fprintf(w, "index: %s, %d roots, %d shown, %s (files: %s), %d found on disk, loaded in %s\n",
		tildePath(indexPath(), home), len(groups), len(visible), countLabel(total, all), files, found,
		took.Round(time.Millisecond))

	if query != "" {
		fmt.Fprintf(w, "query %q:\n", query)
		for _, r := range filterGroups(visible, query, home) {
			fmt.Fprintf(w, "  %5d  %-28s %s\n", r.score, r.g.label, r.g.repo)
		}
		return
	}

	for _, g := range visible {
		repo := g.repo
		if repo == "" {
			repo = "-"
		}
		fmt.Fprintf(w, "%-28s %-18s %10s %4s  %s\n", truncate(g.label, 28), truncate(repo, 18),
			countLabel(g.count(all), all), compactAge(g.last, now), tildePath(g.root, home))
		for _, f := range g.visibleFiles(all) {
			fmt.Fprintf(w, "  %-9s %s  %s\n", f.status, f.last.Format("02/01 15:04"), tildePath(f.path, home))
		}
	}
}
