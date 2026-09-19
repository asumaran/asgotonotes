#!/usr/bin/env python3
"""End-to-end TUI check for asgotonotes without a real terminal.

Spawns the binary on a pty, answers the terminal queries bubbletea sends
(OSC 10/11, CSI 6n, DA1), replays keystrokes, and asserts on frames rendered
with pyte. Everything runs in a throwaway sandbox: a fake HOME, a synthetic
index (CLAUDE_FILES_INDEX), a real git worktree with tracked and untracked
files, and a logging stub instead of Zed (ASGOTONOTES_OPENER). It never reads
the real index and never opens an editor.

Usage: scripts/pty-check.py ./asgotonotes [dark|light]   (needs python3 + pyte)
"""
import atexit, fcntl, os, pty, re, select, shutil, signal, struct, subprocess, sys, tempfile, termios, time
import pyte

BIN = os.path.abspath(sys.argv[1])
BG = sys.argv[2] if len(sys.argv) > 2 else "dark"
ROWS, COLS = 22, 150  # the frame takes up to 8 lines; the fullest list here has 9 rows
SANDBOX = os.path.realpath(tempfile.mkdtemp(prefix="asgotonotes-pty-"))

# ---------- sandbox: fake home, git worktree, synthetic index ----------
home = os.path.join(SANDBOX, "home")
wt = os.path.join(home, "wt", "shop", "fix-ESHOP-551-structured-data")
legacy = os.path.join(home, "wt", "fed-2283-tables")
tool = os.path.join(home, "Developer", "tool")
plans = os.path.join(home, ".claude", "plans")
harness = os.path.join(home, ".claude", "harness", "eshop-551")
memdir = os.path.join(home, ".claude", "projects", "-home-wt-shop", "memory")
for d in (wt, legacy, tool, plans, harness, memdir, os.path.join(wt, "src", "pages")):
    os.makedirs(d)

def write(path, text):
    with open(path, "w") as f:
        f.write(text)
    return path

readme = write(os.path.join(wt, "README.md"), "# shop\n")
page = write(os.path.join(wt, "src", "pages", "[id].tsx"), "export default 1\n")
git_env = dict(os.environ, GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_SYSTEM="/dev/null")
def git(cwd, *args):
    subprocess.run(["git", "-C", cwd, "-c", "user.name=t", "-c", "user.email=t@t", *args],
                   check=True, capture_output=True, env=git_env)
git(wt, "init", "-q", "-b", "main")
git(wt, "add", ".")
git(wt, "commit", "-q", "-m", "init")
handoff = write(os.path.join(wt, "HANDOFF.md"), "# Handoff\n\nSome **bold** handoff text.\n")
plan_wt = write(os.path.join(wt, "PLAN.md"), "# Plan\n")
newcode = write(os.path.join(wt, "src", "new.ts"), "export {}\n")
plan = write(os.path.join(plans, "eshop-551-koala.md"), "# Koala plan\n")
script = write(os.path.join(harness, "script.py"), "print('hi')\n")
gone = os.path.join(harness, "brief-2385.md")
memory = write(os.path.join(memdir, "MEMORY.md"), "# Memory index\n")
draft = write(os.path.join(SANDBOX, "draft.md"), "# Draft\n")
tool_code = write(os.path.join(tool, "main.go"), "package main\n")
git(tool, "init", "-q", "-b", "main")
git(tool, "add", ".")
git(tool, "commit", "-q", "-m", "init")

now = int(time.time())
lines = [
    (now - 60, wt, "shop", "fix/ESHOP-551-structured-data", handoff),
    (now - 50000, wt, "shop", "fix/ESHOP-551-structured-data", handoff),  # duplicate pair
    (now - 120, wt, "shop", "fix/ESHOP-551-structured-data", plan),
    (now - 180, wt, "shop", "fix/ESHOP-551-structured-data", plan_wt),
    (now - 240, wt, "shop", "fix/ESHOP-551-structured-data", script),
    (now - 300, wt, "shop", "fix/ESHOP-551-structured-data", gone),
    (now - 360, wt, "shop", "fix/ESHOP-551-structured-data", readme),
    (now - 420, wt, "shop", "fix/ESHOP-551-structured-data", page),
    (now - 480, wt, "shop", "fix/ESHOP-551-structured-data", newcode),
    (now - 540, wt, "shop", "fix/ESHOP-551-structured-data", memory),
    (now - 86400, tool, "tool", "main", memory),
    (now - 3 * 86400, legacy, "", "", draft),
    (now - 30 * 86400, tool, "tool", "main", tool_code),
]
index = os.path.join(SANDBOX, "index.tsv")
write(index, "".join("%d\t%s\t%s\t%s\t%s\tsession\n" % l for l in lines))
write(index, open(index).read() + "garbage line without tabs\n")

