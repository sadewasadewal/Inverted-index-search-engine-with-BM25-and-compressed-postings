package posting

import "encoding/binary"

// Posting is one document hit for a term. Positions are token offsets, used for phrases.
type Posting struct {
	DocID     uint32
	TF        uint32
	Positions []uint32
}

const SkipEvery = 16

func appendU(buf []byte, v uint32) []byte {
	var tmp [10]byte
	n := binary.PutUvarint(tmp[:], uint64(v))
	return append(buf, tmp[:n]...)
}

func readU(b []byte, i int) (uint32, int) {
	v, n := binary.Uvarint(b[i:])
	if n <= 0 {
		return 0, len(b)
	}
	return uint32(v), i + n
}

// Compress delta-encodes docIDs and positions.
// Every 16 postings a skip record stores the block's first docID and the byte offset of the next block.
func Compress(list []Posting) []byte {
	if len(list) == 0 {
		return nil
	}
	body := make([]byte, 0, len(list)*4)
	starts := []int{}
	for i := 0; i < len(list); i += SkipEvery {
		end := i + SkipEvery
		if end > len(list) {
			end = len(list)
		}
		starts = append(starts, len(body))
		body = appendU(body, list[i].DocID)
		body = append(body, 0, 0, 0, 0)
		prev := uint32(0)
		for j := i; j < end; j++ {
			p := list[j]
			body = appendU(body, p.DocID-prev+1)
			body = appendU(body, p.TF)
			body = appendU(body, uint32(len(p.Positions)))
			pprev := uint32(0)
			for _, pos := range p.Positions {
				body = appendU(body, pos-pprev+1)
				pprev = pos
			}
			prev = p.DocID
		}
	}
	for bi, start := range starts {
		next := len(body)
		if bi+1 < len(starts) {
			next = starts[bi+1]
		}
		_, at := readU(body, start)
		body[at] = byte(next)
		body[at+1] = byte(next >> 8)
		body[at+2] = byte(next >> 16)
		body[at+3] = byte(next >> 24)
	}
	return body
}

// Block is one skip frame.
type Block struct {
	First uint32
	Next  int
	Docs  []Posting
}

// Blocks decodes skip frames. Intersection seeks by First, then scans inside the frame.
func Blocks(b []byte) []Block {
	var out []Block
	at := 0
	for at < len(b) {
		first, n := readU(b, at)
		at = n
		if at+4 > len(b) {
			break
		}
		next := int(uint32(b[at]) | uint32(b[at+1])<<8 | uint32(b[at+2])<<16 | uint32(b[at+3])<<24)
		at += 4
		blk := Block{First: first, Next: next}
		prev := uint32(0)
		for at < next && at < len(b) {
			delta, n2 := readU(b, at)
			at = n2
			tf, n2 := readU(b, at)
			at = n2
			np, n2 := readU(b, at)
			at = n2
			p := Posting{DocID: prev + delta - 1, TF: tf}
			pprev := uint32(0)
			for k := uint32(0); k < np; k++ {
				pd, n3 := readU(b, at)
				at = n3
				pos := pprev + pd - 1
				p.Positions = append(p.Positions, pos)
				pprev = pos
			}
			prev = p.DocID
			blk.Docs = append(blk.Docs, p)
		}
		out = append(out, blk)
		if next <= at-1 && next <= len(b) {
			at = next
		}
		if next <= 0 {
			break
		}
		at = next
	}
	return out
}

func Decompress(b []byte) []Posting {
	var out []Posting
	for _, blk := range Blocks(b) {
		out = append(out, blk.Docs...)
	}
	return out
}

// Cursor walks a compressed list using skip frames.
type Cursor struct {
	blocks []Block
	bi, di int
}

func NewCursor(blob []byte) *Cursor {
	return &Cursor{blocks: Blocks(blob), bi: 0, di: 0}
}

func (c *Cursor) done() bool {
	return c.bi >= len(c.blocks)
}

func (c *Cursor) Doc() uint32 {
	return c.blocks[c.bi].Docs[c.di].DocID
}

func (c *Cursor) Posting() Posting {
	return c.blocks[c.bi].Docs[c.di]
}

// Seek moves to the first docID >= target. Returns false if none remain.
func (c *Cursor) Seek(target uint32) bool {
	if c.done() {
		return false
	}
	for c.bi < len(c.blocks) && c.blocks[c.bi].Docs[len(c.blocks[c.bi].Docs)-1].DocID < target {
		c.bi++
		c.di = 0
	}
	if c.done() {
		return false
	}
	docs := c.blocks[c.bi].Docs
	for c.di < len(docs) && docs[c.di].DocID < target {
		c.di++
	}
	if c.di >= len(docs) {
		c.bi++
		c.di = 0
		return c.Seek(target)
	}
	return true
}

func (c *Cursor) Next() bool {
	if c.done() {
		return false
	}
	c.di++
	if c.di >= len(c.blocks[c.bi].Docs) {
		c.bi++
		c.di = 0
	}
	return !c.done()
}

// Intersect returns docIDs present in every compressed list, seeking by skip frame.
func Intersect(blobs ...[]byte) []uint32 {
	if len(blobs) == 0 {
		return nil
	}
	cs := make([]*Cursor, len(blobs))
	for i, b := range blobs {
		cs[i] = NewCursor(b)
		if cs[i].done() || !cs[i].Seek(0) {
			return nil
		}
	}
	var out []uint32
	for {
		target := cs[0].Doc()
		for _, c := range cs[1:] {
			if c.Doc() > target {
				target = c.Doc()
			}
		}
		all := true
		for _, c := range cs {
			if !c.Seek(target) {
				return out
			}
			if c.Doc() != target {
				all = false
			}
		}
		if all {
			out = append(out, target)
			if !cs[0].Next() {
				return out
			}
		}
	}
}
