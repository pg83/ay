package main

import (
	"math/rand"
	"testing"
)

func TestIntValueMapBasic(t *testing.T) {
	m := NewIntValueMap[string](0)

	if m.Get(1) != nil {
		t.Fatalf("Get on empty returned non-nil")
	}

	m.Put(1, "a")
	m.Put(2, "b")

	if p := m.Get(1); p == nil || *p != "a" {
		t.Fatalf("Get(1) = %v want *a", p)
	}

	if p := m.Get(2); p == nil || *p != "b" {
		t.Fatalf("Get(2) = %v want *b", p)
	}

	m.Put(1, "c") // overwrite in place, no new vals entry

	if p := m.Get(1); p == nil || *p != "c" {
		t.Fatalf("Get(1) after overwrite = %v want *c", p)
	}

	if m.Len() != 2 {
		t.Fatalf("Len = %d want 2 (overwrite must not append)", m.Len())
	}
}

func TestIntValueMapStructValues(t *testing.T) {
	type rec struct{ a, b int }

	m := NewIntValueMap[rec](0)
	m.Put(7, rec{1, 2})

	if p := m.Get(7); p == nil || *p != (rec{1, 2}) {
		t.Fatalf("struct value not round-tripped: %v", p)
	}

	if m.Get(8) != nil {
		t.Fatalf("absent key returned non-nil")
	}
}

// Differential test for IntValueMap vs the builtin map across grows, overwrites
// and negative lookups. Keys non-zero (0 is the reserved sentinel).
func TestIntValueMapMatchesBuiltin(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	ref := map[uint64]int64{}
	m := NewIntValueMap[int64](0)

	for i := 0; i < 200_000; i++ {
		k := rng.Uint64()%30_000 + 1
		v := rng.Int63()
		ref[k] = v
		m.Put(k, v)

		if m.Len() != len(ref) {
			t.Fatalf("step %d: Len = %d want %d", i, m.Len(), len(ref))
		}
	}

	for k, v := range ref {
		if p := m.Get(k); p == nil || *p != v {
			t.Fatalf("Get(%d) = %v want *%d", k, p, v)
		}
	}

	for i := 0; i < 50_000; i++ {
		k := rng.Uint64() | 1
		_, refOK := ref[k]
		gotOK := m.Get(k) != nil

		if refOK != gotOK {
			t.Fatalf("presence mismatch for %d: builtin=%v intvaluemap=%v", k, refOK, gotOK)
		}
	}
}
