package main

import (
	"math/rand"
	"testing"
)

func TestIntValueMapBasic(t *testing.T) {
	m := NewIntValueMap[string](0)

	if _, ok := m.Get(1); ok {
		t.Fatalf("Get on empty returned ok")
	}

	m.Put(1, "a")
	m.Put(2, "b")

	if v, ok := m.Get(1); !ok || v != "a" {
		t.Fatalf("Get(1) = %q,%v want a,true", v, ok)
	}

	if v, ok := m.Get(2); !ok || v != "b" {
		t.Fatalf("Get(2) = %q,%v want b,true", v, ok)
	}

	m.Put(1, "c") // overwrite in place, no new vals entry

	if v, _ := m.Get(1); v != "c" {
		t.Fatalf("Get(1) after overwrite = %q want c", v)
	}

	if m.Len() != 2 {
		t.Fatalf("Len = %d want 2 (overwrite must not append)", m.Len())
	}
}

func TestIntValueMapStructValues(t *testing.T) {
	type rec struct{ a, b int }

	m := NewIntValueMap[rec](0)
	m.Put(7, rec{1, 2})

	if v, ok := m.Get(7); !ok || v != (rec{1, 2}) {
		t.Fatalf("struct value not round-tripped: %v,%v", v, ok)
	}

	if _, ok := m.Get(8); ok {
		t.Fatalf("absent key returned ok")
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
		got, ok := m.Get(k)
		if !ok || got != v {
			t.Fatalf("Get(%d) = %d,%v want %d,true", k, got, ok, v)
		}
	}

	for i := 0; i < 50_000; i++ {
		k := rng.Uint64() | 1
		_, refOK := ref[k]
		_, gotOK := m.Get(k)

		if refOK != gotOK {
			t.Fatalf("presence mismatch for %d: builtin=%v intvaluemap=%v", k, refOK, gotOK)
		}
	}
}