opener_log = os.path.join(SANDBOX, "opener.log")
opener = os.path.join(SANDBOX, "opener")
write(opener, '#!/bin/sh\nfor a in "$@"; do printf "%%s\\n" "$a" >> "%s"; done\n' % opener_log)
os.chmod(opener, 0o755)

BGREPLY = b"\x1b]11;rgb:0000/0000/0000\x1b\\" if BG == "dark" else b"\x1b]11;rgb:ffff/ffff/ffff\x1b\\"
FGREPLY = b"\x1b]10;rgb:ffff/ffff/ffff\x1b\\" if BG == "dark" else b"\x1b]10;rgb:0000/0000/0000\x1b\\"
QUERIES = [(b"\x1b]11;?", BGREPLY), (b"\x1b]10;?", FGREPLY), (b"\x1b[6n", b"\x1b[1;1R"), (b"\x1b[c", b"\x1b[?62c")]

failures = []
def check(cond, msg):
    print(("  ok   " if cond else "  FAIL ") + msg)
    if not cond: failures.append(msg)

class Session:
    """One run of the binary on a pty."""
    def __init__(self):
        env = dict(os.environ, TERM="xterm-256color", COLORTERM="truecolor", HOME=home,
                   CLAUDE_FILES_INDEX=index, ASGOTONOTES_OPENER=opener,
                   GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_SYSTEM="/dev/null")
        for k in ("HERDR_ENV", "HERDR_PLUGIN_STATE_DIR", "XDG_CONFIG_HOME"): env.pop(k, None)   # the state dir stays under the fake HOME
        self.master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.proc = subprocess.Popen([BIN], stdin=slave, stdout=slave, stderr=slave, env=env,
                                     close_fds=True, cwd=SANDBOX)
        os.close(slave)
        # A failed assertion must not leave the binary running on a dead pty.
        atexit.register(lambda p=self.proc: p.poll() is None and p.kill())
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.raw = bytearray()
        self.answered = 0
        if os.path.exists(opener_log): os.remove(opener_log)

    def pump(self, seconds):
        end = time.time() + seconds
        while True:
            left = end - time.time()
            if left <= 0: break
            r, _, _ = select.select([self.master], [], [], left)
            if not r: continue
            try:
                data = os.read(self.master, 65536)
            except OSError:
                break
            if not data: break
            self.raw.extend(data); self.stream.feed(data)
            tail = bytes(self.raw[self.answered:])
            for q, reply in QUERIES:
                for _ in range(tail.count(q)):
                    os.write(self.master, reply)
            self.answered = len(self.raw)

    def frame(self):
        return [line.rstrip() for line in self.screen.display]

    def repaint(self):
        # The v2 renderer updates the screen with scroll regions and SU, which
        # pyte ignores; a resize forces a full redraw it can follow.
        for cols in (COLS - 1, COLS):
            fcntl.ioctl(self.master, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, cols, 0, 0))
            self.screen.resize(ROWS, cols)
            if self.proc.poll() is None: os.kill(self.proc.pid, signal.SIGWINCH)
            self.pump(0.3)

    def send(self, b, wait=0.4):
        os.write(self.master, b); self.pump(wait); self.repaint()
        return self.frame()

    def start(self):
        for _ in range(50):
            self.pump(0.1)
            if "asgotonotes (dev) ❯" in "\n".join(self.frame()): break
        self.pump(0.5)
        return self.frame()

    def finish(self):
        try:
            self.proc.wait(timeout=3)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            return None
        self.pump(0.2)
        return self.proc.returncode

    def opened(self):
        if not os.path.exists(opener_log): return []
        return open(opener_log).read().splitlines()

