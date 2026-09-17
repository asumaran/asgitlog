package main

// Everything that talks to git: the work tree check, the repo summary for the
// footer, the streamed `git log` behind the list and the per-commit detail
// (body + shortstat) behind the preview header. The log uses a machine
// readable format (unit separators between fields, NUL between records) and
// is parsed into structs; column layout happens at render time, so a resize
// or a layout change never re-runs git.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

func runGit(ctx context.Context, args ...string) (string, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func insideWorkTree() bool {
	out, err := runGit(context.Background(), "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// ---- repo summary ----

// repoInfo is the one-line repo summary of the footer: repo path, branch (or
// short hash when detached), upstream and ahead/behind counts.
type repoInfo struct {
	Top      string
	Branch   string
	Upstream string
	Ahead    int
	Behind   int
}

func loadRepoInfo() repoInfo {
	ctx := context.Background()
	var ri repoInfo
	ri.Top, _ = runGit(ctx, "rev-parse", "--show-toplevel")
	ri.Branch, _ = runGit(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if ri.Branch == "" {
		ri.Branch, _ = runGit(ctx, "rev-parse", "--short", "HEAD")
	}
	ri.Upstream, _ = runGit(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if ri.Upstream != "" {
		// --left-right on upstream...HEAD: left = behind, right = ahead.
		if out, err := runGit(ctx, "rev-list", "--left-right", "--count", ri.Upstream+"...HEAD"); err == nil {
			if f := strings.Fields(out); len(f) == 2 {
				ri.Behind, _ = strconv.Atoi(f[0])
				ri.Ahead, _ = strconv.Atoi(f[1])
			}
		}
	}
	return ri
}

func (ri repoInfo) String() string {
	s := homeRel(ri.Top) + "  " + ri.Branch
	if ri.Upstream != "" {
		s += " -> " + ri.Upstream + " (ahead " + strconv.Itoa(ri.Ahead) + ", behind " + strconv.Itoa(ri.Behind) + ")"
	}
	return s
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
// corpus, the single string the fuzzy filter runs on:
//
//	<short> <author> <<email>> <subject> <refs> <dd/mm/yyyy HH:MM>
//
// Slicing instead of copying keeps a large history at one allocation per
// commit, and makes every field's byte offset in the corpus (what the fuzzy
// matcher reports) known, so matched characters can be highlighted in place.
type commit struct {
	hash   string
	corpus string
	refs   []ref

	oAuthor, oEmail, oSubject, oRefs, oDate int
}

func (c *commit) short() string    { return c.corpus[:c.oAuthor-1] }
func (c *commit) author() string   { return c.corpus[c.oAuthor : c.oEmail-2] }
func (c *commit) email() string    { return c.corpus[c.oEmail : c.oSubject-2] }
func (c *commit) subject() string  { return c.corpus[c.oSubject : c.oRefs-1] }
func (c *commit) date() string     { return c.corpus[c.oDate : c.oDate+10] }
func (c *commit) dateTime() string { return c.corpus[c.oDate:] }

const (
	fieldSep  = "\x1f"
	logFormat = "%H%x1f%h%x1f%an%x1f%ae%x1f%ad%x1f%D%x1f%s"
	// logDateFormat must keep the dd/mm/yyyy part 10 bytes wide: commit.date
	// slices it out of the full timestamp.
	logDateFormat = "format:%d/%m/%Y %H:%M"
)

func logArgs() []string {
	return []string{
		"log", "-z", "--no-color", "--decorate=full",
		// origin/HEAD only duplicates the default branch.
		"--decorate-refs-exclude=refs/remotes/origin/HEAD",
		"--date=" + logDateFormat,
		"--format=" + logFormat,
	}
}

// parseCommit parses one NUL-delimited log record.
func parseCommit(rec string) (commit, bool) {
	f := strings.Split(rec, fieldSep)
	if len(f) != 7 || len(f[4]) < 10 {
		return commit{}, false
	}
	hash, short, author, email, date, decor, subject := f[0], f[1], f[2], f[3], f[4], f[5], f[6]

	var b strings.Builder
	b.Grow(len(short) + len(author) + len(email) + len(subject) + len(decor) + len(date) + 8)
	c := commit{hash: hash}
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

// logBatch is one chunk of the streamed log. done marks the last one; err is
// git's failure (an empty repository, for instance).
type logBatch struct {
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
// is closed after the batch flagged done. Cancel ctx to stop early.
func streamLog(ctx context.Context) <-chan logBatch {
	ch := make(chan logBatch, 4)
	go func() {
		defer close(ch)
		send := func(b logBatch) bool {
			select {
			case ch <- b:
				return true
			case <-ctx.Done():
				return false
			}
		}
		var stderr bytes.Buffer
		cmd := exec.CommandContext(ctx, "git", logArgs()...)
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

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ---- commit detail ----

// detail is what the preview header needs beyond the list fields: the message
// body and the shortstat numbers. git has no format placeholder for line
// counts, hence the separate --shortstat run. Merges report git show's
// combined numbers.
type detail struct {
	body    string
	files   int
	added   int
	deleted int
}

func loadDetail(ctx context.Context, hash string) (detail, error) {
	out, err := runGit(ctx, "show", "--no-color", "--shortstat", "--no-renames", "--format=%b%x00", hash)
	if err != nil {
		return detail{}, err
	}
	body, stat, _ := strings.Cut(out, "\x00")
	d := parseShortstat(stat)
	d.body = strings.TrimRight(body, "\n")
	return d, nil
}

// parseShortstat reads " 3 files changed, 10 insertions(+), 2 deletions(-)".
// Any of the two counts can be missing; an empty string is "no changes".
func parseShortstat(s string) detail {
	var d detail
	f := strings.Fields(s)
	for i := 1; i < len(f); i++ {
		n, err := strconv.Atoi(f[i-1])
		if err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(f[i], "file"):
			d.files = n
		case strings.HasPrefix(f[i], "insertion"):
			d.added = n
		case strings.HasPrefix(f[i], "deletion"):
			d.deleted = n
		}
	}
	return d
}
