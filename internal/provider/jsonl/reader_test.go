package jsonl

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func readAll(t *testing.T, r *Reader) []string {
	t.Helper()
	var out []string
	for {
		line, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(line))
	}
}

func TestReaderCompleteLinesOnly(t *testing.T) {
	in := "{\"a\":1}\n\n  \r\n{\"b\":2}\r\n{\"partial\""
	r := NewReader(strings.NewReader(in), 0)
	got := readAll(t, r)
	if len(got) != 2 || got[0] != `{"a":1}` || got[1] != `{"b":2}` {
		t.Fatalf("got %q", got)
	}
	if !r.Partial {
		t.Error("esperava Partial")
	}
	if want := int64(len(in) - len(`{"partial"`)); r.Offset != want {
		t.Errorf("Offset = %d, want %d", r.Offset, want)
	}
}

func TestReaderNoPartialWhenEndsWithNewline(t *testing.T) {
	r := NewReader(strings.NewReader("{}\n"), 0)
	readAll(t, r)
	if r.Partial || r.Offset != 3 || r.Lines != 1 {
		t.Fatalf("Partial=%v Offset=%d Lines=%d", r.Partial, r.Offset, r.Lines)
	}
}

// Linhas maiores que o buffer interno (64 KB) precisam chegar inteiras.
func TestReaderLongLineWithinLimit(t *testing.T) {
	big := `{"x":"` + strings.Repeat("a", 300_000) + `"}`
	r := NewReader(strings.NewReader(big+"\n{}\n"), 1<<20)
	got := readAll(t, r)
	if len(got) != 2 || got[0] != big {
		t.Fatalf("linha longa corrompida: %d linhas, len0=%d", len(got), len(got[0]))
	}
}

func TestReaderSkipsLinesOverLimit(t *testing.T) {
	big := strings.Repeat("z", 200_000)
	r := NewReader(strings.NewReader("{}\n"+big+"\n{\"ok\":1}\n"), 100_000)
	got := readAll(t, r)
	if len(got) != 2 || got[1] != `{"ok":1}` {
		t.Fatalf("got %q", got)
	}
	if r.LongLines != 1 {
		t.Errorf("LongLines = %d", r.LongLines)
	}
}

func TestReaderEmpty(t *testing.T) {
	r := NewReader(strings.NewReader(""), 0)
	if got := readAll(t, r); len(got) != 0 || r.Partial {
		t.Fatalf("got %q partial=%v", got, r.Partial)
	}
}
