package main

// Filtering of the commit list. Matching runs on each commit's corpus (see
// commit in git.go) and never reorders: hits stay in log order. A query is
// split on whitespace and every term must match. A plain term is a
// case-insensitive substring, which is what a log search almost always wants
// (a fuzzy subsequence over a whole row matches nearly everything: "fix pane"
// hits a fifth of herdr's history). A term starting with ~ is fuzzy; the terms
// are parsed by queryTerms in match.go, the same in every tool of the family.

import (
	"strings"
	"unicode/utf8"

	"github.com/sahilm/fuzzy"
)

// hit is one commit that passed the filter. matched holds byte offsets into
// the commit's corpus (sahilm/fuzzy reports byte positions too), for
// highlighting.
type hit struct {
	idx     int
	matched []int
}

// foldIndexAll returns the byte offsets of every occurrence of needle (already
// lowercased) in s, ignoring case. The ASCII path does not allocate; anything
// else falls back to lowercasing s, which keeps byte offsets for all but a
// handful of code points whose lowercase form has another length.
func foldIndexAll(s, needle string) []int {
	if needle == "" {
		return nil
	}
	var out []int
	ascii := true
	for i := 0; i < len(needle); i++ {
		if needle[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if !ascii {
		low := strings.ToLower(s)
		if len(low) != len(s) {
			if strings.Contains(low, needle) {
				return []int{} // a match, with no trustworthy offsets
			}
			return nil
		}
		for from := 0; ; {
			j := strings.Index(low[from:], needle)
			if j < 0 {
				return out
			}
			out = append(out, from+j)
			from += j + len(needle)
		}
	}
	n := len(needle)
	for i := 0; i+n <= len(s); i++ {
		j := 0
		for ; j < n; j++ {
			c := s[i+j]
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != needle[j] {
				break
			}
		}
		if j == n {
			out = append(out, i)
			i += n - 1
		}
	}
	return out
}

// corpusSource adapts a subset of commits to fuzzy.Source.
type corpusSource struct {
	commits []commit
	idx     []int
}

func (s corpusSource) Len() int            { return len(s.idx) }
func (s corpusSource) String(i int) string { return s.commits[s.idx[i]].corpus }

// filterCommits returns the commits matching every term, in log order.
// Candidates are commits[from:] when among is nil, else exactly the commits
// indexed by among (used to narrow a previous result).
func filterCommits(commits []commit, among []int, from int, terms []qterm) []hit {
	if len(terms) == 0 {
		return nil
	}
	var hits []hit
	if among != nil {
		hits = make([]hit, len(among))
		for i, idx := range among {
			hits[i].idx = idx
		}
	} else {
		hits = make([]hit, 0, len(commits)-from)
		for i := from; i < len(commits); i++ {
			hits = append(hits, hit{idx: i})
		}
	}
	for _, t := range terms {
		next := hits[:0:0]
		if t.fuzzy {
			idx := make([]int, len(hits))
			for i, h := range hits {
				idx[i] = h.idx
			}
			for _, mt := range fuzzy.FindFromNoSort(t.text, corpusSource{commits, idx}) {
				tighten(t.text, &mt) // log order, so only the highlight changes
				h := hits[mt.Index]
				h.matched = append(h.matched, mt.MatchedIndexes...)
				next = append(next, h)
			}
		} else {
			for _, h := range hits {
				offs := foldIndexAll(commits[h.idx].corpus, t.text)
				if offs == nil {
					continue
				}
				for _, o := range offs {
					for k := 0; k < len(t.text); k++ {
						h.matched = append(h.matched, o+k)
					}
				}
				next = append(next, h)
			}
		}
		hits = next
		if len(hits) == 0 {
			return nil
		}
	}
	return hits
}

// narrows reports whether a result for prev can be reused as the candidate
// set for next. Terms are ANDed and each one only gets more specific as text
// is appended (substring and subsequence alike), so extending the query can
// only shrink the result.
func narrows(prev, next string) bool {
	return len(queryTerms(prev, false)) > 0 && strings.HasPrefix(next, prev)
}
