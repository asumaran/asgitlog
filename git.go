package main

// Everything that talks to git: the work tree check, the repo summary for the
// head, the streamed `git log` behind the list, the per-commit detail (body +
// numstat) behind the preview header, and the remote's web URL. The log uses a
// machine readable format (unit separators between fields, NUL between
// records) and is parsed into structs; column layout happens at render time,
// so a resize or a layout change never re-runs git.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ---- the remote's web page ----

// loadWebURL is the https base of the web page of the remote the branch
// follows (origin without an upstream), "" when it is not recognizable.
func loadWebURL(upstream string) string {
	remote := "origin"
	if name, _, ok := strings.Cut(upstream, "/"); ok {
		remote = name
	}
	url, err := runGit(context.Background(), "remote", "get-url", remote)
	if err != nil {
		return ""
	}
	return webURL(url)
}

// webURL turns a remote URL (scp-like, ssh:// or https://) into the https
// base of the repository's web page, or "" when it is not recognizable.
func webURL(remote string) string {
	remote = strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	var host, path string
	switch {
	case strings.Contains(remote, "://"):
		_, rest, _ := strings.Cut(remote, "://")
		if _, after, ok := strings.Cut(rest, "@"); ok && !strings.Contains(rest[:strings.Index(rest, "@")], "/") {
			rest = after
		}
		host, path, _ = strings.Cut(rest, "/")
		host, _, _ = strings.Cut(host, ":") // drop a port
	case strings.Contains(remote, ":"):
		host, path, _ = strings.Cut(remote, ":")
		if _, after, ok := strings.Cut(host, "@"); ok {
			host = after
		}
	}
	if host == "" || path == "" || strings.HasPrefix(path, "/") {
		return ""
	}
	return "https://" + host + "/" + path
}

// commitURL is the web page of one commit on the usual forges.
func commitURL(base, hash string) string {
	switch {
	case strings.Contains(base, "gitlab"):
		return base + "/-/commit/" + hash
	case strings.Contains(base, "bitbucket"):
		return base + "/commits/" + hash
	}
	return base + "/commit/" + hash
}

// ---- commits ----

type refKind int

const (
	refHead   refKind = iota // a detached "HEAD"
	refBranch                // refs/heads/*
	refRemote                // refs/remotes/*
	refTag                   // refs/tags/*
	refOther                 // refs/stash, grafted, replaced, ...
)

// ref is one decoration. text is what is displayed ("main", "origin/main",
// "tag: v1.0"); off is its byte offset in the commit's corpus. head marks the
// branch HEAD points at, displayed with a "HEAD -> " prefix that sits right
// before off in the corpus.
type ref struct {
	text string
	kind refKind
	off  int
	head bool
}

const headArrow = "HEAD -> "

// commit is one parsed log record. All displayed fields are substrings of
// corpus, the single string the filter runs on:
//
//	<short> <author> <<email>> <subject> <refs> <dd/mm/yyyy HH:MM>
//
// Slicing instead of copying keeps a large history at one allocation per
// commit, and makes every field's byte offset in the corpus (what the matcher
// reports) known, so matched characters can be highlighted in place.
type commit struct {
	hash    string
	corpus  string
	refs    []ref
	when    int64  // author time, unix seconds
	parents string // full parent hashes, kept only for merges
	wt      bool   // the "uncommitted changes" pseudo-commit

	oAuthor, oEmail, oSubject, oRefs, oDate int
}

func (c *commit) short() string    { return c.corpus[:c.oAuthor-1] }
func (c *commit) author() string   { return c.corpus[c.oAuthor : c.oEmail-2] }
func (c *commit) email() string    { return c.corpus[c.oEmail : c.oSubject-2] }
func (c *commit) subject() string  { return c.corpus[c.oSubject : c.oRefs-1] }
func (c *commit) date() string     { return c.corpus[c.oDate : c.oDate+10] }
func (c *commit) dateTime() string { return c.corpus[c.oDate:] }
func (c *commit) merge() bool      { return c.parents != "" }

const (
	fieldSep  = "\x1f"
	logFormat = "%H%x1f%h%x1f%an%x1f%ae%x1f%ad%x1f%D%x1f%P%x1f%at%x1f%s"
	// logDateFormat must keep the dd/mm/yyyy part 10 bytes wide: commit.date
	// slices it out of the full timestamp.
	logDateFormat = "format:%d/%m/%Y %H:%M"
	goDateFormat  = "02/01/2006 15:04"
)

