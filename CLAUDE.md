# CLAUDE.md

Guidance for working in this repository.

## What this is

`asgotonotes` is a herdr plugin popup that browses the notes Claude Code wrote
(plans, handoffs, drafts, reports), grouped by worktree root (one worktree =
one ticket, however many sessions worked on it), and opens the chosen files
in Zed. Two views: worktree groups, then the files of the chosen group. Open,
pick, exit: same lifecycle and look as `asgotopr` and `asgoto`, which this
repo is modeled on.

It only reads the notes. It never edits, deletes or commits anything and uses
no network. What it writes is its own: its settings (the panel's option and
the list size) in the state dir.

Distributed as a herdr plugin (`herdr plugin install asumaran/asgotonotes`; the
manifest's `[[build]]` runs `scripts/fetch-binary.sh`). Each GitHub Release
attaches the `asgotonotes-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64). There is no published library.

## Data source

`~/.claude/files-index/index.tsv` (override: `CLAUDE_FILES_INDEX`), appended
by a Claude Code PostToolUse hook that is not part of this repository. One line per Write/Edit/NotebookEdit, tab-separated, no header, no
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

- `main.go`: flags (`-version`, `-dump`, `-all`, `-query`), `loadGroups`,
  `tea.NewProgram`, post-quit `runOpen`, `runDump`.
- `index.go`: pure logic: `parseIndex`, `buildGroups` (dedup, newest first),
  `groupLabel`, `fileStatus`, the note rule `isNote` / `isMemoryFile`,
  `visibleGroups`, `stateDir()`.
- `git.go`: `trackedFiles` (the one-call-per-group lookup), `parseLsFiles`,
  `resolveStatuses` (groups in parallel).
- `filter.go`: fuzzy rows for both views.
- `match.go`: `findTight`/`tighten`, the fuzzy matcher with one correction: it
  is greedy (first candidate for each rune, left to right), so a query that
  occurs in one piece could still match scattered letters before it. When the
  query occurs whole, that occurrence is the match, for the highlight and for
  the score. `hasTerms` says whether a query searches for anything: spaces and
  a bare `~` or `'` do not, so they never filter, rank or move the cursor. The
  same file in every tool of the family.
- `text.go`: `truncate`, `padRight`, `padLeft`: fitting text, styled or not,
  into cells. The same file in every tool of the family.
- `statedir.go`: `stateDirFor`: the state dir herdr injects
  (`HERDR_PLUGIN_STATE_DIR`) or, when the tool runs on its own, the same
  directory worked out
  (`${XDG_STATE_HOME:-~/.local/state}/herdr/plugins/asumaran.asgotonotes`), so
  the popup and a run from the shell share settings and caches. The same file
  in every tool of the family.
- `setting.go`: `loadSetting`, `saveSetting`: a setting the tool remembers,
  one plain-text file each in the state dir. Every option of the panel is
  kept this way, per tool. The same file in every tool of the family that
  needs it.
- `age.go`: `compactAge` (`5m`, `3h`, `2d`, `6w`, `2y`) for a list column and
  `relTime` (`3h ago`) for a sentence. The same file in every tool of the
  family that shows an age.
- `markdown.go`: `renderMarkdown` (glamour with a fixed style, never
  auto-detected), `glamourStyle` and `setPreviewStyle`. The same file in every
  tool of the family that renders Markdown.
- `listmouse.go`: `inList`, `rowUnder`, `wheelKey`: the mouse over the list.
  The wheel goes through the same code as the arrows; a click moves the
  cursor and never opens anything. The same file in every tool of the family.
- `prompt.go`: the filter input: its prompt (with the tool's name only outside
  herdr's popup), the placeholder, the `(dev)` mark on the edge over the
  input. `typeInto` hands a message to the input and reports whether the query
  changed: a key, a terminal paste and the input's own `ctrl+v` all edit it,
  and the caller filters again only when it did. The same file in every tool
  of the family.
- `helpfoot.go`: the help line at the foot, cut to the width, and the key that
  opens the panel. `footLine` is what the foot shows: a flash first, then a
  notice in the error color, else the help. The same file in every tool of the
  family.
- `panel.go`: the panel `f1` opens over the frame: options to change in
  place and every key under them (`option`, `panel`, `panelLines`,
  `overlay`). The same file in every tool of the family.
- `listnav.go`: `listNav`: the keys that move the cursor through a list and
  where each one takes it, group headers skipped. `scrollTo` keeps the cursor
  in view together with the row `withHeader` names: the header of its group
  when that is the row right above. `emptyList` is what a list says instead of
  rows: the error that kept it from loading, in the error color, `No matches`,
  or the tool's own reason. The same file in every tool of the family.
- `highlight.go`: `highlight`/`highlightFrom`, `matchOver`, `onSel`,
  `selPad` and the `stSel`/`stMatch` styles: how a match and the selected row
  look. The same file in every tool of the family.
- `flash.go`: `flash`, `flashMsg`, `clearFlashMsg`: a confirmation that takes
  the help line for a moment. The same file in every tool of the family.
- `clipboard.go`: `copyCmd`: feeds a text to the system clipboard and reports
  it with a `flashMsg`; `ASGOTONOTES_CLIPBOARD` replaces the command. The same
  file in every tool of the family.
- `border.go`: `hline`, `framed`, `fit`, `scrollPos`: the primitives the frame
  is drawn with (an edge with texts set into it, a line between the frame's
  sides, the position a scrolled viewport reports on an edge). `fitLines` is
  content as exactly so many lines of a width, and `popupView` is the
  `tea.View` every tool returns: the alt screen and, while the mouse is on,
  cell-motion mouse reports. The same file in every tool of the family.
- `homepath.go`: `tildePath`, `homeDir`, `homeRel`: a path with the home
  directory abbreviated to `~`. The same file in every tool of the family that
  shows paths.
- `rank.go`: `rank`: with a query the list is a search result, best score
  first; in a grouped list the groups go by their best item and keep their
  items together, and equal scores keep the list's own order. The same file in
  every tool of the family that ranks its matches.
- `ticket.go`: `ticketFrom`: the ticket key (`KEY-123`, uppercased) found in a
  branch name, a title or a folder name. The same file in every tool of the
  family that needs it.
- `pathcells.go`: `pathCells`, `pathTail`, `tailCut`: a path cut to a width
  by its head, its prefix dimmed, its matches marked. The same file in every
  tool that lists paths.
- `frame.go`: the single-frame layout the pickers share: `frameHead`,
  `splitMain` (list and preview) and the section rows (`mainY`, `listY`,
  `frameRows`, each with or without the optional context line), drawn with the
  primitives of `border.go`. Copied, not imported: the same file ships in
  asgoto, asgotopr, asgotoissues, asgotosession and asgotochanged (all under
  github.com/asumaran), and there is no shared library. A pull request only
  needs to change it here; the maintainer ports the change to the other
  copies.
- `split.go`: the divider between the list and the preview: `loadSplit`,
  `saveSplit`, `stepSplit`, `splitWidths`, `moveSplit` (one step, remembered)
  and `sizePanes` (the list and the preview get their share of the main
  section). Copied, not imported, like `frame.go`: the same file ships in
  asgotopr, asgotoissues, asgotosession and asgotochanged.
- `ui.go`: the bubbletea model/Update/View, the two views, multi-select,
  opening, mouse, styles.
- `preview.go`: file preview as a `tea.Cmd` (glamour for markdown, plain text
  otherwise, `gone:` when missing), 200-line cap, render cache.
- `scripts/pty-check.py`: end-to-end TUI driver (see Testing).

## Build & run

```bash
go build -o asgotonotes .    # plugin runs ./asgotonotes from the repo root
./asgotonotes -dump          # groups + notes as the popup would list them, no TTY
./asgotonotes -dump -all     # every indexed file
./asgotonotes -dump -query x # filtered groups with scores
go vet ./... && go test ./...
scripts/pty-check.py ./asgotonotes   # end-to-end TUI check on a pty (python3 + pyte)
herdr plugin link "$PWD"   # link does NOT run [[build]]; go build yourself
```

Keybinding (user config): `prefix+i` / `ctrl+alt+i` → `plugin_action`
`asumaran.asgotonotes.open` → `scripts/open-pane.sh` → `herdr plugin pane open`.

## Behaviour / decisions

- **Layout**: one rounded frame of sections split by shared edges, the layout
  asgitlog introduced and every picker of the family follows (`frame.go`, the
  same file in each repo): the filter input, the main section (list and preview split by a divider; its
  bottom edge carries the matches/total counter under the list and, while the
  preview overflows, its scroll position on the right),
  and the help. A context line on top is only for what the rest of the screen
  cannot say; a title is not context. View 1 has none; view 2 has one, the
  group being browsed, so `listY`, `mainY` and `frameRows` take `hasContext()`
  and the two views differ by two lines. The list starts on screen row
  `listY`, one cell in from the left side, which is what the click-to-row math
  uses. Errors and notices take the help line.
- **Moving through the list** is the same in every tool of the family and
  comes from `listnav.go` (the same file in each repo): arrows or
  `ctrl+p`/`ctrl+n` a row, `pgup`/`pgdn` a page, `alt+↑`/`alt+↓` or
  `home`/`end` the ends. `home`/`end` are taken from the filter input's caret
  on purpose (`←`/`→` and `ctrl+e` still move it). The preview scrolls with
  `shift+↑`/`shift+↓` only. The keys are listed in the panel.
- **The filter input** comes from `prompt.go` (the same file in every tool of
  the family). Inside herdr's popup the prompt is the arrow alone, because the
  pane's title (`[[panes]] title` in the manifest, the tool's name) already
  says which tool it is, and a placeholder says what the filter searches. Run
  on its own the prompt carries the tool's name. A build that is not a release
  says `(dev)` on the edge over the input, never inside the
  prompt.
  herdr sets `HERDR_PLUGIN_ENTRYPOINT_ID` for a plugin pane; that is how the
  two cases are told apart.
  Whatever reaches the input goes through `toInput`: a key, a paste from the
  terminal (`tea.PasteMsg`) and the input's own `ctrl+v` filter the list the
  same way (`typeInto`), and a message that leaves the query alone (a caret
  move, the blink) never moves the cursor. A paste under the open panel is
  dropped. A query made only of spaces, or a bare `~` or `'`, is not a query
  (`hasTerms`): it does not filter, rank or move the cursor.
- **Help and options**: the line at the foot shows the tool's own actions,
  the panel's key and the quit keys (`helpfoot.go`). `f1` opens the panel (`panel.go`, the same file in
  every tool of the family): the options on top, to change with `←`/`→` or
  `space`, and every key in columns under them, laid out by bubbles' `help`
  from `FullHelp()`. The panel is spliced over the middle of the frame, which
  keeps its size; while it is open it takes every key and the mouse, and `esc`
  closes it before it does anything else. `?` is not a help key: the filter
  has the focus, so it is text. Moving, scrolling and resizing are listed in
  the panel only, so the help line stays short enough for a narrow popup. A
  message (error, notice) takes the help line's place.
  This tool's options are notes only or all files: `options()` lists them as things stand and
  `setOption` is the one place that changes a setting, for the panel and for
  the keys that kept a shortcut. A setting that is chosen once has no key of
  its own; the panel is where it lives.
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
  listed in all-files mode, never counted as notes. `ctrl+a` toggles notes / all files; the choice holds across
  both views and is remembered between runs (setting `files`). Groups with zero notes are hidden in notes mode.
