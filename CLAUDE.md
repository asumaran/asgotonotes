# CLAUDE.md

Guidance for working in this repository.

## What this is

`asgotonotes` is a herdr plugin popup that browses the notes Claude Code wrote
(plans, handoffs, drafts, reports), grouped by worktree root (one worktree =
one ticket, however many sessions worked on it), and opens the chosen files
in Zed. Two views: worktree groups, then the files of the chosen group. Open,
pick, exit: same lifecycle and look as `asgotopr` and `asgoto`, which this
repo is modeled on.

It is read-only. It never edits, deletes or commits anything, uses no network
and writes nothing except a one-line stderr note about skipped files.

Distributed as a herdr plugin (`herdr plugin install asumaran/asgotonotes`; the
manifest's `[[build]]` runs `scripts/fetch-binary.sh`). Each GitHub Release
attaches the `asgotonotes-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64). There is no published library.

## Data source

`~/.claude/files-index/index.tsv` (override: `CLAUDE_FILES_INDEX`), appended
by a Claude Code PostToolUse hook that lives in the user's dotfiles, not
here. One line per Write/Edit/NotebookEdit, tab-separated, no header, no
quoting, unsorted, with repeated (root, file) pairs:

```
<epoch> \t <worktree root> \t <repo> \t <branch> \t <absolute file path> \t <session id>
```

The root is the group key and arrives already resolved (the worktree the file
lives in; for files outside any worktree, the worktree of the session cwd;
outside git, the cwd). `repo` is empty for legacy `~/wt/<branch>` roots and
`branch` may be empty. The format is owned by the hook: do not change it from
here. Indexed files may no longer exist; they are listed as `gone`, never
crash anything and are skipped when opening.

## Stack & layout

Go single module, single `package main`, static binary. TUI: Bubble Tea v2 +
bubbles v2 (`textinput`, `viewport`, `key`, `help`), lipgloss v2,
`sahilm/fuzzy` for matching, glamour v2 for the markdown preview. The charm
v2 modules are imported under their canonical `charm.land/<name>/v2` paths
(the `github.com/charmbracelet/<name>/v2` spelling is rejected by `go get`).
Files are split by concern:

- `main.go` — flags (`-version`, `-dump`, `-all`, `-query`), `loadGroups`,
  `tea.NewProgram`, post-quit `runOpen`, `runDump`.
- `index.go` — pure logic: `parseIndex`, `buildGroups` (dedup, newest first),
  `groupLabel`, `fileStatus`, the note rule `isNote` / `isMemoryFile`,
  `visibleGroups`,
  `compactAge`, `tildePath`.
- `git.go` — `trackedFiles` (the one-call-per-group lookup), `parseLsFiles`,
  `resolveStatuses` (groups in parallel).
- `filter.go` — fuzzy rows for both views.
- `match.go` — `findTight`, the fuzzy matcher with one correction: it is
  greedy (first candidate for each rune, left to right), so a query that
  occurs in one piece could still match scattered letters before it. When the
  query occurs whole, that occurrence is the match, for the highlight and for
  the score. The same file in every tool of the family.
- `highlight.go` — `highlight`/`highlightFrom`, `matchOver`, `onSel`,
  `selPad` and the `stSel`/`stMatch` styles: how a match and the selected row
  look. The same file in every tool of the family.
- `pathcells.go` — `pathCells`, `pathTail`, `tailCut`: a path cut to a width
  by its head, its prefix dimmed, its matches marked. The same file in every
  tool that lists paths.
- `frame.go` — the single-frame layout shared by the family: `hline`, `fit`,
  `framed`, `frameHead`, `splitMain`, `scrollPos` and the section rows (`mainY`,
  `listY`, `frameRows`, each with or without the optional context line).
- `split.go` — the divider between the list and the preview: `loadSplit`,
  `saveSplit`, `stepSplit`, `splitWidths`. The file is copied, not imported:
  the same one ships in asgotochanged, asgotosession, asgotopr and asgotoissues (all
  under github.com/asumaran), and there is no shared library. A pull request
  only needs to change it here; the maintainer ports the change to the other
  copies.
- `ui.go` — the bubbletea model/Update/View, the two views, multi-select,
  opening, mouse, styles, `pathCells`.
- `preview.go` — file preview as a `tea.Cmd` (glamour for markdown, plain text
  otherwise, `gone:` when missing), 200-line cap, render cache.
- `scripts/pty-check.py` — end-to-end TUI driver (see Testing).

## Build & run

```bash
go build -o asgotonotes .    # plugin runs ./asgotonotes from the repo root
./asgotonotes -dump          # groups + notes as the popup would list them, no TTY
./asgotonotes -dump -all     # every indexed file
./asgotonotes -dump -query x # filtered groups with scores
go vet ./... && go test ./...
herdr plugin link "$PWD"   # link does NOT run [[build]]; go build yourself
```

Keybinding (user config): `prefix+i` / `ctrl+alt+i` → `plugin_action`
`asumaran.asgotonotes.open` → `scripts/open-pane.sh` → `herdr plugin pane open`.

## Behaviour / decisions

- **Layout**: one rounded frame of sections split by shared edges, the layout
  asgitlog introduced and every picker of the family follows (`frame.go`, the
  same file in each repo): the filter input (the border over it carries the
  matches/total counter), the main section (list and preview split by a
  divider; its bottom edge carries the list's position on the left and the
  preview's scroll position on the right, each only while its side overflows),
  and the help. A context line on top is only for what the rest of the screen
  cannot say; a title is not context. View 1 has none; view 2 has one, the
  group being browsed, so `listY`, `mainY` and `frameRows` take `hasContext()`
  and the two views differ by two lines. The list starts on screen row
  `listY`, one cell in from the left side, which is what the click-to-row math
  uses. Errors and notices take the help line.
- **Filter matches** look the same in every tool of the family and come from
  one place, `highlight.go` (the same file in each repo; it also owns `stSel`
  and `stMatch`): a match is the match color plus an underline on top of the
  style the text already has, and the selected row shows them too. That row
  is never one big `stSel.Render` around styled text, because the reset that
  ends a match would cut the background: every piece is rendered over `stSel`
  (`highlight(s, idx, stSel)`) and `selPad` fills the rest. Do not write a
  local highlighter.
- **Resizable list**: `shift+←/→` move the divider in 5% steps, as in
  asgitlog. The setting is the PREVIEW's share of the width, clamped to
  30-85 and saved as `split-columns` in the state dir; the default is 75
  (list 25%, preview 75%), the same in every picker of the family. Rows
  must degrade for a narrow list instead of truncating their last columns.
  A list too narrow for the four columns of a group row drops the repo
  (`groupLine`). `stateDir()` lives in `index.go`.
- **Group label**: first `[A-Za-z]{2,}-[0-9]+` match in `branch + " " +
  basename(root)`, uppercased; else `repo/branch`; else the root with `~`.
  Repo and branch come from the newest index line that has them (a main clone
  changes branch over time; legacy lines lack the repo).
- **Status**, in this order: `gone` (does not exist), `plan` (under
  `~/.claude/plans/`), `tracked`, `untracked` (inside a root that is a git
  checkout), else empty (outside the worktree, or the root is not in git).
- **Note rule** (default filter): not tracked AND not a memory file AND
  (`.md`/`.markdown`/`.txt` OR status empty/plan). Memory files are anything
  under `~/.claude/projects/<project>/memory/` (`isMemoryFile`): indexed and
  listed in all-files mode, never counted as notes. Same rule as `_cf_is_note`
  in the dotfiles zsh prototype. `ctrl+a` toggles notes / all files and persists across
  both views. Groups with zero notes are hidden in notes mode.
- **Tracked lookup**: ONE `git --literal-pathspecs -C <root> ls-files -z --
  <files inside root>` per group, never a full `ls-files` of the tree (the
  work monorepos are huge). Literal pathspecs keep `[id].tsx` from being read
  as a glob. Output paths are relative to `<root>`. A group with no existing
  file inside its root runs no git at all. A failing call (not a checkout,
  root removed) means "not in git". All groups resolve in parallel before the
  first frame (about 100 ms for 60 roots) and the result lives for the popup
  lifetime; there is no on-disk cache.
- **A query makes the list a search result**: rows are ranked, best match
  first, and the cursor sits on the first one (`rank` in `rank.go`, the same
  file in every picker of the family). Equal scores keep the list's own
  order, newest first, which is also the order without a query. A score says how
  good the match is and nothing about the length of the text (`match.go`).
  View 1 matches label, repo and a
  hidden corpus (branch + root); view 2 matches the `~` path and the status.
  Matched indexes from `sahilm/fuzzy` are BYTE offsets. With an empty query
  the cursor stays on the row it was on (`refilter`).
- **Views**: enter stashes the view 1 query and opens view 2 with an empty
  one; esc restores the query and puts the cursor back on the same group.
  The group header of view 2 is the frame's context line; view 1 has none, so
  `listY` and `bodyH` depend on the view (`hasContext()`).
- **Keys vs. filter**: every printable key filters, so `q` quits only while
  the filter is empty, and `space` marks in view 2 instead of typing. `ctrl+a`
  is intercepted before the textinput (which would treat it as line-start).
- **Paths** that do not fit lose their head, not their tail (`pathCells`), so
  the file name is always visible; the root prefix is dimmed.
- **Opening** happens after the TUI quits (quitting closes the popup and
  anything printed after that is lost): `zed -n <files...>`, one new window.
  Existence is re-checked at open time. If nothing chosen still exists, or
  the `zed` CLI cannot be found, the popup stays open with a footer notice
  instead of quitting. Enter opens the multi-selection in list order (even
  rows the filter currently hides), else the cursor file; `ctrl+o` opens the
  rows currently listed. `openerPath` also tries the usual zed locations since
  the popup may run with a shorter PATH; `ASGOTONOTES_OPENER` replaces it.
- **Preview**: view 1 is synchronous (the group's file rows). View 2 reads at
  most 512 KB / 200 lines off the update loop, caches per (path, width,
  mtime, style), shows `(binary file)` on NUL bytes. A leading YAML
  frontmatter block (memory files, plans) is shown dimmed as plain text,
  because glamour renders it as a rule plus a heading.
- **Never query the terminal behind bubbletea's back**: `Init` issues
  `tea.RequestBackgroundColor()` and the `tea.BackgroundColorMsg` reply picks
  the glamour style ("dark"/"light"). Frames before the reply use "dark";
  when the style flips, the render cache is dropped and the current preview
  re-renders. Don't call glamour's `WithAutoStyle` or lipgloss's
  `HasDarkBackground` from inside the program.
- **Mouse**: the wheel follows the pointer, as in asgitlog: over the list
  (`overList`) it moves the cursor through the same code as the arrow keys,
  anywhere else it scrolls the preview. A left click on a list row moves the
  cursor and never opens anything.
- **Alt screen and mouse mode** are declared per frame in `View()`; there is
  no `tea.WithAltScreen` program option in v2.

## Testing

Unit tests cover the pure logic with fixtures and no git: index parsing,
grouping, labels, statuses, the note rule, ages, `ls-files` output parsing,
filtering, preview rendering, view transitions, multi-select and opening.

For end-to-end verification without a TTY, `scripts/pty-check.py ./asgotonotes
[dark|light]` (python3 + `pyte`) spawns the binary on a pty, answers the OSC
10/11 + CSI 6n + DA1 queries, replays keystrokes and asserts on pyte-rendered
frames. It runs in a throwaway sandbox (fake `HOME`, synthetic index via
`CLAUDE_FILES_INDEX`, a real git worktree with tracked/untracked files, a
logging stub as `ASGOTONOTES_OPENER`), so it never reads the real index and
never launches Zed.

## Commits & branches

- Conventional Commits: `type(scope): description`.
- Never mention AI tooling in commits, PRs, or any repo-visible text as the
  author of changes.
- Default branch is `main`. Don't commit, tag, or push unless explicitly
  asked (releasing is an explicit, separate request).
- `HANDOFF.md` and `REPORT.md` at the root are local notes and are gitignored.

## Releasing

`scripts/release.sh <X.Y.Z>` — clean-tree + vet/build/test gate, CHANGELOG
generation from commit subjects, manifest version sync, commit + tag + GitHub
release; CI (`.github/workflows/release.yml`) attaches
the `asgotonotes-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64). Releasing never touches the linked plugin's
`./asgotonotes`; rebuild locally to keep testing dev code.
