# 🔍 Lexicon — Inverted Index Search Engine Lab

Lexicon is a lightweight, dependency-free full-text search engine and information retrieval lab written from scratch in Go. It demonstrates how modern search engines (like Lucene, Elasticsearch, and Vespa) actually work under the hood: converting text into token postings, delta-compressing them with skip lists, performing fast multi-term intersection, and ranking results with **BM25**.

Lexicon also implements a linear document scanner as a correctness baseline, ensuring the inverted index algorithm returns identical results while executing an order of magnitude faster.

---

## 📑 Table of Contents

- [Core Concepts Explained Simply](#-core-concepts-explained-simply)
- [How It Works (Architecture)](#-how-it-works-architecture)
- [Project Structure](#-project-structure)
- [Quickstart & Usage](#-quickstart--usage)
  - [1. Run Unit Tests](#1-run-unit-tests)
  - [2. Build the Index](#2-build-the-index)
  - [3. Search via CLI](#3-search-via-cli)
  - [4. Run Benchmarks & Verifications](#4-run-benchmarks--verifications)
  - [5. Run the Web UI & API](#5-run-the-web-ui--api)
- [Query Syntax Guide](#-query-syntax-guide)
- [Binary Index File Format (`index.bin`)](#-binary-index-file-format-indexbin)
- [Benchmark Results](#-benchmark-results)

---

## 🧠 Core Concepts Explained Simply

If you are new to search engines, here is how each component works:

### 1. Inverted Index
In a regular database, you look up a document by its ID and read its text. An **inverted index** flips this around: it maps each unique **term (word)** to a sorted list of documents that contain it (called a **Postings List**).
```
Term: "commit"  ──▶  Postings: [Doc 0 (tf=2), Doc 4 (tf=1)]
Term: "index"   ──▶  Postings: [Doc 0 (tf=3), Doc 2 (tf=1), Doc 4 (tf=2)]
```

### 2. Delta Encoding & Varint Compression
Storing raw 32-bit integers (`DocID`, `TF`, `Position`) for millions of tokens wastes memory. Because postings are strictly sorted by `DocID`, we only store the **difference** between consecutive IDs (`Doc[i] - Doc[i-1]`). Small delta numbers take only 1 or 2 bytes using unsigned variable-length integers (**Uvarint**), shrinking the index size by ~37% or more.

### 3. Skip Blocks (Skip Pointers)
When searching for documents containing both `"election"` and `"vote"`, you need to intersect their postings lists. Instead of inspecting every document one by one, Lexicon groups postings into **blocks of 16 documents** with a skip frame storing the block's first docID and the byte offset of the next block. If the target document is far ahead, the cursor **skips entire blocks** without decompressing them.

### 4. Phrase Search with Positions
For exact phrase queries like `"commit index"`, finding documents with both words is not enough—the words must appear **directly next to each other**. Lexicon stores token positions in each posting, enabling positional verification (`pos(index) == pos(commit) + 1`).

### 5. BM25 Relevance Scoring
Not all matching documents are equally relevant. Lexicon uses **Okapi BM25** (with Lucene parameters $k_1 = 1.2$, $b = 0.75$):
- **TF (Term Frequency)**: Documents where the term appears more often get higher scores.
- **IDF (Inverse Document Frequency)**: Rare words across the corpus carry much more weight than common words.
- **Document Length Normalization**: Prevents long documents from dominating just because they contain more words. Short, focused documents score higher.

---

## 🏗️ How It Works (Architecture)

```
 Corpus (.txt files)
        │
        ▼
 [ 1. Token Analyzer ] ──▶ Lowercases, strips punctuation & stop words
        │
        ▼
 [ 2. Index Builder ]  ──▶ Calculates TF, token positions, DF, and avg doc length
        │
        ▼
 [ 3. Postings Codec ] ──▶ Delta encodes + adds skip pointer every 16 docs
        │
        ▼
   `index.bin`         ──▶ Persisted binary file (Header + Offsets + Blobs)
        │
        ├──▶ CLI Search & Web Server (`/search?q=...`)
        │        │
        │        ▼
        └──▶ [ Query Parser & Intersector ] ──▶ BM25 Ranker ──▶ Top Hits
```

---

## 📁 Project Structure

```
lexicon/
├── cmd/
│   └── lexicon/
│       └── main.go           # CLI driver (index, search, bench, serve commands)
├── corpus/                   # Sample document corpus (text notes)
│   ├── 01-commit.txt
│   ├── 02-election.txt
│   ├── 03-postings.txt
│   └── ...
├── internal/
│   ├── token/
│   │   └── token.go          # Tokenizer, stop-word filter, positional analyzer
│   ├── posting/
│   │   ├── posting.go        # Postings list, delta compression, skip blocks & cursor
│   │   └── posting_test.go   # Roundtrip, seek, and intersection unit tests
│   ├── index/
│   │   └── index.go          # In-memory index, BM25 scoring, save/load to index.bin
│   └── query/
│       ├── query.go          # Query parser (AND, OR, NOT, phrase), search & linear scan
│       └── query_test.go     # Tests for boolean logic, phrase matching, BM25 preferences
├── ui/
│   └── index.html            # Clean dark-mode web interface for real-time search
├── go.mod                    # Go module definition (Zero external dependencies)
├── index.bin                 # Compiled binary index file
└── README.md                 # Project documentation
```

---

## 🚀 Quickstart & Usage

### Prerequisites
- [Go](https://go.dev/dl/) **1.22+** installed on your system.
- No external libraries or database servers required.

---

### 1. Run Unit Tests
Verify compression round-trips, block skipping, phrase adjacency, and ranking logic:
```bash
go test -v ./...
```

*Expected output:*
```text
=== RUN   TestRoundTrip
--- PASS: TestRoundTrip (0.00s)
=== RUN   TestSkipIntersectMatchesSorted
--- PASS: TestSkipIntersectMatchesSorted (0.00s)
=== RUN   TestSeekSkipsBlocks
--- PASS: TestSeekSkipsBlocks (0.00s)
PASS
ok      lexicon/internal/posting    0.565s
=== RUN   TestAndAndPhrase
--- PASS: TestAndAndPhrase (0.00s)
=== RUN   TestNotAndOr
--- PASS: TestNotAndOr (0.00s)
=== RUN   TestBM25PrefersRepeatedTerm
--- PASS: TestBM25PrefersRepeatedTerm (0.00s)
PASS
ok      lexicon/internal/query      0.565s
```

---

### 2. Build the Index
Index all `.txt` documents located in the `corpus/` directory and write the compressed binary index to `index.bin`:
```bash
go run ./cmd/lexicon index -corpus corpus -out index.bin
```

*Output summary:*
```text
docs=8 terms=129 raw_postings=2132B compressed=1345B ratio=0.63
```
*(Notice that delta compression reduced raw posting bytes by ~37%).*

---

### 3. Search via CLI
Execute queries directly against the built index:

```bash
# Exact phrase query (must be adjacent tokens)
go run ./cmd/lexicon search -index index.bin -q '"commit index"'

# Boolean exclusion (must include "postings", must exclude "elasticsearch")
go run ./cmd/lexicon search -index index.bin -q 'postings -elasticsearch'

# Boolean OR
go run ./cmd/lexicon search -index index.bin -q 'election OR bm25'
```

*Sample JSON Output:*
```json
[
  {
    "docId": 0,
    "title": "Raft commit rule",
    "path": "01-commit.txt",
    "score": 4.114473534230767,
    "why": "commit,index"
  },
  {
    "docId": 4,
    "title": "Phrase on the commit index",
    "path": "05-phrase.txt",
    "score": 3.620439781353737,
    "why": "commit,index"
  }
]
```

---

### 4. Run Benchmarks & Verifications
The benchmark suite evaluates queries across both the **compressed inverted index** and the **full linear scan baseline**, verifying that:
1. Every hit matched by the index matches the linear scan exactly.
2. Ranking order is consistent.
3. The index executes significantly faster than scanning raw documents.

```bash
go run ./cmd/lexicon bench -index index.bin
```

*Output:*
```text
ok "\"commit index\""           index_hits=2
ok "postings compression"       index_hits=1
ok "election OR bm25"           index_hits=3
ok "postings -elasticsearch"    index_hits=3
index_query=12.54µs scan_query=103.5µs raw=2132B compressed=1345B
```

---

### 5. Run the Web UI & API
Launch the built-in HTTP server to explore the search engine through a browser UI:

```bash
go run ./cmd/lexicon serve -index index.bin -addr 127.0.0.1:8787
```

- Open your browser to: **[http://127.0.0.1:8787](http://127.0.0.1:8787)**
- Or query the JSON API directly:
  ```bash
  curl -s "http://127.0.0.1:8787/search?q=%22commit%20index%22"
  ```

---

## 🔎 Query Syntax Guide

| Syntax | Type | Description | Example |
| :--- | :--- | :--- | :--- |
| `term1 term2` | **AND** | Documents must contain both terms | `postings compression` |
| `term1 OR term2` | **OR** | Documents containing either term | `election OR bm25` |
| `-term` | **NOT** | Exclude documents containing this term | `postings -elasticsearch` |
| `"term1 term2"` | **Phrase** | Terms must appear directly adjacent in order | `"commit index"` |
| `term1 "term2 term3"` | **Mixed** | Word AND adjacent phrase | `raft "commit index"` |

---

## 💾 Binary Index File Format (`index.bin`)

The generated `index.bin` file is structured as follows:

```
+-------------------------------------------------------+
| 4-byte uint32 (N) : Length of Metadata JSON           |
+-------------------------------------------------------+
| N bytes           : JSON {docs: [...], avgdl, terms}  |
+-------------------------------------------------------+
| 4-byte uint32 (M) : Length of Term Offsets Table      |
+-------------------------------------------------------+
| M bytes           : [uint32 term0_offset, term1, ...] |
+-------------------------------------------------------+
| Remaining bytes   : Concatenated Compressed Postings  |
|                     (Delta varints + Skip blocks)     |
+-------------------------------------------------------+
```

Inside each term's compressed posting blob:
- Every 16 documents, a skip block header stores:
  - `FirstDocID` (varint)
  - `NextBlockOffset` (4 bytes little-endian)
- Followed by delta-encoded `DocID` offsets, `TF`, and token `Positions`.

---

## 📊 Benchmark Results

| Metric | Linear Scan Baseline | Inverted Index (Lexicon) | Improvement |
| :--- | :--- | :--- | :--- |
| **Query Latency (4 queries)** | ~103.5 µs | **~12.5 µs** | **~8.2x faster** |
| **Postings Storage** | 2,132 Bytes (Raw) | **1,345 Bytes (Compressed)** | **~37% compression** |
| **Result Precision** | 100% | **100% (Identical Hits)** | Zero drift |

*(Benchmarked on Apple Silicon macOS).*
