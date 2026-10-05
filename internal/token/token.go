package token

import (
	"strings"
	"unicode"
)

// Stop is a small English set. Kept tiny on purpose: the index, not the list, is the project.
var Stop = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "of": true,
	"to": true, "in": true, "on": true, "for": true, "is": true, "are": true,
	"with": true, "by": true, "from": true, "that": true, "this": true, "it": true,
}

type Tok struct {
	Term string
	Pos  int
}

// Analyze lowercases and splits on non-letters. Positions count kept tokens only.
func Analyze(text string, dropStop bool) []Tok {
	var out []Tok
	pos := 0
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		term := b.String()
		b.Reset()
		if dropStop && Stop[term] {
			return
		}
		out = append(out, Tok{Term: term, Pos: pos})
		pos++
	}
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}
