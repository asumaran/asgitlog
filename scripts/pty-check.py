#!/usr/bin/env python3
"""End-to-end TUI check for asgitlog without a real terminal.

Spawns the binary on a pty inside a throwaway git repository, answers the
terminal queries bubbletea sends (OSC 10/11, CSI 6n, DA1), replays keystrokes,
SGR mouse reports and resizes, and asserts on frames rendered with pyte. The
settings and the render cache go to a sandboxed state dir and XDG_CACHE_HOME
and the clipboard/browser to logging stubs, so nothing real is touched.

Usage: scripts/pty-check.py ./asgitlog   (needs python3 + pyte and git; delta and hunk are
used when installed: without delta the diffs are plain git, as on a CI runner)
"""
import fcntl, json, os, pty, re, select, shutil, signal, struct, subprocess, sys, tempfile, termios, time
import pyte

BIN = os.path.abspath(sys.argv[1])
ROWS, COLS = 40, 160
SANDBOX = os.path.realpath(tempfile.mkdtemp(prefix="asgitlog-pty-"))
REPO = os.path.join(SANDBOX, "repo")
STATE = os.path.join(SANDBOX, "state")
CACHE = os.path.join(SANDBOX, "cache")
LINEAR = 60            # commits on the straight part of main
ON_MAIN = LINEAR + 2   # + the side branch's commit and its merge
ALL = ON_MAIN + 1      # + a branch that was never merged

# ---------- sandbox: a repository with a predictable history ----------
GIT_ENV = dict(os.environ, GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_SYSTEM="/dev/null",
               GIT_AUTHOR_NAME="Ada Lovelace", GIT_AUTHOR_EMAIL="ada@example.com",
               GIT_COMMITTER_NAME="Ada Lovelace", GIT_COMMITTER_EMAIL="ada@example.com")

def git(*args, date=None):
    env = dict(GIT_ENV)
    if date: env["GIT_AUTHOR_DATE"] = env["GIT_COMMITTER_DATE"] = date
    return subprocess.run(["git", "-C", REPO] + list(args), check=True, env=env, capture_output=True, text=True).stdout.strip()

def write(name, text):
    with open(os.path.join(REPO, name), "w") as f: f.write(text)

def file_txt(i):
    return "".join("line %02d of revision %02d\n" % (n, i if n % 10 == i % 10 else 0) for n in range(80))

os.makedirs(REPO); os.makedirs(os.path.join(STATE, "asgitlog"))
# hunk is the default renderer; these checks read delta's output, so delta is the saved choice.
with open(os.path.join(STATE, "asgitlog", "renderer"), "w") as f: f.write("delta")
git("init", "-q", "-b", "main")
git("remote", "add", "origin", "git@github.com:acme/widgets.git")
for i in range(LINEAR):
    write("file.txt", file_txt(i))
    if i % 5 == 0: write("other.txt", "other %02d\n" % i)   # every fifth commit touches two files
    git("add", ".")
    subject = "fix: the needle commit" if i == 30 else "feat: change number %02d" % i
    git("commit", "-q", "-m", subject, "-m", "Body of commit %02d." % i, date="2026-03-%02dT10:%02d:00+0000" % (1 + i % 28, i % 60))
git("switch", "-q", "-c", "other", "HEAD~10")
write("unmerged.txt", "x\n"); git("add", "."); git("commit", "-q", "-m", "chore: never merged", date="2026-04-01T10:00:00+0000")
git("switch", "-q", "-c", "side", "main")
write("side.txt", "side\n"); git("add", "."); git("commit", "-q", "-m", "feat: side work", date="2026-04-02T10:00:00+0000")
git("switch", "-q", "main")
git("merge", "-q", "--no-ff", "-m", "Merge branch 'side'", "side", date="2026-04-03T10:00:00+0000")
git("tag", "v1.0", "HEAD~2")

