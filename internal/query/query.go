package query

import (
	"sort"
	"strings"

	"lexicon/internal/index"
	"lexicon/internal/posting"
	"lexicon/internal/token"
)

type Hit struct {
	DocID uint32  `json:"docId"`
	Title string  `json:"title"`
	Path  string  `json:"path"`
	Score float64 `json:"score"`
	Why   string  `json:"why"`
}

type parsed struct {
	must    []string
	should  [][]string
	not     []string
	phrases [][]string
}

// Parse: bare terms are AND, OR splits alternatives, -term excludes, quotes are a phrase.
func Parse(q string) parsed {
	var p parsed
	var cur []string
	flush := func() {
		if len(cur) > 0 {
			p.should = append(p.should, cur)
			cur = nil
		}
	}
	parts := splitQuery(q)
	for _, part := range parts {
		if part == "OR" {
			flush()
			continue
		}
		if strings.HasPrefix(part, "-") && len(part) > 1 {
			for _, t := range token.Analyze(part[1:], true) {
				p.not = append(p.not, t.Term)
			}
			continue
		}
		if strings.HasPrefix(part, `"`) && strings.HasSuffix(part, `"`) && len(part) >= 2 {
			toks := token.Analyze(part, false)
			var ph []string
			for _, t := range toks {
				if token.Stop[t.Term] {
					continue
				}
				ph = append(ph, t.Term)
			}
			if len(ph) > 0 {
				p.phrases = append(p.phrases, ph)
				cur = append(cur, ph...)
			}
			continue
		}
		for _, t := range token.Analyze(part, true) {
			cur = append(cur, t.Term)
			p.must = append(p.must, t.Term)
		}
	}
	flush()
	return p
}

func splitQuery(q string) []string {
	var out []string
	var b strings.Builder
	inQ := false
	for _, r := range q {
		switch {
		case r == '"':
			inQ = !inQ
			b.WriteRune(r)
		case r == ' ' && !inQ:
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

func Search(idx *index.Idx, q string, limit int) []Hit {
	p := Parse(q)
	if len(p.should) == 0 && len(p.phrases) == 0 {
		return nil
	}
	cands := map[uint32]float64{}
	why := map[uint32][]string{}
	groups := p.should
	if len(groups) == 0 {
		groups = [][]string{p.must}
	}
	for _, group := range groups {
		ids := candidates(idx, group)
		for _, id := range ids {
			if excluded(idx, id, p.not) {
				continue
			}
			if !phrasesMatch(idx, id, p.phrases) {
				continue
			}
			var score float64
			for _, term := range group {
				tf := tfOf(idx, term, id)
				if tf == 0 {
					continue
				}
				score += idx.BM25(term, tf, idx.Docs[id].Len)
				why[id] = append(why[id], term)
			}
			if len(p.phrases) > 0 {
				score *= 1.4
			}
			if score > cands[id] {
				cands[id] = score
			}
		}
	}
	hits := make([]Hit, 0, len(cands))
	for id, score := range cands {
		hits = append(hits, Hit{
			DocID: id,
			Title: idx.Docs[id].Title,
			Path:  idx.Docs[id].Path,
			Score: score,
			Why:   strings.Join(unique(why[id]), ","),
		})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].DocID < hits[j].DocID
		}
		return hits[i].Score > hits[j].Score
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func candidates(idx *index.Idx, terms []string) []uint32 {
	if len(terms) == 0 {
		return nil
	}
	blobs := [][]byte{}
	for _, term := range terms {
		b := idx.Blob(term)
		if len(b) == 0 {
			return nil
		}
		blobs = append(blobs, b)
	}
	return posting.Intersect(blobs...)
}

func excluded(idx *index.Idx, id uint32, terms []string) bool {
	for _, term := range terms {
		if tfOf(idx, term, id) > 0 {
			return true
		}
	}
	return false
}

func tfOf(idx *index.Idx, term string, id uint32) uint32 {
	for _, p := range idx.List(term) {
		if p.DocID == id {
			return p.TF
		}
	}
	return 0
}

func phrasesMatch(idx *index.Idx, id uint32, phrases [][]string) bool {
	for _, ph := range phrases {
		if !phraseIn(idx, id, ph) {
			return false
		}
	}
	return true
}

func phraseIn(idx *index.Idx, id uint32, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	positions := make([][]uint32, len(terms))
	for i, term := range terms {
		var pos []uint32
		for _, p := range idx.List(term) {
			if p.DocID == id {
				pos = p.Positions
				break
			}
		}
		if len(pos) == 0 {
			return false
		}
		positions[i] = pos
	}
	for _, start := range positions[0] {
		ok := true
		for i := 1; i < len(terms); i++ {
			want := start + uint32(i)
			if !hasPos(positions[i], want) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func hasPos(pos []uint32, want uint32) bool {
	i := sort.Search(len(pos), func(i int) bool { return pos[i] >= want })
	return i < len(pos) && pos[i] == want
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Scan is the baseline: walk every document and score with the same BM25 function.
func Scan(idx *index.Idx, q string) []Hit {
	p := Parse(q)
	var hits []Hit
	for _, doc := range idx.Docs {
		toks := token.Analyze(doc.Title+" "+doc.Text, true)
		tf := map[string]uint32{}
		for _, t := range toks {
			tf[t.Term]++
		}
		if excludedMap(tf, p.not) {
			continue
		}
		best := 0.0
		for _, group := range p.should {
			ok := true
			score := 0.0
			for _, term := range group {
				if tf[term] == 0 {
					ok = false
					break
				}
				score += idx.BM25(term, tf[term], doc.Len)
			}
			if !ok {
				continue
			}
			if len(p.phrases) > 0 && !phraseInTokens(toks, p.phrases) {
				continue
			}
			if len(p.phrases) > 0 {
				score *= 1.4
			}
			if score > best {
				best = score
			}
		}
		if best > 0 {
			hits = append(hits, Hit{DocID: doc.ID, Title: doc.Title, Path: doc.Path, Score: best})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].DocID < hits[j].DocID
		}
		return hits[i].Score > hits[j].Score
	})
	return hits
}

func excludedMap(tf map[string]uint32, terms []string) bool {
	for _, t := range terms {
		if tf[t] > 0 {
			return true
		}
	}
	return false
}

func phraseInTokens(toks []token.Tok, phrases [][]string) bool {
	pos := map[string][]uint32{}
	for _, t := range toks {
		if token.Stop[t.Term] {
			continue
		}
		pos[t.Term] = append(pos[t.Term], uint32(t.Pos))
	}
	for _, ph := range phrases {
		ok := false
		for _, start := range pos[ph[0]] {
			match := true
			for i := 1; i < len(ph); i++ {
				if !hasPos(pos[ph[i]], start+uint32(i)) {
					match = false
					break
				}
			}
			if match {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
