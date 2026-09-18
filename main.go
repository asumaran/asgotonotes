// gotonotes: a herdr plugin popup that browses the notes Claude Code wrote
// (plans, handoffs, drafts, reports), grouped by worktree, and opens the
// chosen ones in Zed.
//
// The data comes from ~/.claude/files-index/index.tsv, appended by a Claude
// Code PostToolUse hook on every Write/Edit. gotonotes only reads: it never
// edits, deletes or commits anything.
package main

import (
	"flag"
	"fmt"
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
	all := flag.Bool("all", false, "with -dump: every indexed file instead of notes only")
	query := flag.String("query", "", "with -dump: filter the groups and print scores")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	start := time.Now()
	groups, err := loadGroups(indexPath())
	loadErr := ""
	if err != nil {
		loadErr = err.Error()
	}

	if *dump {
		if err != nil {
			fmt.Fprintln(os.Stderr, "gotonotes:", err)
			os.Exit(1)
		}
		runDump(groups, *all, *query, time.Since(start))
		return
	}

	// Alt screen and mouse mode are declared per frame by View().
	res, err := tea.NewProgram(newModel(groups, loadErr)).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	final := res.(model)
	runOpen(final.open, final.skipped)
}

// loadGroups reads the index and resolves every file's status.
func loadGroups(path string) ([]*group, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no index at %s (files-index hook not installed, or nothing written yet)",
				tildePath(path, homeDir()))
		}
		return nil, err
	}
	defer f.Close()
	groups := buildGroups(parseIndex(f))
	resolveStatuses(groups)
	return groups, nil
}

// runDump prints what the popup would show, without a TTY: the groups of
// view 1 and, under each, the files of view 2. With -query it prints the
// filtered groups and their scores instead.
func runDump(groups []*group, all bool, query string, took time.Duration) {
	home, now := homeDir(), time.Now()
	visible := visibleGroups(groups, all)
	total := 0
	for _, g := range visible {
		total += g.count(all)
	}
	fmt.Printf("index: %s, %d roots, %d shown, %s, loaded in %s\n",
		tildePath(indexPath(), home), len(groups), len(visible), countLabel(total, all),
		took.Round(time.Millisecond))

	if query != "" {
		fmt.Printf("query %q:\n", query)
		for _, r := range filterGroups(visible, query, home) {
			fmt.Printf("  %5d  %-28s %s\n", r.score, r.g.label, r.g.repo)
		}
		return
	}

	for _, g := range visible {
		repo := g.repo
		if repo == "" {
			repo = "-"
		}
		fmt.Printf("%-28s %-18s %10s %4s  %s\n", truncate(g.label, 28), truncate(repo, 18),
			countLabel(g.count(all), all), compactAge(g.last, now), tildePath(g.root, home))
		for _, f := range g.visibleFiles(all) {
			fmt.Printf("  %-9s %s  %s\n", f.status, f.last.Format("02/01 15:04"), tildePath(f.path, home))
		}
	}
}