// logOpts is the scope of the log: what the command line asked for plus the
// toggles of the TUI.
type logOpts struct {
	revs    []string // revisions / ranges from the command line
	paths   []string // pathspecs from the command line (after --)
	all     bool     // --all
	pickaxe string   // -S<text>: only commits that change the number of occurrences
}

func (o logOpts) args() []string {
	args := []string{
		"log", "-z", "--no-color", "--decorate=full",
		// <remote>/HEAD only duplicates the remote's default branch.
		"--decorate-refs-exclude=refs/remotes/*/HEAD",
		"--date=" + logDateFormat,
		"--format=" + logFormat,
	}
	if o.all {
		args = append(args, "--all")
	}
	if o.pickaxe != "" {
		args = append(args, "-S"+o.pickaxe)
	}
	args = append(args, o.revs...)
	return append(append(args, "--"), o.paths...)
}

// pathArgs is the "-- <paths>" tail for the per-commit commands.
func pathArgs(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	return append([]string{"--"}, paths...)
}

// parseCommit parses one NUL-delimited log record.
func parseCommit(rec string) (commit, bool) {
	f := strings.Split(rec, fieldSep)
	if len(f) != 9 || len(f[4]) < 10 {
		return commit{}, false
	}
	hash, short, author, email, date, decor, parents, at, subject := f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7], f[8]

	var b strings.Builder
	b.Grow(len(short) + len(author) + len(email) + len(subject) + len(decor) + len(date) + 8)
	c := commit{hash: hash}
	c.when, _ = strconv.ParseInt(at, 10, 64)
	if strings.Contains(parents, " ") {
		c.parents = parents
	}
	b.WriteString(short)
	b.WriteByte(' ')
	c.oAuthor = b.Len()
	b.WriteString(author)
	b.WriteString(" <")
	c.oEmail = b.Len()
	b.WriteString(email)
	b.WriteString("> ")
	c.oSubject = b.Len()
	b.WriteString(subject)
	b.WriteByte(' ')
	c.oRefs = b.Len()
	c.refs = writeRefs(&b, decor)
	b.WriteByte(' ')
	c.oDate = b.Len()
	b.WriteString(date)
	c.corpus = b.String()
	return c, true
}

// writeRefs parses a --decorate=full %D string ("HEAD -> refs/heads/main,
// tag: refs/tags/v1, refs/remotes/origin/main"), appends the short display
// form to b and returns the refs with their offsets in b.
func writeRefs(b *strings.Builder, decor string) []ref {
	if decor == "" {
		return nil
	}
	items := strings.Split(decor, ", ")
	refs := make([]ref, 0, len(items))
	for i, item := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		var r ref
		if rest, ok := strings.CutPrefix(item, headArrow); ok {
			r.head = true
			item = rest
			b.WriteString(headArrow)
		}
		tag := false
		if rest, ok := strings.CutPrefix(item, "tag: "); ok {
			tag = true
			item = rest
		}
		switch {
		case item == "HEAD":
			r.kind, r.text = refHead, item
		case strings.HasPrefix(item, "refs/heads/"):
			r.kind, r.text = refBranch, strings.TrimPrefix(item, "refs/heads/")
		case strings.HasPrefix(item, "refs/remotes/"):
			r.kind, r.text = refRemote, strings.TrimPrefix(item, "refs/remotes/")
		case strings.HasPrefix(item, "refs/tags/"):
			r.kind, r.text = refTag, strings.TrimPrefix(item, "refs/tags/")
		default:
			r.kind, r.text = refOther, item
		}
		if tag {
			r.kind = refTag
			r.text = "tag: " + r.text
		}
		r.off = b.Len()
		b.WriteString(r.text)
		refs = append(refs, r)
	}
	return refs
}

// wtShort stands in for the hash of the working tree pseudo-commit.
const wtShort = "*"

// workTreeCommit builds the "uncommitted changes" row shown above the newest
// commit, or reports false when the tree is clean (or HEAD does not exist yet,
// in which case there is nothing to diff against).
func workTreeCommit(ctx context.Context, paths []string) (commit, bool) {
	if _, err := runGit(ctx, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		return commit{}, false
	}
	out, err := runGit(ctx, append([]string{"status", "--porcelain"}, pathArgs(paths)...)...)
	if err != nil || out == "" {
		return commit{}, false
	}
	n := strings.Count(out, "\n") + 1
	now := time.Now()
	c, ok := parseCommit(strings.Join([]string{
		"", wtShort, "", "", now.Format(goDateFormat), "", "", strconv.FormatInt(now.Unix(), 10),
		"Uncommitted changes (" + plural(n, "file") + ")",
	}, fieldSep))
	c.wt = true
	return c, ok
}

