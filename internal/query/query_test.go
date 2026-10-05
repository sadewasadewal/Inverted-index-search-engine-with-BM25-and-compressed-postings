package query

import (
	"testing"

	"lexicon/internal/index"
)

func docs() []index.Doc {
	return []index.Doc{
		{Title: "Raft commit rule", Path: "raft.txt", Text: "A leader commits an entry only when a majority matches the commit index and the entry is from the current term."},
		{Title: "Election notes", Path: "election.txt", Text: "Leader election uses a randomized timeout. A follower votes once per term."},
		{Title: "Postings compression", Path: "postings.txt", Text: "Delta encoded postings and skip pointers cut the size of an inverted index. BM25 ranks the hits."},
		{Title: "Elasticsearch is a product", Path: "elastic.txt", Text: "Calling a hosted search cluster is not the same as building postings compression."},
		{Title: "Phrase check", Path: "phrase.txt", Text: "The commit index advances only after the match index reaches a majority."},
	}
}

func TestAndAndPhrase(t *testing.T) {
	idx := index.Build(docs())
	hits := Search(idx, `"commit index"`, 5)
	if len(hits) != 2 {
		t.Fatalf("phrase hits %d: %+v", len(hits), hits)
	}
	scan := Scan(idx, `"commit index"`)
	if len(scan) != len(hits) {
		t.Fatalf("scan %d index %d", len(scan), len(hits))
	}
	for i := range hits {
		if scan[i].DocID != hits[i].DocID {
			t.Fatalf("rank mismatch at %d", i)
		}
	}
}

func TestNotAndOr(t *testing.T) {
	idx := index.Build(docs())
	hits := Search(idx, "postings -elasticsearch", 5)
	if len(hits) != 1 || hits[0].Path != "postings.txt" {
		t.Fatalf("%+v", hits)
	}
	or := Search(idx, "election OR bm25", 5)
	if len(or) < 2 {
		t.Fatalf("or hits %+v", or)
	}
}

func TestBM25PrefersRepeatedTerm(t *testing.T) {
	idx := index.Build([]index.Doc{
		{Title: "once", Path: "a.txt", Text: "bm25 appears once in this longer unrelated note about logs and disks and files and buffers"},
		{Title: "often", Path: "b.txt", Text: "bm25 bm25 bm25 bm25"},
	})
	hits := Search(idx, "bm25", 2)
	if hits[0].Path != "b.txt" {
		t.Fatalf("want repeated term first, got %+v", hits)
	}
}
