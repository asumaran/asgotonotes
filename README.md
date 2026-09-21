# asgotonotes

A [herdr](https://github.com/asumaran/herdr) plugin popup for finding the
notes Claude Code left behind: plans, handoffs, drafts, reports. They are
grouped by worktree, because one worktree is one ticket no matter how many
sessions worked on it, and the ones you pick open in Zed.

Sibling of [asgotopr](https://github.com/asumaran/asgotopr) and
[asgoto](https://github.com/asumaran/asgoto): same open-pick-exit
popup, same fuzzy search.

```
╭──────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ ❯ Search by ticket, repo, branch…                                                                            │
├───────────────────────────┬──────────────────────────────────────────────────────────────────────────────────┤
│▌ ESHOP-270    2 notes   2h│ untracked 17/09 17:18  ~/wt/shop/fix-ESHOP-270-ssr/PLAN.md                       │
│  ESHOP-256    5 notes  13h│ untracked 17/09 17:12  ~/wt/shop/fix-ESHOP-270-ssr/HANDOFF.md                    │
│  ESHOP-551   20 notes  15h│                                                                                  │
│  FED-2283    29 notes   4w│                                                                                  │
├───────────────────── 4/4 ─┴──────────────────────────────────────────────────────────────────────────────────┤
│ type filter • enter files • ^a all files • f1 options • esc/q quit                                           │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
```

## Requirements

asgotonotes reads an index it does not write. A Claude Code `PostToolUse` hook
appends one line to `~/.claude/files-index/index.tsv` for every
Write/Edit/NotebookEdit call:

```
<epoch seconds> \t <worktree root> \t <repo> \t <branch> \t <absolute file path> \t <session id>
```

The file is tab-separated and has no header or quoting. Lines can repeat and
come in any order, and repo and branch can be empty. Without that hook the
popup has nothing to show. `CLAUDE_FILES_INDEX` points it at another file.

It also needs the `zed` CLI (in Zed: `cli: install`), git, macOS or Linux and
herdr >= 0.7.5.

## Install

```
herdr plugin install asumaran/asgotonotes
```

The manifest's `[[build]]` runs `scripts/fetch-binary.sh`, which downloads the
release binary matching the manifest version and falls back to `go build`
(`ASGOTONOTES_BUILD_FROM_SOURCE=1` skips the download).

Bind a key to the `open` action in `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = ["prefix+i", "ctrl+alt+i"]
type = "plugin_action"
command = "asumaran.asgotonotes.open"
description = "asgotonotes (Claude's notes per worktree)"
```

## Usage

The filter input is focused on open, so just type. A query of several words matches them in any order (`login fix` finds "fix login flow"), and a word starting with `'` must occur as typed instead of fuzzily (`'dex`). There are two views.

### Worktrees

One row per worktree root, newest write first: label, repo, number of notes
and age of the newest write. A narrow list leaves the repo out. The label is the ticket id found in
the branch or in the worktree directory name (`fix/eshop-2707-ssr` becomes
`ESHOP-2707`), otherwise `repo/branch`, otherwise the path. The right side
lists the notes of the worktree under the cursor. The filter matches the
label, the repo, the branch and the path. `enter` opens the worktree's files,
`esc` closes the popup, and so does `q` while the filter is empty.

### Files

The files of that worktree, newest write first. Each row has a status, the
time of the last write and the path. When the list is narrow the time shrinks
to an age (`3h`) and then goes away, so the file name stays readable:

| status | meaning |
| --- | --- |
| `plan` | lives under `~/.claude/plans/` |
| `tracked` | inside the worktree and tracked by git |
| `untracked` | inside the worktree, not tracked |
| `gone` | no longer exists |
| (empty) | outside the worktree: harness dirs, `/tmp` |

The right side previews the file under the cursor: markdown is rendered, other
files are shown as text, both capped at 200 lines. `enter` opens the file in a
new Zed window. `tab` or `space` marks several files first and `enter` then
opens all of them in one window. `ctrl+o` opens every listed file. `esc` goes
back to the worktrees with the cursor where you left it. Files that no longer
exist are skipped; if nothing is left to open, the popup stays up and says so.

### Both views

`ctrl+a` switches between notes and every indexed file, and the choice is
remembered. `ctrl+y` copies the
path under the cursor to the clipboard, the file in the files view and the
root of the worktree in the worktrees view; the help line confirms it for a
moment. `↑/↓` (or
`ctrl+p`/`ctrl+n`) move the cursor, PgDn/PgUp move it a page, and
`alt+↑`/`alt+↓` (or Home/End) take it to the top or the bottom of the list.
`shift+↓`/`shift+↑` scroll the preview. `f1` opens a panel with the notes / all files option, to change in place, and every key; `esc` closes it. The mouse wheel moves the cursor over the list
and scrolls the preview anywhere else. `shift+←`/`shift+→` resize
the list; the split is remembered, and the list takes a quarter of the width
by default. A click selects a row.

### What counts as a note

Everything Claude wrote is in the index, source code included, and most of it
is noise here. A file is a note when git does not track it and either its
extension is `.md`, `.markdown` or `.txt`, or it lives outside the worktree.
So `HANDOFF.md` and `PLAN.md` left untracked in a worktree are notes, as are
`~/.claude/plans/*`, harness scripts and `/tmp` drafts. README edits, tracked
code, untracked `.ts` files and deleted specs are not. Neither are Claude's
own memory files (anything under `~/.claude/projects/<project>/memory/`): they
look like notes but are not about a ticket. They are still listed with
`ctrl+a`. Worktrees without notes are hidden until `ctrl+a` shows all files.

## Behavior notes

- asgotonotes is read-only. It never edits, deletes or commits anything, and
  the only thing it runs besides git is `zed -n <files...>`, after the popup
  closes.
- Tracked or untracked is answered with one `git ls-files` call per worktree,
  restricted to that worktree's indexed files. It never lists the whole tree,
  so big monorepos stay fast. Pathspecs are literal, so `[id].tsx` is a file
  name and not a glob. The real index (about 1,650 lines, 60 roots) loads in
  around 100 ms.
- Repo and branch of a worktree come from its newest index line that has
  them, because a main clone changes branch over time.
- A removed worktree still shows up: its files are `gone` and its notes that
  live outside it (plans, harness) still open.

## Development

```bash
go build -o asgotonotes .   # local build (plugin runs ./asgotonotes from the repo root)
./asgotonotes -dump         # print the worktrees and their notes (no TTY)
./asgotonotes -dump -all    # every indexed file instead of notes only
./asgotonotes -dump -query eshop   # filtered worktrees with their scores
go vet ./... && go test ./...
scripts/pty-check.py ./asgotonotes   # end-to-end TUI check on a pty (python3 + pyte)
herdr plugin link "$PWD"   # register the working copy (no build step)
```

`ASGOTONOTES_OPENER` replaces the `zed` binary and `ASGOTONOTES_CLIPBOARD`
the clipboard command (the pty check points both at logging stubs).
`ASGOTONOTES_POPUP_WIDTH` / `ASGOTONOTES_POPUP_HEIGHT` override the
popup size from the manifest.

## Releasing

`scripts/release.sh <X.Y.Z>` gates on a clean tree + green vet/build/test,
generates the CHANGELOG entry from commit subjects, syncs the manifest
version, commits, tags and publishes the GitHub release; CI then attaches
the `asgotonotes-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64), the assets `fetch-binary.sh` downloads on installs.