// logBatch is one chunk of the streamed log. gen identifies the stream it
// belongs to (the log restarts when its scope changes), done marks the last
// one and err is git's failure (an empty repository, for instance).
type logBatch struct {
	gen     int
	commits []commit
	done    bool
	err     error
}

const (
	firstBatchSize = 200 // small, so the first screen shows up immediately
	batchSize      = 4000
)

func scanNUL(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// streamLog runs git log and sends the parsed commits in batches; the channel
// is closed after the batch flagged done. The working tree row, when there is
// one, leads the first batch. Cancel ctx to stop early.
func streamLog(ctx context.Context, opts logOpts, gen int) <-chan logBatch {
	ch := make(chan logBatch, 4)
	go func() {
		defer close(ch)
		send := func(b logBatch) bool {
			b.gen = gen
			select {
			case ch <- b:
				return true
			case <-ctx.Done():
				return false
			}
		}
		// The status check runs while git log starts up.
		wtCh := make(chan *commit, 1)
		go func() {
			// A content search lists commits that touched a string; the
			// working tree is not one of them.
			if c, ok := workTreeCommit(ctx, opts.paths); ok && opts.pickaxe == "" {
				wtCh <- &c
			} else {
				wtCh <- nil
			}
		}()
		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, "git", opts.args()...)
		cmd.Stderr = &stderr
		out, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			send(logBatch{done: true, err: err})
			return
		}
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
		sc.Split(scanNUL)
		limit := firstBatchSize
		batch := make([]commit, 0, limit)
		if wt := <-wtCh; wt != nil {
			batch = append(batch, *wt)
		}
		for sc.Scan() {
			// git separates -z records with NUL but still ends the output
			// with a newline in some versions; tolerate both.
			if c, ok := parseCommit(strings.TrimPrefix(sc.Text(), "\n")); ok {
				batch = append(batch, c)
			}
			if len(batch) >= limit {
				if !send(logBatch{commits: batch}) {
					_ = cmd.Wait()
					return
				}
				limit = batchSize
				batch = make([]commit, 0, limit)
			}
		}
		err = cmd.Wait()
		if err != nil {
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				err = errors.New(firstLine(msg))
			}
		}
		if ctx.Err() != nil {
			return
		}
		send(logBatch{commits: batch, done: true, err: err})
	}()
	return ch
}

// ---- commit detail ----

type fileStat struct {
	path    string
	added   int
	deleted int
	binary  bool
}

// detail is what the preview header needs beyond the list fields: the message
// body and the per-file line counts (git has no format placeholder for them,
// hence the separate --numstat run). untracked only applies to the working
// tree row.
type detail struct {
	body      string
	files     []fileStat
	added     int
	deleted   int
	untracked []string
}

// showArgs are the flags shared by the numstat and the patch of a commit: a
// merge is compared against its first parent (what the merge brought in),
// instead of git show's combined diff, which is empty for a clean merge.
var showArgs = []string{"-c", "core.quotepath=false", "show", "--no-color", "-m", "--first-parent"}

func loadDetail(ctx context.Context, c *commit, paths []string) (detail, error) {
	if c.wt {
		out, err := runGit(ctx, append([]string{"-c", "core.quotepath=false", "diff", "HEAD", "--no-color", "--numstat"}, pathArgs(paths)...)...)
		if err != nil {
			return detail{}, err
		}
		d := parseNumstat(out)
		if others, err := runGit(ctx, append([]string{"-c", "core.quotepath=false", "ls-files", "--others", "--exclude-standard"}, pathArgs(paths)...)...); err == nil && others != "" {
			d.untracked = strings.Split(others, "\n")
		}
		return d, nil
	}
	args := append(append([]string{}, showArgs...), "--numstat", "--format=%b%x00", c.hash)
	out, err := runGit(ctx, append(args, pathArgs(paths)...)...)
	if err != nil {
		return detail{}, err
	}
	body, stat, _ := strings.Cut(out, "\x00")
	d := parseNumstat(stat)
	d.body = strings.TrimRight(body, "\n")
	return d, nil
}

// parseNumstat reads "added<TAB>deleted<TAB>path" lines; binary files report
// "-" for both counts.
func parseNumstat(s string) detail {
	var d detail
	for _, line := range strings.Split(s, "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) != 3 {
			continue
		}
		fs := fileStat{path: f[2]}
		if f[0] == "-" {
			fs.binary = true
		} else {
			fs.added, _ = strconv.Atoi(f[0])
			fs.deleted, _ = strconv.Atoi(f[1])
		}
		d.files = append(d.files, fs)
		d.added += fs.added
		d.deleted += fs.deleted
	}
	return d
}