- **Tracked lookup**: ONE `git --literal-pathspecs -C <root> ls-files -z --
  <files inside root>` per group, never a full `ls-files` of the tree (a
  monorepo's is huge). Literal pathspecs keep `[id].tsx` from being read
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
- **Copy**: `ctrl+y` copies the absolute path under the cursor in both views
  (the file, or the root of the worktree) with `copyCmd` (the shared
  `clipboard.go`) and the help line flashes `copied <~ path>` (`flash.go`).
  `ASGOTONOTES_CLIPBOARD` replaces the clipboard command (the tests point it
  at a stub).
- **Settings**: notes only or all files is one file in the state dir
  (`files`; `setting.go`, the same file in every tool of the family that
  remembers an option), next to the divider's `split-columns`.
- **Paths** that do not fit lose their head, not their tail (`pathCells`), so
  the file name is always visible; the root prefix is dimmed.
- **Opening** happens after the TUI quits (quitting closes the popup and
  anything printed after that is lost): `zed -n <files...>`, one new window.
  Existence is re-checked at open time. If nothing chosen still exists, or
  the `zed` CLI cannot be found, the popup stays open with a footer notice
  instead of quitting. Enter opens the multi-selection in list order (even
  rows the filter currently hides), else the cursor file; `ctrl+o` opens the
  rows currently listed. `openerCmd` also tries the usual zed locations since
  the popup may run with a shorter PATH; `ASGOTONOTES_OPENER` replaces the
  whole `zed -n` command, as words (`opener.go`).
- **Preview**: view 1 is synchronous (the group's file rows). View 2 reads at
  most 512 KB / 200 lines off the update loop, caches per (path, width,
  mtime) and drops the cache when the glamour style flips
  (`setPreviewStyle`), shows `(binary file)` on NUL bytes. A leading YAML
  frontmatter block (memory files, plans) is shown dimmed as plain text,
  because glamour renders it as a rule plus a heading.
- **Errors**: an index that cannot be read is shown in the list in the error
  color (`loadErr` through the shared `emptyList`), as in every tool of the
  family; the popup still opens. Notices (nothing left to open, no `zed`)
  take the help line in the error color until the next key (`footLine`).
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
`CLAUDE_FILES_INDEX`, a real git worktree with tracked/untracked files,
logging stubs as `ASGOTONOTES_OPENER` and `ASGOTONOTES_CLIPBOARD`), so it
never reads the real index, never launches Zed and never touches the real
clipboard.

## Commits & branches

- Conventional Commits: `type(scope): description` (feat, fix, chore, docs,
  style, refactor, test, perf).
- Never mention AI tooling in commits, PRs, or any repo-visible text as the
  author of changes.
- Default branch is `main`. Don't commit, tag, or push unless explicitly
  asked (releasing is an explicit, separate request).
- `HANDOFF.md` and `REPORT.md` at the root are local notes and are gitignored.

## Releasing

`scripts/release.sh <X.Y.Z>`: clean-tree + vet/build/test gate, CHANGELOG
generation from commit subjects, manifest version sync, commit + tag + GitHub
release; CI (`.github/workflows/release.yml`) attaches
the `asgotonotes-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64). Releasing never touches the linked plugin's
`./asgotonotes`; rebuild locally to keep testing dev code.
