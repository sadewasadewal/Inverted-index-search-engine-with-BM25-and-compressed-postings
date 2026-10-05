package posting

import "testing"

func sample(n int) []Posting {
	var list []Posting
	for i := 0; i < n; i++ {
		list = append(list, Posting{DocID: uint32(i * 3), TF: uint32(1 + i%4), Positions: []uint32{uint32(i), uint32(i + 2)}})
	}
	return list
}

func TestRoundTrip(t *testing.T) {
	in := sample(40)
	got := Decompress(Compress(in))
	if len(got) != len(in) {
		t.Fatalf("len %d want %d", len(got), len(in))
	}
	for i := range in {
		if got[i].DocID != in[i].DocID || got[i].TF != in[i].TF || len(got[i].Positions) != 2 || got[i].Positions[0] != in[i].Positions[0] {
			t.Fatalf("posting %d mismatch: %+v want %+v", i, got[i], in[i])
		}
	}
}

func TestSkipIntersectMatchesSorted(t *testing.T) {
	a := sample(50)
	var b []Posting
	for i := 0; i < 50; i += 2 {
		b = append(b, Posting{DocID: uint32(i * 3), TF: 1, Positions: []uint32{1}})
	}
	got := Intersect(Compress(a), Compress(b))
	if len(got) != len(b) {
		t.Fatalf("intersect %d want %d (%v)", len(got), len(b), got)
	}
	for i, doc := range got {
		if doc != b[i].DocID {
			t.Fatalf("doc %d: %d want %d", i, doc, b[i].DocID)
		}
	}
}

func TestSeekSkipsBlocks(t *testing.T) {
	list := sample(64)
	c := NewCursor(Compress(list))
	if !c.Seek(90) { // 30*3 = 90
		t.Fatal("seek failed")
	}
	if c.Doc() != 90 {
		t.Fatalf("doc %d", c.Doc())
	}
	if c.bi < 1 {
		t.Fatal("expected to skip at least one frame of 16")
	}
}
