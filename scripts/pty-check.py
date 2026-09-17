#!/usr/bin/env python3
"""End-to-end TUI check for asgitlog without a real terminal.

Spawns the binary on a pty inside a throwaway git repository, answers the
terminal queries bubbletea sends (OSC 10/11, CSI 6n, DA1), replays keystrokes,
SGR mouse reports and resizes, and asserts on frames rendered with pyte. The
settings go to a sandboxed XDG_STATE_HOME, so the real ones are never touched.

Usage: scripts/pty-check.py ./asgitlog   (needs python3 + pyte, git, delta)
"""
import fcntl, json, os, pty, select, shutil, signal, struct, subprocess, sys, tempfile, termios, time
import pyte

BIN = os.path.abspath(sys.argv[1])
ROWS, COLS = 40, 160
SANDBOX = os.path.realpath(tempfile.mkdtemp(prefix="asgitlog-pty-"))
REPO = os.path.join(SANDBOX, "repo")
STATE = os.path.join(SANDBOX, "state")
NCOMMITS = 60

# ---------- sandbox: a repository with a predictable history ----------
GIT_ENV = dict(os.environ, GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_SYSTEM="/dev/null",
               GIT_AUTHOR_NAME="Ada Lovelace", GIT_AUTHOR_EMAIL="ada@example.com",
               GIT_COMMITTER_NAME="Ada Lovelace", GIT_COMMITTER_EMAIL="ada@example.com")

