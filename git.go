package main

// Tracked-file lookup. Statuses need to know which of a group's files git
// tracks, and the worktrees involved are huge monorepos: a full `ls-files`
// of the tree costs seconds. Instead each group asks git only about its own
// files, in one call, and the answers are kept for the popup lifetime.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// lsFilesChunk bounds the pathspecs per git call so a group with thousands
// of files cannot exceed the argv limit. Real groups fit in one call.
const lsFilesChunk = 1000

// trackedFiles returns the absolute paths among files that git tracks in
// the checkout at root, and whether root is a git checkout at all. Files
// outside root are never asked about. Pathspecs are literal: Next.js-style
// names like `[id].tsx` are not globs.
func trackedFiles(root string, files []string) (tracked map[string]bool, inGit bool) {
	tracked = map[string]bool{}
	var inside []string
	for _, f := range files {
		if insideRoot(root, f) {
			inside = append(inside, f)
		}
	}
	if len(inside) == 0 {
		// Nothing can be tracked or untracked, so git is not needed.
		return tracked, false
	}
	for start := 0; start < len(inside); start += lsFilesChunk {
		end := min(start+lsFilesChunk, len(inside))
		args := append([]string{"--literal-pathspecs", "-C", root, "ls-files", "-z", "--"}, inside[start:end]...)
		out, err := exec.Command("git", args...).Output()
		if err != nil {
			// Not a checkout, or the root is gone.
			return map[string]bool{}, false
		}
		for p := range parseLsFiles(root, string(out)) {
			tracked[p] = true
		}
	}
	return tracked, true
}

// parseLsFiles turns NUL-separated `ls-files -z` output (paths relative to
// root, the -C directory) into a set of absolute paths.
func parseLsFiles(root, out string) map[string]bool {
	set := map[string]bool{}
	for _, rel := range strings.Split(out, "\x00") {
		if rel != "" {
			set[filepath.Join(root, rel)] = true
		}
	}
	return set
}

func fileExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// resolveStatuses fills in the status of every file of every group: one
// existence check per file and one git call per group, groups in parallel.
func resolveStatuses(groups []*group) {
	home := homeDir()
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, g := range groups {
		wg.Add(1)
		go func(g *group) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			exists := make([]bool, len(g.files))
			var present []string
			for i, f := range g.files {
				if exists[i] = fileExists(f.path); exists[i] {
					present = append(present, f.path)
				}
			}
			tracked, inGit := trackedFiles(g.root, present)
			for i := range g.files {
				g.files[i].status = fileStatus(g.root, g.files[i].path, home, exists[i], tracked, inGit)
			}
		}(g)
	}
	wg.Wait()
}
