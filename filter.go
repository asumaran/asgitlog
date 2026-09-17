package main

// Fuzzy filtering of the commit list. Matching runs on each commit's corpus
// (see commit in git.go) and never reorders: hits stay in log order, like fzf
// with --no-sort. A query is split on whitespace and every term must match
// (fzf's extended-search AND), so "fix preview" finds commits matching both
// words in any position.

import (
	"strings"

	"github.com/sahilm/fuzzy"
)

// hit is one commit that passed the filter. matched holds byte offsets into
// the commit's corpus (sahilm/fuzzy reports byte positions), for highlighting.
type hit struct {
	idx     int
	matched []int
}

func queryTerms(q string) []string { return strings.Fields(q) }

// corpusSource adapts a subset of commits to fuzzy.Source. idx nil means
// commits[from:].
type corpusSource struct {
	commits []commit
	idx     []int
	from    int
}

func (s corpusSource) Len() int {
	if s.idx != nil {
		return len(s.idx)
	}
	return len(s.commits) - s.from
}

func (s corpusSource) at(i int) int {
	if s.idx != nil {
		return s.idx[i]
	}
	return s.from + i
}

func (s corpusSource) String(i int) string { return s.commits[s.at(i)].corpus }

// filterCommits returns the commits matching every term, in log order.
// Candidates are commits[from:] when among is nil, else exactly the commits
// indexed by among (used to narrow a previous result).
func filterCommits(commits []commit, among []int, from int, terms []string) []hit {
	if len(terms) == 0 {
		return nil
	}
	src := corpusSource{commits: commits, idx: among, from: from}
	var hits []hit
	for ti, term := range terms {
		matches := fuzzy.FindFromNoSort(term, src)
		next := make([]hit, 0, len(matches))
		for _, mt := range matches {
			// The matcher hands over MatchedIndexes of actual matches (it only
			// recycles the slices of non-matches), so no copy is needed.
			h := hit{idx: src.at(mt.Index), matched: mt.MatchedIndexes}
			if ti > 0 {
				h.matched = append(h.matched, hits[mt.Index].matched...)
			}
			next = append(next, h)
		}
		hits = next
		if len(hits) == 0 {
			return nil
		}
		idx := make([]int, len(hits))
		for i, h := range hits {
			idx[i] = h.idx
		}
		src = corpusSource{commits: commits, idx: idx}
	}
	return hits
}

// narrows reports whether a result for prev can be reused as the candidate
// set for next. Every term is a subsequence match and terms are ANDed, so
// extending the query text can only shrink the result.
func narrows(prev, next string) bool {
	return strings.TrimSpace(prev) != "" && strings.HasPrefix(next, prev)
}
