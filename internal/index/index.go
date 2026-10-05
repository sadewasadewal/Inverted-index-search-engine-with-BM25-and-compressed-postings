package index

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"sort"

	"lexicon/internal/posting"
	"lexicon/internal/token"
)

type Doc struct {
	ID    uint32 `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
	Len   int    `json:"len"`
	Text  string `json:"text,omitempty"`
}

type termPostings struct {
	DF    int
	List  []posting.Posting
	Blob  []byte
}

// Idx is an in-memory inverted index with a compressed postings image per term.
type Idx struct {
	Docs   []Doc
	Terms  map[string]*termPostings
	AvgDL  float64
	N      int
}

func Build(docs []Doc) *Idx {
	idx := &Idx{Docs: docs, Terms: map[string]*termPostings{}}
	var total int
	for di := range docs {
		docs[di].ID = uint32(di)
		toks := token.Analyze(docs[di].Title+" "+docs[di].Text, true)
		docs[di].Len = len(toks)
		total += len(toks)
		seen := map[string]*posting.Posting{}
		order := []string{}
		for _, t := range toks {
			p, ok := seen[t.Term]
			if !ok {
				p = &posting.Posting{DocID: docs[di].ID}
				seen[t.Term] = p
				order = append(order, t.Term)
			}
			p.TF++
			p.Positions = append(p.Positions, uint32(t.Pos))
		}
		for _, term := range order {
			tp := idx.Terms[term]
			if tp == nil {
				tp = &termPostings{}
				idx.Terms[term] = tp
			}
			tp.List = append(tp.List, *seen[term])
			tp.DF++
		}
	}
	idx.N = len(docs)
	if idx.N > 0 {
		idx.AvgDL = float64(total) / float64(idx.N)
	}
	idx.Docs = docs
	for _, tp := range idx.Terms {
		sort.Slice(tp.List, func(i, j int) bool { return tp.List[i].DocID < tp.List[j].DocID })
		tp.Blob = posting.Compress(tp.List)
	}
	return idx
}

func (idx *Idx) Blob(term string) []byte {
	tp := idx.Terms[term]
	if tp == nil {
		return nil
	}
	return tp.Blob
}

func (idx *Idx) List(term string) []posting.Posting {
	tp := idx.Terms[term]
	if tp == nil {
		return nil
	}
	return tp.List
}

// BM25 is Robertson-Sparck Jones with Lucene's k1=1.2, b=0.75.
func (idx *Idx) BM25(term string, tf uint32, dl int) float64 {
	tp := idx.Terms[term]
	if tp == nil || idx.AvgDL == 0 {
		return 0
	}
	const k1 = 1.2
	const b = 0.75
	n := float64(idx.N)
	df := float64(tp.DF)
	idf := math.Log(1 + (n-df+0.5)/(df+0.5))
	ft := float64(tf)
	norm := 1 - b + b*float64(dl)/idx.AvgDL
	return idf * (ft * (k1 + 1)) / (ft + k1*norm)
}

func (idx *Idx) CompressedBytes() int {
	n := 0
	for _, tp := range idx.Terms {
		n += len(tp.Blob)
	}
	return n
}

func (idx *Idx) RawPostingBytes() int {
	n := 0
	for _, tp := range idx.Terms {
		for _, p := range tp.List {
			n += 4 + 4 + 4*len(p.Positions)
		}
	}
	return n
}

type fileHeader struct {
	Docs  []Doc             `json:"docs"`
	AvgDL float64           `json:"avgdl"`
	Terms []string          `json:"terms"`
}

// Save writes a JSON dictionary plus a postings blob. The blob is the compressed image.
func (idx *Idx) Save(path string) error {
	terms := make([]string, 0, len(idx.Terms))
	for t := range idx.Terms {
		terms = append(terms, t)
	}
	sort.Strings(terms)
	blob := []byte{}
	offsets := make([]uint32, len(terms))
	for i, t := range terms {
		offsets[i] = uint32(len(blob))
		blob = append(blob, idx.Terms[t].Blob...)
	}
	head := fileHeader{Docs: idx.Docs, AvgDL: idx.AvgDL, Terms: terms}
	meta, err := json.Marshal(head)
	if err != nil {
		return err
	}
	var out []byte
	out = binary.BigEndian.AppendUint32(out, uint32(len(meta)))
	out = append(out, meta...)
	offb := make([]byte, 4*len(offsets))
	for i, o := range offsets {
		binary.BigEndian.PutUint32(offb[i*4:], o)
	}
	out = binary.BigEndian.AppendUint32(out, uint32(len(offb)))
	out = append(out, offb...)
	out = append(out, blob...)
	return os.WriteFile(path, out, 0o644)
}

func Load(path string) (*Idx, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(b[:4])
	var head fileHeader
	if err := json.Unmarshal(b[4:4+n], &head); err != nil {
		return nil, err
	}
	at := 4 + int(n)
	no := binary.BigEndian.Uint32(b[at : at+4])
	at += 4
	offb := b[at : at+int(no)]
	at += int(no)
	blob := b[at:]
	idx := &Idx{Docs: head.Docs, AvgDL: head.AvgDL, N: len(head.Docs), Terms: map[string]*termPostings{}}
	for i, term := range head.Terms {
		start := binary.BigEndian.Uint32(offb[i*4:])
		end := uint32(len(blob))
		if i+1 < len(head.Terms) {
			end = binary.BigEndian.Uint32(offb[(i+1)*4:])
		}
		raw := append([]byte(nil), blob[start:end]...)
		idx.Terms[term] = &termPostings{DF: dfOf(raw), List: posting.Decompress(raw), Blob: raw}
	}
	return idx, nil
}

func dfOf(raw []byte) int {
	return len(posting.Decompress(raw))
}