def short(rev): return git("rev-parse", "--short", rev)
def full(rev): return git("rev-parse", rev)
# HEAD is the merge; its first parent is commit 59 of the straight part.
HEAD, SIDE, C59 = short("HEAD"), short("side"), short("HEAD~1")
NEEDLE, NEEDLE_FULL = short("HEAD~30"), full("HEAD~30")

# ---------- stubs for the clipboard and the browser ----------
STUBLOG = os.path.join(SANDBOX, "stub.log")
STUB = os.path.join(SANDBOX, "stub")
with open(STUB, "w") as f: f.write('#!/bin/sh\n{ echo "args:$*"; [ -t 0 ] || cat; echo; } >> "%s"\n' % STUBLOG)
os.chmod(STUB, 0o755)
def stublog():
    try: return open(STUBLOG).read()
    except OSError: return ""

# ---------- pty plumbing ----------
class Screen(pyte.Screen):
    # pyte chokes on the "private" flag of a couple of CSI sequences.
    def report_device_status(self, *a, **k): pass
    def set_margins(self, *a, **k):
        k.pop("private", None); return super().set_margins(*a, **k)

    # pyte has no SU/SD (CSI n S / CSI n T), which bubbletea's renderer uses to
    # scroll a region instead of repainting it. Built on index/reverse_index,
    # which already honor the margins.
    def _scroll(self, count, line, step):
        x, y = self.cursor.x, self.cursor.y
        for _ in range(count or 1):
            self.cursor.y = line
            step()
        self.cursor.x, self.cursor.y = x, y
    def scroll_up(self, count=1, **k):
        bottom = self.margins.bottom if self.margins else self.lines - 1
        self._scroll(count, bottom, self.index)
    def scroll_down(self, count=1, **k):
        top = self.margins.top if self.margins else 0
        self._scroll(count, top, self.reverse_index)

pyte.Stream.csi = dict(pyte.Stream.csi, S="scroll_up", T="scroll_down")