# One frame (see frame.go): top border, input, main edge, list | preview,
# bottom edge, help, border. View 2 adds a context line (the group header) and
# its edge on top, so everything below sits two lines lower there; the main
# edge is found by its divider joint.
INNER = COLS - 2
def listw(): return max(COLS - 3 - (COLS - 2) * 75 // 100, 10)   # the default split: list 25%, preview 75%
def divider(f): return next(l for l in f if l.startswith("├") and "┬" in l).index("┬")
SHIFT_RIGHT, SHIFT_LEFT = b"\x1b[1;2C", b"\x1b[1;2D"
def edge(f):  return next(i for i, l in enumerate(f) if l.startswith("├") and "┬" in l)
def main(f):  return f[edge(f) + 1:-3]
def left(f, top=1):  return [l[1:divider(f)].rstrip() for l in main(f)]
def right(f, top=1): return [l[divider(f) + 2:-1].rstrip() for l in main(f)]
def prompt(f):   return f[edge(f) - 1].strip("│ ").rstrip()
def context(f):  return f[1].strip("│").strip() if edge(f) == 4 else ""
def helpline(f): return f[-2]
def dump(title, f):
    print("--- %s ---" % title)
    for i, l in enumerate(f): print("%2d|%s" % (i, l))

CTRL_A, CTRL_O, ESC, ENTER, TAB, DOWN, UP = b"\x01", b"\x0f", b"\x1b", b"\r", b"\t", b"\x1b[B", b"\x1b[A"

print("== asgotonotes pty driver (%s background, %dx%d) ==" % (BG, COLS, ROWS))

# The content checks read every column, so they run with the list at half the
# width; run 4 goes back to the default split.
split_file = os.path.join(home, ".config", "herdr", "asgotonotes-tui", "split-columns")
os.makedirs(os.path.dirname(split_file), exist_ok=True)
with open(split_file, "w") as fh: fh.write("50\n")

# ---------- run 1: browse, filter, multi-select, open ----------
s = Session()
f = s.start(); dump("view 1, notes", f)
check(prompt(f) == "asgotonotes (dev) ❯", "prompt line is clean: %r" % f[1])
check(f[0].startswith("╭") and f[-1].startswith("╰") and edge(f) == 2, "view 1: one frame, input right under the top border, no title line")
check(b"\x1b[?1049h" in s.raw, "program entered the alt screen")
rows = [l for l in left(f) if l.strip()]
check(len(rows) == 2, "notes mode lists 2 groups (tool/main has only code and a memory file): %d" % len(rows))
check(rows[0].startswith("▌ ESHOP-551") and "shop" in rows[0] and "5 notes" in rows[0] and rows[0].endswith("1m"),
      "newest group first with label, repo, count and age: %r" % rows[0])
check("FED-2283" in rows[1] and " - " in rows[1] and "1 note " in rows[1] and rows[1].endswith("3d"),
      "legacy root: ticket from the dir name, '-' repo: %r" % rows[1])
pv = "\n".join(right(f))
check("HANDOFF.md" in pv and "plan" in pv and "gone" in pv and "README.md" not in pv and "new.ts" not in pv,
      "view 1 preview lists the group's notes only")
check("MEMORY.md" not in pv, "memory files are not notes")

f = s.send(CTRL_A); dump("view 1, all files", f)
rows = [l for l in left(f) if l.strip()]
check(len(rows) == 3 and "9 files" in rows[0] and "tool/main" in rows[1] and "2 files" in rows[1],
      "ctrl+a lists every group (tool/main only has code and a memory file), counts worded as files")
check("MEMORY.md" in "\n".join(right(f)), "memory files stay visible in all-files mode")
f = s.send(CTRL_A)

f = s.send(b"fed-2"); dump("view 1, query 'fed-2'", f)
rows = [l for l in left(f) if l.strip()]
check(len(rows) == 1 and rows[0].startswith("▌ FED-2283"), "typing filters the groups: %r" % rows)
f = s.send(b"\x7f" * 5)
rows = [l for l in left(f) if l.strip()]
check(len(rows) == 2 and rows[1].startswith("▌ FED-2283"), "clearing the filter keeps the cursor on the group")
f = s.send(UP)
f = s.send(ENTER, 0.8); dump("view 2, notes", f)
check(edge(f) == 4 and context(f).startswith("ESHOP-551 · ~/wt/shop/fix-ESHOP-551-structured-data · 5 notes"), "view 2 header: %r" % f[1])
check(prompt(f) == "asgotonotes (dev) ❯", "view 2 starts with an empty filter")
rows = [l for l in left(f, 2) if l.strip()]
check(len(rows) == 5, "5 notes listed: %d" % len(rows))
check("untracked" in rows[0] and rows[0].endswith("HANDOFF.md"), "untracked .md is a note: %r" % rows[0])
check(rows[1].split()[0] == "plan" and "~/.claude/plans/eshop-551-koala.md" in rows[1], "plan status: %r" % rows[1])
check("script.py" in rows[3] and "untracked" not in rows[3] and "tracked" not in rows[3],
      "file outside the worktree has no status: %r" % rows[3])
check(rows[4].split()[0] == "gone", "missing file listed as gone: %r" % rows[4])
pv = "\n".join(right(f, 2))
check("Handoff" in pv and "**bold**" not in pv and "bold handoff text" in pv, "markdown preview is rendered by glamour")

f = s.send(CTRL_A, 0.8); dump("view 2, all files", f)
rows = [l for l in left(f, 2) if l.strip()]
check(len(rows) == 9 and "9 files" in context(f), "ctrl+a lists all 9 files")
check(any(l.endswith("memory/MEMORY.md") for l in rows), "the memory file is listed in all-files mode")
tracked = [l for l in rows if l.split()[0] == "tracked"]
check(len(tracked) == 2 and any("[id].tsx" in l for l in tracked),
      "git lookup marks tracked files, literal pathspec for [id].tsx: %r" % tracked)
check(any(l.split()[0] == "untracked" and l.endswith("new.ts") for l in rows), "untracked code shows in all-files mode")
f = s.send(CTRL_A, 0.6)

f = s.send(DOWN * 4, 0.6); dump("cursor on the gone file", f)
check(any("gone: ~/.claude/harness/eshop-551/brief-2385.md" in l for l in right(f, 2)), "gone file preview says so")
f = s.send(ENTER)
check(s.proc.poll() is None and "no longer exists" in helpline(f), "enter on a gone file stays open with a notice: %r" % helpline(f))

f = s.send(UP * 4)
f = s.send(TAB + TAB); dump("two files marked", f)
check("2 selected" in context(f) and sum("●" in l for l in left(f, 2)) == 2, "tab marks files and moves down")
s.send(ENTER, 0.2)
rc = s.finish()
check(rc == 0, "clean exit after enter: %r" % rc)
check(s.opened() == ["-n", handoff, plan], "opener got -n and the 2 marked files: %r" % s.opened())
check(b"\x1b[?1049l" in s.raw, "program left the alt screen")

# ---------- run 2: esc goes back, ctrl+o opens all and skips missing ----------
s = Session()
s.start()
f = s.send(DOWN + ENTER, 0.6)
check(context(f).startswith("FED-2283 · ~/wt/fed-2283-tables · 1 note"), "second group opens: %r" % f[1])
f = s.send(ESC); dump("back in view 1", f)
rows = [l for l in left(f) if l.strip()]
check(s.proc.poll() is None and rows[1].startswith("▌ FED-2283"), "esc goes back and keeps the cursor on the group")
f = s.send(UP + ENTER, 0.6)
s.send(CTRL_O, 0.2)
rc = s.finish()
tail = s.raw.decode("utf-8", "replace")
check(rc == 0, "clean exit after ctrl+o: %r" % rc)
check(s.opened() == ["-n", handoff, plan, plan_wt, script], "ctrl+o opens every existing note: %r" % s.opened())
check("asgotonotes: skipped 1 file(s) that no longer exist" in tail, "skipped files are reported on stderr")

# ---------- run 3: q quits, nothing is opened ----------
s = Session()
s.start()
s.send(b"q", 0.2)
check(s.finish() == 0 and s.opened() == [], "q quits with an empty filter and opens nothing")

# ---------- run 4: the divider moves and stays where it was left ----------
os.remove(split_file)
s = Session()
f = s.start(); at = divider(f)
check(at == listw() + 1, "without a saved split the list takes a quarter: %d" % at)
rows = [l for l in left(f) if l.strip()]
check(rows[0].startswith("▌ ESHOP-551") and "5 notes" in rows[0] and rows[0].endswith("1m"),
      "a narrow list drops the repo column, not the count or the age: %r" % rows[0])
f = s.send(ENTER, 0.6)
rows = [l for l in left(f) if l.strip()]
check(re.match(r"^▌ untracked +\d+[smhd]  …\S*/HANDOFF\.md$", rows[0].rstrip()),
      "a narrow list shrinks the date to an age and keeps the file name: %r" % rows[0])
f = s.send(ESC, 0.6)
f = s.send(SHIFT_RIGHT, 0.6); grown = divider(f)
check(grown > at and all(len(l) == COLS for l in f), "shift+right grows the list: %d -> %d" % (at, grown))
f = s.send(SHIFT_LEFT, 0.6)
check(divider(f) == at, "shift+left shrinks it back: %d" % divider(f))
s.send(SHIFT_RIGHT, 0.6)
s.send(b"q", 0.2); s.finish()
s = Session()
f = s.start()
check(divider(f) == grown, "the next run opens with the same split: %d" % divider(f))
s.send(SHIFT_LEFT, 0.6)
s.send(b"q", 0.2); s.finish()

shutil.rmtree(SANDBOX, ignore_errors=True)
print("\n%d failure(s)" % len(failures))
sys.exit(1 if failures else 0)
