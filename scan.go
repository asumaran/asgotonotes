package main

// The second source. The index only knows the files Claude wrote through
// Write/Edit: a note made from the shell (`cat > x <<EOF`, `sed -i`) never
// reaches it. Claude's own note folders are small enough to walk, so what is
// on disk there and missing from the index is added as records of its own,
// before the groups are built. The index stays the only thing that knows
// which worktree a file belongs to, so a found file takes its owner from the
// indexed files around it.

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// scanDirs are the folders walked for notes the index missed.
func scanDirs(home string) []string {
	if home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".claude", "harness"),
		filepath.Join(home, ".claude", "plans"),
	}
}

// scanned is one note found on disk.
type scanned struct {
	path  string
	mtime int64
}

// skipDir reports whether a folder is not worth walking.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "__pycache__" || name == "node_modules"
}

// scanNotes walks dirs for note-shaped files. Only the extension tells a note
// here: these folders also hold evidence, captures and scripts by the
// hundred, and outside a worktree the note rule takes any of them. A folder
// that is missing or cannot be read adds nothing.
func scanNotes(dirs []string) []scanned {
	var found []scanned
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if p != dir && skipDir(d.Name()) {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || !hasNoteExt(p) {
				return nil
			}
			if info, err := d.Info(); err == nil {
				found = append(found, scanned{path: p, mtime: info.ModTime().Unix()})
			}
			return nil
		})
	}
	return found
}

// adopt turns the found files the index does not have into records. A file
// goes to the root with the most index lines in its own folder (a tie goes to
// the newest line), else in the nearest folder above that has any. The
// scanned folder itself never gives an owner: loose plans of every ticket sit
// together there. A file nobody owns groups under its first folder inside the
// scanned one, or under the scanned folder when it is loose. The records
// carry no repo or branch, so the group keeps the ones the index gave it.
func adopt(recs []record, found []scanned, dirs []string) []record {
	type tally struct {
		n      int
		newest int64
	}
	indexed := map[string]bool{}
	owners := map[string]map[string]*tally{} // folder -> root -> its lines
	for _, r := range recs {
		indexed[r.path] = true
		dir := filepath.Dir(r.path)
		if owners[dir] == nil {
			owners[dir] = map[string]*tally{}
		}
		t := owners[dir][r.root]
		if t == nil {
			t = &tally{newest: r.ts}
			owners[dir][r.root] = t
		}
		t.n++
		t.newest = max(t.newest, r.ts)
	}
	owner := func(dir string) string {
		best, bt := "", &tally{}
		for root, t := range owners[dir] {
			if t.n > bt.n || t.n == bt.n && (t.newest > bt.newest || t.newest == bt.newest && root < best) {
				best, bt = root, t
			}
		}
		return best
	}

	var out []record
	for _, f := range found {
		if indexed[f.path] {
			continue
		}
		base := ""
		for _, d := range dirs {
			if insideRoot(d, f.path) {
				base = d
				break
			}
		}
		if base == "" {
			continue
		}
		root := ""
		for dir := filepath.Dir(f.path); root == "" && dir != base; dir = filepath.Dir(dir) {
			root = owner(dir)
		}
		if root == "" {
			root = base
			if first, _, nested := strings.Cut(strings.TrimPrefix(f.path, base+"/"), "/"); nested {
				root = filepath.Join(base, first)
			}
		}
		out = append(out, record{ts: f.mtime, root: root, path: f.path})
	}
	return out
}
