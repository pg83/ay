package main

import (
	"math/rand"
	"testing"
)

func TestIntValueMapBasic(t *testing.T) {
	m := newIntValueMap[string](0)

	if m.get(1) != nil {
		t.Fatalf("Get on empty returned non-nil")
	}

	m.put(1, "a")
	m.put(2, "b")

	if p := m.get(1); p == nil || *p != "a" {
		t.Fatalf("Get(1) = %v want *a", p)
	}

	if p := m.get(2); p == nil || *p != "b" {
		t.Fatalf("Get(2) = %v want *b", p)
	}

	m.put(1, "c") // overwrite in place, no new vals entry

	if p := m.get(1); p == nil || *p != "c" {
		t.Fatalf("Get(1) after overwrite = %v want *c", p)
	}

	if m.len() != 2 {
		t.Fatalf("Len = %d want 2 (overwrite must not append)", m.len())
	}
}

func TestIntValueMapStructValues(t *testing.T) {
	type rec struct{ a, b int }

	m := newIntValueMap[rec](0)
	m.put(7, rec{1, 2})

	if p := m.get(7); p == nil || *p != (rec{1, 2}) {
		t.Fatalf("struct value not round-tripped: %v", p)
	}

	if m.get(8) != nil {
		t.Fatalf("absent key returned non-nil")
	}
}

// Differential test for IntValueMap vs the builtin map across grows, overwrites
// and negative lookups. Keys non-zero (0 is the reserved sentinel).
func TestIntValueMapMatchesBuiltin(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	ref := map[uint64]int64{}
	m := newIntValueMap[int64](0)

	for i := 0; i < 200_000; i++ {
		k := rng.Uint64()%30_000 + 1
		v := rng.Int63()
		ref[k] = v
		m.put(k, v)

		if m.len() != len(ref) {
			t.Fatalf("step %d: Len = %d want %d", i, m.len(), len(ref))
		}
	}

	for k, v := range ref {
		if p := m.get(k); p == nil || *p != v {
			t.Fatalf("Get(%d) = %v want *%d", k, p, v)
		}
	}

	for i := 0; i < 50_000; i++ {
		k := rng.Uint64() | 1
		_, refOK := ref[k]
		gotOK := m.get(k) != nil

		if refOK != gotOK {
			t.Fatalf("presence mismatch for %d: builtin=%v intvaluemap=%v", k, refOK, gotOK)
		}
	}
}