def git(*args):
    subprocess.run(["git", "-C", REPO] + list(args), check=True, env=GIT_ENV,
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

os.makedirs(REPO); os.makedirs(STATE)
git("init", "-q", "-b", "main")
for i in range(NCOMMITS):
    with open(os.path.join(REPO, "file.txt"), "w") as f:
        f.write("".join("line %02d of revision %02d\n" % (n, i if n % 10 == i % 10 else 0) for n in range(80)))
    git("add", ".")
    subject = "feat: change number %02d" % i
    if i == 30: subject = "fix: the needle commit"
    env_date = "2026-03-%02dT10:%02d:00+0000" % (1 + i % 28, i % 60)
    GIT_ENV["GIT_AUTHOR_DATE"] = GIT_ENV["GIT_COMMITTER_DATE"] = env_date
    git("commit", "-q", "-m", subject, "-m", "Body of commit %02d." % i)
git("tag", "v1.0", "HEAD~1")

def short(rev):
    return subprocess.run(["git", "-C", REPO, "rev-parse", "--short", rev], check=True, env=GIT_ENV,
                          capture_output=True, text=True).stdout.strip()
HEAD, HEAD1, HEAD2, NEEDLE = short("HEAD"), short("HEAD~1"), short("HEAD~2"), short("HEAD~29")

# ---------- pty plumbing ----------
class Term:
    def __init__(self, cwd, extra_env=None, rows=ROWS, cols=COLS):
        env = dict(os.environ, TERM="xterm-256color", COLORTERM="truecolor", XDG_STATE_HOME=STATE,
                   GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_SYSTEM="/dev/null")
        for k in [k for k in env if k.startswith("HERDR_")]: env.pop(k)
        env.update(extra_env or {})
        self.master, slave = pty.openpty()
        self.resize(rows, cols, signal_proc=False)
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        self.proc = subprocess.Popen([BIN], stdin=slave, stdout=slave, stderr=slave, env=env, close_fds=True, cwd=cwd)
        os.close(slave)
        self.raw = bytearray(); self.answered = 0

    def resize(self, rows, cols, signal_proc=True):
        self.rows, self.cols = rows, cols
        self.screen = pyte.Screen(cols, rows); self.stream = pyte.ByteStream(self.screen)
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
def check(cond, msg):
    print(("  ok   " if cond else "  FAIL ") + msg)
    if not cond: failures.append(msg)

def dump(title, f):
    print("--- %s ---" % title)
    for i, l in enumerate(f): print("%2d|%s" % (i, l))

def has(f, text): return any(text in l for l in f)
def pref(name):
    try: return open(os.path.join(STATE, "asgitlog", name)).read().strip()
    except OSError: return None

PROMPT = "asgitlog (dev) ❯"
UP, DOWN, ESC, ENTER, CTRL_L, CTRL_T, BACKSPACE = b"\x1b[A", b"\x1b[B", b"\x1b", b"\r", b"\x0c", b"\x14", b"\x7f"

# ---------- 1. browsing in the rows layout ----------
print("== asgitlog pty driver (%dx%d) ==" % (COLS, ROWS))
t = Term(REPO)
check(t.wait_for("of revision 59"), "first preview rendered by delta")
f0 = t.frame(); dump("initial frame (rows layout)", f0)
check(f0[0].startswith(PROMPT), "prompt on the first line: %r" % f0[0][:30])
check(f0[0].endswith(str(NCOMMITS)), "commit counter on the input line: %r" % f0[0][-12:])
check(f0[1].startswith("▌ " + HEAD + " Ada Lovelace    <ada@example.c…> feat: change number 59"), "wide row: hash, author <email>, subject")
check(f0[1].endswith("HEAD -> main " + f0[1][-10:]) and f0[1][-10:].count("/") == 2, "wide row: refs right before the dd/mm/yyyy date: %r" % f0[1][-30:])
check("tag: v1.0" in f0[2], "tag decoration on its commit")
check(has(f0, "── side-by-side ─"), "preview label shows the diff mode")
check(has(f0, "commit ") and has(f0, "Author: Ada Lovelace <ada@example.com>"), "native header: commit + author")
check(has(f0, "Stat:   1 file, +16 -16"), "native header: stat line")
check(has(f0, "Body of commit 59."), "native header: body")
check(sum(l.count("│") for l in f0) > 20, "side-by-side diff panels drawn")
check(f0[-2].startswith(REPO.replace(os.path.expanduser("~"), "~") + "  main") or "  main" in f0[-2], "repo summary line: %r" % f0[-2])
check("full diff" in f0[-1] and "layout" in f0[-1], "help line: %r" % f0[-1])
check(b"\x1b[?1049h" in t.raw, "alt screen entered")

t.send(DOWN)
check(t.wait_for("Body of commit 58."), "down moves the selection and the preview follows")
check(t.frame()[2].startswith("▌ " + HEAD1), "marker on the second row")

# wheel scrolls the preview, not the list, and never leaks into the filter
before = t.frame()
t.send(b"\x1b[<65;70;30M" * 30, settle=0.6)
after = t.frame()
check(after[0] == before[0], "prompt clean after a wheel burst")
check(after[1:12] == before[1:12], "list unchanged by the wheel")
check(after[14:36] != before[14:36], "preview scrolled by the wheel")

# click selects a row
t.send(b"\x1b[<0;20;4M\x1b[<0;20;4m")
check(t.frame()[3].startswith("▌ " + HEAD2), "left click selects the row under the pointer")

# ---------- 2. filter ----------
t.send(b"needle")
f = t.frame(); dump("filtered", f[:4])
check("fix: the needle commit" in f[1] and f[1].startswith("▌ "), "fuzzy filter narrows to the needle commit")
check(f[0].endswith("1/%d" % NCOMMITS), "counter shows matches/total: %r" % f[0][-10:])
check(t.wait_for("Body of commit 30."), "preview follows the filtered selection")
t.send(BACKSPACE * 6)
f = t.frame()
check(f[0].rstrip().endswith(str(NCOMMITS)) and has(f[1:13], "fix: the needle commit") and
      [l for l in f[1:13] if l.startswith("▌ ")][0].find("needle") > 0, "clearing the query stays on the found commit")

# ---------- 3. layout and diff mode, persisted ----------
t.send(CTRL_L, settle=0.8)
f = t.frame(); dump("columns layout", f[:6])
sel = [l for l in f[1:-2] if l.startswith("▌ ")]
check(len(sel) == 1 and "needle" in sel[0], "cursor stays on the same commit across the layout change")
check(all(" │" in l for l in f[1:-2]), "columns layout: list | preview")
check(sel and sel[0][2:2 + len(HEAD) + 11].count("/") == 2 and "Ada" not in sel[0].split("│")[0], "compact rows: hash, date, subject")
check(pref("layout") == "columns", "layout persisted: %r" % pref("layout"))
t.send(CTRL_T, settle=0.2)
check(t.wait_for("── single column ─"), "ctrl+t switches the label to single column")
check(pref("diff") == "single", "diff mode persisted: %r" % pref("diff"))
t.pump(0.8)

# ---------- 4. resize ----------
t.resize(30, 110); t.pump(1.0)
f = t.frame(); dump("after resize to 110x30", f[:4])
sel = [l for l in f[1:-2] if l.startswith("▌ ")]
check(len(sel) == 1 and NEEDLE in sel[0], "cursor stays on the same commit across a resize")
check(f[0].startswith(PROMPT) and "layout" in f[-1], "frame re-laid out for the new size")

# ---------- 5. full view ----------
t.send(ENTER, settle=0.2)
check(t.wait_for("q/esc back"), "enter opens the full-screen diff")
t.pump(0.8)
f = t.frame(); dump("full view", f[:8])
check(not has(f, PROMPT) and "single column" in f[0] and "needle" in f[0], "full view title: %r" % f[0])
check(has(f, "Body of commit 30."), "full view shows header + diff")
top = t.frame()[1:6]
t.send(b"j"); t.send(b"j"); t.send(b"j")
check(t.frame()[1:6] != top, "j scrolls the full view")
t.send(b"G"); check(t.frame()[0].rstrip().endswith("100%"), "G jumps to the bottom")
t.send(b"g"); check(t.frame()[1:6] == top, "g jumps back to the top")
t.send(b"/"); t.send(b"line 70 of"); t.send(ENTER)
f = t.frame()
check(has(f[1:-1], "line 70 of revision") and "match 1/" in f[0], "search jumps to the first match: %r" % f[0][-40:])
t.send(b"n"); check("match 2/" in t.frame()[0], "n goes to the next match")
t.send(ESC); check("match" not in t.frame()[0] and not has(t.frame(), PROMPT), "esc clears the search first")
t.send(b"q")
check(t.wait_for(PROMPT, 2.0), "q returns to the list")
sel = [l for l in t.frame()[1:-2] if l.startswith("▌ ")]
check(len(sel) == 1 and NEEDLE in sel[0], "selection intact after the full view")

# ---------- 6. exit ----------
t.send(ESC)
check(t.wait_exit() == 0, "esc exits with status 0")
check(b"\x1b[?1049l" in t.raw, "alt screen left on exit")

# ---------- 7. settings restored on the next run ----------
t = Term(REPO)
check(t.wait_for("── single column ─") and all(" │" in l for l in t.frame()[1:-2]), "next run starts in columns + single column")
t.send(CTRL_L); t.send(CTRL_T); t.send(b"\x03")
check(t.wait_exit() == 0 and pref("layout") == "rows" and pref("diff") == "sbs", "ctrl+c exits; settings back to rows + sbs")

# ---------- 8. outside a repository ----------
t = Term(SANDBOX, rows=10, cols=80)
code = t.wait_exit()
out = bytes(t.raw).decode(errors="replace")
check(code == 1 and "not inside a git work tree" in out and "\x1b[?1049h" not in out, "plain shell outside a repo: message + exit 1, no TUI")

# ---------- 9. as a herdr plugin pane ----------
pane = {"HERDR_PLUGIN_ENTRYPOINT_ID": "log", "HERDR_PLUGIN_CONTEXT_JSON": json.dumps({"focused_pane_cwd": REPO})}
t = Term(SANDBOX, extra_env=pane)
check(t.wait_for("feat: change number 59"), "plugin pane browses the focused pane's repository, not its own cwd")
t.send(ESC); t.wait_exit()
pane["HERDR_PLUGIN_CONTEXT_JSON"] = json.dumps({"focused_pane_cwd": SANDBOX})
t = Term(SANDBOX, extra_env=pane, rows=10, cols=80)
check(t.wait_for("not inside a git work tree"), "plugin pane outside a repo: error shown inside the TUI")
t.send(b"x")
check(t.wait_exit() == 1, "any key closes the error view with status 1")

shutil.rmtree(SANDBOX, ignore_errors=True)
print("\n%s (%d failed)" % ("FAILED" if failures else "ALL OK", len(failures)))
sys.exit(1 if failures else 0)