class Term:
    def __init__(self, cwd, args=(), extra_env=None, rows=ROWS, cols=COLS):
        env = dict(os.environ, TERM="xterm-256color", COLORTERM="truecolor", XDG_STATE_HOME=STATE,
                   XDG_CACHE_HOME=CACHE, GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_SYSTEM="/dev/null",
                   ASGITLOG_CLIPBOARD=STUB, ASGITLOG_OPENER=STUB)
        for k in [k for k in env if k.startswith("HERDR_")]: env.pop(k)
        env["HERDR_PLUGIN_STATE_DIR"] = os.path.join(STATE, "asgitlog")   # where herdr would put the settings
        env.update(extra_env or {})
        self.master, slave = pty.openpty()
        self.resize(rows, cols, signal_proc=False)
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        self.proc = subprocess.Popen([BIN] + list(args), stdin=slave, stdout=slave, stderr=slave, env=env, close_fds=True, cwd=cwd)
        os.close(slave)
        self.raw = bytearray(); self.answered = 0

    def resize(self, rows, cols, signal_proc=True):
        self.rows, self.cols = rows, cols
        self.screen = Screen(cols, rows); self.stream = pyte.ByteStream(self.screen)
        if signal_proc:
            fcntl.ioctl(self.master, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
            self.proc.send_signal(signal.SIGWINCH)

    QUERIES = [(b"\x1b]11;?", b"\x1b]11;rgb:0000/0000/0000\x1b\\"), (b"\x1b]10;?", b"\x1b]10;rgb:ffff/ffff/ffff\x1b\\"),
               (b"\x1b[6n", b"\x1b[1;1R"), (b"\x1b[c", b"\x1b[?62c")]

    def pump(self, seconds):
        end = time.time() + seconds
        while True:
            left = end - time.time()
            if left <= 0: break
            r, _, _ = select.select([self.master], [], [], left)
            if not r: continue
            try: data = os.read(self.master, 65536)
            except OSError: break
            if not data: break
            self.raw.extend(data); self.stream.feed(data)
            tail = bytes(self.raw[self.answered:])
            for q, reply in self.QUERIES:
                for _ in range(tail.count(q)): os.write(self.master, reply)
            self.answered = len(self.raw)

    def frame(self):
        return [l.rstrip() for l in self.screen.display]

    def wait_for(self, text, seconds=5.0):
        end = time.time() + seconds
        while time.time() < end:
            self.pump(0.1)
            if any(text in l for l in self.frame()): return True
        return False

    def send(self, b, settle=0.4):
        os.write(self.master, b); self.pump(settle)

    def wait_exit(self, seconds=3.0):
        end = time.time() + seconds
        while time.time() < end and self.proc.poll() is None: self.pump(0.1)
        return self.proc.poll()

failures = []
# Without delta the diffs are git's own: the mode change is flashed as a notice
# and a file starts at its "diff --git" line instead of delta's header.
DELTA = shutil.which("delta") is not None
RENDERER = DELTA or shutil.which("hunk") is not None
def flash(label): return ("│ diff: %s " % label) if RENDERER else "│ no renderer found: plain git colors"
def filehead(name): return name if DELTA else "diff --git a/" + name

def check(cond, msg):
    print(("  ok   " if cond else "  FAIL ") + msg)
    if not cond: failures.append(msg)

def dump(title, f):
    print("--- %s ---" % title)
    for i, l in enumerate(f): print("%2d|%s" % (i, l))

def has(f, text): return any(text in l for l in f)
def selected(f): return [l for l in f if l.startswith("│▌ ")]
# text is "matches/total", optionally followed by the scope: the count sits at
# the right end of the edge under the list, the scope on the top border. The pty check
# runs an unstamped build, which says "(dev)" at the end of that border.
def counter(f, text):
    count, _, scope = text.partition(" ")
    under = any(re.match("├─+ " + re.escape(count) + " ─[┴┤]", l) for l in f)
    return under and f[COUNTER].endswith((" " + scope if scope else "─") + " (dev) ─╮")
def pref(name):
    try: return open(os.path.join(STATE, "asgitlog", name)).read().strip()
    except OSError: return None

PROMPT = "asgitlog ❯"
UP, DOWN, RIGHT, ESC, ENTER, TAB, BACKSPACE = b"\x1b[A", b"\x1b[B", b"\x1b[C", b"\x1b", b"\r", b"\t", b"\x7f"
CTRL_A, CTRL_C, CTRL_G, CTRL_O, CTRL_T, CTRL_Y, PANEL, SHIFT_RIGHT = b"\x01", b"\x03", b"\x07", b"\x0f", b"\x14", b"\x19", b"\x1bOP", b"\x1b[1;2C"

# Rows layout at 40 lines, one frame of three sections sharing their edges:
# input 1 (scope and state on the top border, 0), main 2-37 (list 3-12,
# top-down with the newest at 3, divider 13, details 14-36), foot 38 (the
# repo summary and the panel key).
COUNTER, INPUT, LIST_TOP, NEWEST, DIVIDER, BOTTOM, HELP = 0, 1, 3, 3, 13, 37, 38

# ---------- 1. the rows layout ----------
print("== asgitlog pty driver (%dx%d) ==" % (COLS, ROWS))
t = Term(REPO)
check(t.wait_for("side.txt"), "first preview rendered by delta")
t.pump(0.5)
f0 = t.frame(); dump("initial frame (rows layout)", f0)
check(f0[0].startswith("╭─") and f0[INPUT].startswith("│ ") and "/repo  main" in f0[HELP] and f0[HELP].rstrip("│ ").endswith("f1 options"), "repo summary at the foot, the panel key at its right end: %r" % f0[HELP][-60:])
check(all(len(l) == COLS for l in f0), "every line spans the full width")
check(f0[NEWEST].startswith("│▌ " + HEAD + " Ada Lovelace Merge branch 'side'"), "newest commit first, wide row: hash, author, subject (no email)")
check(re.search(r"  HEAD -> main \d\d/\d\d/\d{4}│$", f0[NEWEST]) is not None, "refs take what they need, right before the date: %r" % f0[NEWEST][-32:])
check(f0[NEWEST + 1].startswith("│  " + SIDE + " Ada Lovelace feat: side work") and "side" in f0[NEWEST + 1][-21:], "older commits go down")
check("tag: v1.0" in f0[NEWEST + 3], "tag decoration on its commit")
check(counter(f0, "%d/%d" % (ON_MAIN, ON_MAIN)) and f0[INPUT].startswith("│ " + PROMPT), "input above the list, counter on its edge")
check(f0[LIST_TOP - 1].startswith("├─") and re.fullmatch(r"├─+ %d/%d ─┤" % (ON_MAIN, ON_MAIN), f0[DIVIDER]) and len(f0[DIVIDER]) == COLS,
      "list and details share a section; the divider carries the counter at its right end: %r" % f0[DIVIDER][-24:])
check(all(l.startswith("│ ") and l.endswith(" │") for l in f0[DIVIDER + 1:BOTTOM]) and f0[BOTTOM].startswith("├─"), "details framed with padding")
check(has(f0, "Merge:  ") and has(f0, "diff against the first parent") and has(f0, "── 1 file changed  +1 -0 ─") and has(f0, "── diff ─"), "a clean merge shows what it brought in")
check(f0[HELP].startswith("│ ") and "type filter" not in f0[HELP] and f0[HELP + 1].startswith("╰─"), "the foot shows the context, not the actions, at the bottom of the frame: %r" % f0[HELP][:60])
check([i for i, l in enumerate(f0) if l[0] in "╭╰"] == [0, ROWS - 1], "one frame: no section spends lines on borders of its own")
check(b"\x1b[?1049h" in t.raw, "alt screen entered")

t.send(DOWN); t.send(DOWN)
check(t.wait_for("Body of commit 59."), "down moves to older commits and the preview follows")
t.pump(0.6)
f = t.frame()
check(f[NEWEST + 2].startswith("│▌ " + C59), "marker two lines below the newest")
check(has(f, "── 1 file changed  +16 -16 ─") and sum(l.count("│") for l in f) > 40, "titled file list and side-by-side panels")
check(re.search(r" \d+/\d+ ─┤$", f[BOTTOM]) is not None, "scroll position on the main section's bottom edge: %r" % f[BOTTOM][-16:])

# wheel scrolls the preview, not the list, and never leaks into the filter
before = t.frame()
t.send(b"\x1b[<65;70;30M" * 30, settle=0.6)
after = t.frame()
check(after[INPUT] == before[INPUT] and after[LIST_TOP:DIVIDER] == before[LIST_TOP:DIVIDER], "wheel: input clean, list unchanged")
check(after[DIVIDER + 1:BOTTOM] != before[DIVIDER + 1:BOTTOM], "wheel: details scrolled")
check(after[DIVIDER] == before[DIVIDER], "scrolled: the divider stays a plain line: %r" % after[DIVIDER][:60])

t.send(b"\x1b[<0;20;%dM\x1b[<0;20;%dm" % (NEWEST + 2, NEWEST + 2))   # 1-based: the line below the newest
check(t.frame()[NEWEST + 1].startswith("│▌ " + SIDE), "left click selects the row under the pointer")

# the wheel over the list walks the history; home comes back to the newest
t.send(b"\x1b[<65;20;10M" * 3, settle=0.6)   # wheel down over the list = older
check("feat: change number 57" in (selected(t.frame()) or [""])[0], "wheel over the list moves the selection: %r" % (selected(t.frame()) or [""])[0][:50])
t.send(b"\x1b[<65;20;10M" * 40, settle=0.8)
check("feat: change number 17" in (selected(t.frame()) or [""])[0], "a long wheel burst scrolls far down the history")
t.send(b"\x1b[H", settle=0.6)
check(t.frame()[NEWEST].startswith("│▌ " + HEAD), "home jumps back to the newest commit")
t.send(b"\x1b[F", settle=0.6)
check("feat: change number 00" in (selected(t.frame()) or [""])[0], "end jumps to the oldest")
t.send(b"\x1b[1;3A", settle=0.6)   # alt+up, for keyboards without home/end
check(t.frame()[NEWEST].startswith("│▌ " + HEAD), "alt+up reaches the top of the list (the newest)")
t.send(b"\x1b[1;3B", settle=0.6)
check("feat: change number 00" in (selected(t.frame()) or [""])[0], "alt+down reaches the bottom of the list (the oldest)")
t.send(b"\x1b[H", settle=0.6)

# ---------- 2. filter ----------
t.send(b"needle")
f = t.frame(); dump("filtered", f[COUNTER:DIVIDER + 1])
check(f[NEWEST].startswith("│▌ " + NEEDLE) and "fix: the needle commit" in f[NEWEST], "substring filter narrows to the needle commit")
check(counter(f, "1/%d" % ON_MAIN), "counter shows matches/total: %r" % f[COUNTER][-14:])
t.send(BACKSPACE * 6 + b"chng")
check(counter(t.frame(), "0/%d" % ON_MAIN), "letters in order but apart do not match a plain term")
t.send(BACKSPACE * 4 + b"~ndl cmmt")
check(counter(t.frame(), "0/%d" % ON_MAIN), "terms are ANDed (cmmt is not a substring)")
t.send(BACKSPACE * 4 + b"~cmmt")
f = t.frame()
check(counter(f, "1/%d" % ON_MAIN) and NEEDLE in f[NEWEST], "~terms match fuzzily")
check(t.wait_for("Body of commit 30."), "preview follows the filtered selection")
t.send(BACKSPACE * 10)
f = t.frame()
check(counter(f, "%d/%d" % (ON_MAIN, ON_MAIN)) and len(selected(f)) == 1 and NEEDLE in selected(f)[0], "clearing the query stays on the found commit")

# ---------- 3. columns layout, diff mode, list size: all persisted ----------
# The layout is chosen in the panel: f1 lays it over the frame.
rows = len(t.frame())
t.send(PANEL, settle=0.5)
f = t.frame(); dump("panel", f)
check(len(f) == rows and has(f, "╭─ options ") and has(f, "▌ Diff renderer") and has(f, "search the diffs") and f[-1].startswith("╰─"),
      "f1 opens the options and the keys over a frame that keeps its size")
t.send(b"zz"); t.send(b"\x1b[B" * 3); t.send(b" ", settle=0.8)   # down to Layout, space changes it
t.send(ESC, settle=0.8)
f = t.frame(); dump("columns layout", f[:7])
check(f[INPUT].startswith("│ " + PROMPT + " ") and "zz" not in f[INPUT] and not has(f, "╭─ options "), "esc closes the panel; it took the keys, the filter did not")
sel = selected(f)
check(len(sel) == 1 and NEEDLE in sel[0], "cursor stays on the same commit across the layout change")
check(f[INPUT].startswith("│ " + PROMPT) and counter(f, "%d/%d" % (ON_MAIN, ON_MAIN)), "columns: same input, list top-down")
check(re.match(r"^│  [0-9a-f]+ +\d+(mo|[ymwdh]) (feat|fix): ", f[LIST_TOP]) is not None and "Ada" not in f[LIST_TOP].split("│")[1], "compact rows: hash, relative date, subject: %r" % f[LIST_TOP][:40])
col = f[LIST_TOP - 1].index("┬")
check(f[BOTTOM][col] == "┴" and all(l[col] == "│" for l in f[LIST_TOP:BOTTOM]) and f[LIST_TOP - 1].replace("─", "") == "├┬┤",
      "a vertical divider splits the main section, list | details, under a plain edge")
check(pref("layout") == "columns", "layout persisted: %r" % pref("layout"))
edge = col
t.send(SHIFT_RIGHT, settle=0.8)
check(t.frame()[LIST_TOP - 1].index("┬") > edge and pref("split-columns") == "70", "shift+right grows the list, persisted: %r" % pref("split-columns"))
t.send(CTRL_T, settle=0.2)
check(t.wait_for(flash("side-by-side")) and pref("diff") == "sbs", "ctrl+t: auto -> side-by-side, flashed in the help line")
t.send(CTRL_T, settle=0.2)
check(t.wait_for(flash("single column")) and pref("diff") == "single", "ctrl+t: side-by-side -> single column")
t.pump(0.8)
t.send(b"\x13", settle=0.2)   # ctrl+s
check(t.wait_for("│ whitespace: ignored ") and pref("whitespace") == "ignore" and has(t.frame(), "[-w] ─"),
      "ctrl+s ignores the whitespace: flashed, marked on the bottom edge and persisted")
t.send(b"\x13", settle=0.2)
check(t.wait_for("│ whitespace: shown ") and pref("whitespace") == "show" and not has(t.frame(), "[-w]"), "ctrl+s again shows it")
t.pump(0.8)

t.resize(30, 110); t.pump(1.0)
f = t.frame()
sel = selected(f)
check(len(sel) == 1 and NEEDLE in sel[0] and f[INPUT].startswith("│ " + PROMPT) and "f1 options" in f[-2], "resize: re-laid out, same commit selected")

# ---------- 4. full view ----------
t.send(ENTER, settle=0.2)
check(t.wait_for("q/esc back"), "enter opens the full-screen diff")
t.pump(0.8)
f = t.frame(); dump("full view", f[:8])
check(not has(f, PROMPT) and NEEDLE in f[0] and "needle" in f[0] and "single column" in f[0], "full view title: %r" % f[0])
check(has(f, "Body of commit 30.") and has(f, "    other.txt") and f[1] == "─" * 110, "header with the changed files, under a rule")
top = t.frame()[2:7]
t.send(b"j"); t.send(b"j"); t.send(b"j")
check(t.frame()[2:7] != top, "j scrolls")
t.send(b"G"); check(t.frame()[0].rstrip().endswith("100%"), "G jumps to the bottom")
t.send(b"g"); check(t.frame()[2:7] == top, "g jumps back to the top")
t.send(TAB); check(t.frame()[2].startswith(filehead("file.txt")), "tab jumps to the first file: %r" % t.frame()[2][:20])
t.send(TAB)  # the last file is near the end, so it cannot reach the top line
check(any(l.startswith(filehead("other.txt")) for l in t.frame()) and t.frame()[0].rstrip().endswith("100%"), "tab again, the next file comes into view")
t.send(b"\x1b[Z"); check(t.frame()[2].startswith(filehead("file.txt")), "shift+tab goes back")
t.send(b"g/"); t.send(b"line 70 of"); t.send(ENTER)
f = t.frame()
check(has(f[2:-1], "line 70 of revision") and "match 1/" in f[0], "search jumps to the first match: %r" % f[0][-40:])
t.send(b"n"); check("match 2/" in t.frame()[0], "n goes to the next match")
t.send(ESC); check("match" not in t.frame()[0] and not has(t.frame(), PROMPT), "esc clears the search first")
t.send(b"]", settle=0.2)
check(t.wait_for("Body of commit 29.") and "change number 29" in t.frame()[0], "] opens the older commit without leaving")
t.send(b"[", settle=0.2)
check(t.wait_for("Body of commit 30."), "[ comes back")
t.send(b"y", settle=0.8)
check(has(t.frame()[-1:], "copied " + NEEDLE_FULL) and NEEDLE_FULL + "\n" in stublog(), "y copies the full hash: %r" % t.frame()[-1])
t.send(b"o", settle=0.8)
check("args:https://github.com/acme/widgets/commit/" + NEEDLE_FULL in stublog(), "o opens the commit page of the remote")
t.pump(2.2)  # let the "opened ..." status give the last line back to the help
t.send(b"?")
f = t.frame()
check(has(f, "next / previous match") and has(f, "copy the hash") and has(f, "page, half page") and has(f, "▌ Diff renderer") and not has(f, "Layout"),
      "? opens the panel in the full view: its keys and its options")
check(has(f[-1:], "q/esc back") and NEEDLE in f[0], "the full view stays in place under it")
t.send(b"?"); check(has(t.frame()[-1:], "q/esc back") and not has(t.frame(), "page, half page"), "? again closes it")
t.send(b"q")
check(t.wait_for(PROMPT, 2.0) and NEEDLE in (selected(t.frame()) or [""])[0], "q returns to the list, selection intact")

t.send(ESC)
check(t.wait_exit() == 0 and b"\x1b[?1049l" in t.raw, "esc exits with status 0 and leaves the alt screen")

# ---------- 5. next run: settings restored; log scopes ----------
t = Term(REPO)
check(t.wait_for("┬") and pref("diff") == "single", "next run starts in columns + single column")
t.send(CTRL_A, settle=1.0)
f = t.frame()
check(counter(f, "%d/%d [all refs]" % (ALL, ALL)) and has(f, "chore: never merged"), "ctrl+a lists all refs: %r" % f[COUNTER][-30:])
t.send(CTRL_A, settle=1.0)
check(counter(t.frame(), "%d/%d" % (ON_MAIN, ON_MAIN)), "ctrl+a again, back to the current branch")
t.send(CTRL_G)
check(has(t.frame(), "search the diffs (-S) ❯"), "ctrl+g asks for the text to search the diffs for")
t.send(b"revision 47"); t.send(ENTER, settle=1.2)
f = t.frame(); dump("content search", f[:7])
check(counter(f, '2/2 [-S"revision 47"]') and "change number 48" in f[LIST_TOP] and "change number 47" in f[LIST_TOP + 1], "only the commits adding or removing the text")
t.send(CTRL_G); t.send(BACKSPACE * 11); t.send(ENTER, settle=1.0)
check(counter(t.frame(), "%d/%d" % (ON_MAIN, ON_MAIN)), "an empty text lifts the scope")
t.send(b"?", settle=0.6)
f = t.frame()
check(f[INPUT].rstrip("│ ").endswith("?") and not has(f, "╭─ options "), "? is text for the filter in the list: %r" % f[INPUT][:40])
t.send(BACKSPACE, settle=0.6)
t.send(PANEL); t.send(b"\x1b[B" * 3); t.send(b" "); t.send(ESC); t.send(CTRL_T)
t.send(CTRL_C)
check(t.wait_exit() == 0 and pref("layout") == "rows" and pref("diff") == "auto", "ctrl+c exits; settings back to rows + auto")

# ---------- 6. revision / path arguments, working tree row ----------
t = Term(REPO, args=["--", "other.txt"])
check(t.wait_for("[-- other.txt]"), "paths after -- scope the log")
t.wait_for("── 1 file changed")   # the preview is rendered after the list: a slow runner shows the gap
f = t.frame()
check(counter(f, "12/12 [-- other.txt]") and has(f, "── 1 file changed  +1 -1 ─") and not has(f, "file.txt"), "and the preview: %r" % f[COUNTER][-30:])
t.send(ESC); t.wait_exit()

write("file.txt", file_txt(59) + "one more line\n"); write("scratch.txt", "x\n")
t = Term(REPO)
check(t.wait_for("Working tree"), "uncommitted changes get a row of their own")
t.pump(0.6)
f = t.frame()
check(f[NEWEST].startswith("│▌ *") and "Uncommitted changes (2 files)" in f[NEWEST] and HEAD in f[NEWEST + 1], "on top of the newest commit: %r" % f[NEWEST][:50])
check(has(f, "── 1 file changed  +1 -0 ─") and has(f, "── 1 untracked ─") and has(f, "    scratch.txt") and has(f, "one more line"), "its preview is the diff against HEAD")
t.send(ESC); t.wait_exit()
git("checkout", "-q", "--", "file.txt"); os.remove(os.path.join(REPO, "scratch.txt"))

# ---------- 7. outside a repository ----------
t = Term(SANDBOX, rows=10, cols=80)
code = t.wait_exit()
out = bytes(t.raw).decode(errors="replace")
check(code == 1 and "not inside a git work tree" in out and "\x1b[?1049h" not in out, "plain shell outside a repo: message + exit 1, no TUI")

# ---------- 8. as a herdr plugin pane ----------
pane = {"HERDR_PLUGIN_ENTRYPOINT_ID": "log", "HERDR_PLUGIN_CONTEXT_JSON": json.dumps({"focused_pane_cwd": REPO})}
t = Term(SANDBOX, extra_env=pane)
check(t.wait_for("feat: change number 59"), "plugin pane browses the focused pane's repository, not its own cwd")
t.send(ESC); t.wait_exit()
pane["HERDR_PLUGIN_CONTEXT_JSON"] = json.dumps({"focused_pane_cwd": SANDBOX})
t = Term(SANDBOX, extra_env=pane, rows=10, cols=80)
check(t.wait_for("not inside a git work tree") and t.wait_for("press enter to close"), "plugin pane outside a repo: the error is held on screen")
t.send(b"\r")
check(t.wait_exit() == 1, "enter closes it with status 1")

# ---------- 9. hunk as the diff renderer ----------
if shutil.which("hunk"):
    t = Term(REPO)
    check(t.wait_for("side.txt"), "starts with delta's render")
    t.send(PANEL); t.send(b" ", settle=0.2); t.send(ESC, settle=0.2)   # the renderer is the panel's first option
    check(t.wait_for("│ diffs by hunk ") and pref("renderer") == "hunk", "the panel: hunk renders the diffs, flashed and persisted")
    check(t.wait_for("+1 -0", 8.0), "hunk's file header (path and counts) shows in the details")
    t.pump(0.5)
    f = t.frame(); dump("rendered by hunk", f[DIVIDER:DIVIDER + 16])
    check(all(len(l) == COLS and l.startswith("│") and l.endswith("│") for l in f[1:-1] if not l.startswith("├")), "hunk's screen stays inside the frame")
    check(has(f, "── diff ─") and any(l.startswith("│ ▌") for l in f[DIVIDER + 1:BOTTOM]), "hunk's rows under the native header")
    t.send(PANEL); t.send(b" ", settle=0.5); t.send(ESC, settle=0.2)
    check(pref("renderer") == "delta", "again: back to delta")
    t.send(ESC); t.wait_exit()
    # The renders were kept for the next run.
    stored = sum(len(fs) for _, _, fs in os.walk(os.path.join(CACHE, "asgitlog", "renders")))
    check(stored > 0, "renders are kept on disk: %d files" % stored)
    with open(os.path.join(STATE, "asgitlog", "renderer"), "w") as f: f.write("hunk")
    t = Term(REPO)
    check(t.wait_for("+1 -0  ", 3.0) and any(l.startswith("│ ▌") for l in t.frame()), "next run starts with hunk, its render at hand")
    t.send(ESC); t.wait_exit()
    with open(os.path.join(STATE, "asgitlog", "renderer"), "w") as f: f.write("delta")
    left = subprocess.run(["pgrep", "-f", "hunk patch .*asgitlog-"], capture_output=True, text=True).stdout.split()
    check(not left, "no hunk process outlives its render: %r" % left)
else:
    print("  (hunk not installed: renderer step skipped)")

shutil.rmtree(SANDBOX, ignore_errors=True)
print("\n%s (%d failed)" % ("FAILED" if failures else "ALL OK", len(failures)))
sys.exit(1 if failures else 0)
