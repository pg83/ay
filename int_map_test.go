package main

import (
	"math/rand"
	"testing"
)

func TestIntMapBasicPutGet(t *testing.T) {
	m := NewIntMap[int](0)

	if m.Get(42) != nil {
		t.Fatalf("Get on empty map returned non-nil")
	}

	m.Put(42, 100)

	if p := m.Get(42); p == nil || *p != 100 {
		t.Fatalf("Get(42) = %v want *100", p)
	}

	if m.Len() != 1 {
		t.Fatalf("Len = %d want 1", m.Len())
	}
}

func TestIntMapOverwrite(t *testing.T) {
	m := NewIntMap[string](0)
	m.Put(7, "a")
	m.Put(7, "b")

	if p := m.Get(7); p == nil || *p != "b" {
		t.Fatalf("Get(7) = %v want *\"b\"", p)
	}

	if m.Len() != 1 {
		t.Fatalf("Len = %d want 1 after overwrite", m.Len())
	}
}

func TestIntMapCapacityIsPowerOfTwo(t *testing.T) {
	for _, hint := range []int{0, 1, 7, 8, 100, 1000, 1 << 20} {
		m := NewIntMap[int](hint)
		c := len(m.data)

		if c&(c-1) != 0 || c < intMapMinCap {
			t.Fatalf("hint %d: capacity %d not a power of two >= %d", hint, c, intMapMinCap)
		}

		if c*intMapFillNum < hint*intMapFillDen {
			t.Fatalf("hint %d: capacity %d threshold below hint", hint, c)
		}
	}
}

// Keys that share a home slot (k, k+cap, k+2*cap) must all be found via probing,
// and probing must wrap around the end of the table.
func TestIntMapCollisionAndWraparound(t *testing.T) {
	m := NewIntMap[int](0) // cap 8, mask 7
	cap0 := uint64(len(m.data))

	keys := []uint64{1, 1 + cap0, 1 + 2*cap0, 7, 7 + cap0, 7 + 2*cap0} // slot 1 and slot 7 (wraps)
	for i, k := range keys {
		m.Put(k, i)
	}

	for i, k := range keys {
		if p := m.Get(k); p == nil || *p != i {
			t.Fatalf("Get(%d) = %v want *%d", k, p, i)
		}
	}
}

func TestIntMapGrowKeepsAll(t *testing.T) {
	m := NewIntMap[uint64](0)
	const n = 100_000

	for i := uint64(1); i <= n; i++ {
		m.Put(i*0x9E3779B97F4A7C15, i) // spread keys
	}

	if m.Len() != n {
		t.Fatalf("Len = %d want %d", m.Len(), n)
	}

	for i := uint64(1); i <= n; i++ {
		if p := m.Get(i * 0x9E3779B97F4A7C15); p == nil || *p != i {
			t.Fatalf("after grow Get(key %d) = %v want *%d", i, p, i)
		}
	}

	if m.Get(0xdeadbeefdeadbeef) != nil {
		t.Fatalf("Get of absent key returned non-nil")
	}
}

// Differential test: behave identically to the builtin map across a random
// mix of inserts, overwrites and lookups (positive and negative). Key 0 is
// reserved (empty sentinel), so keys are kept non-zero.
func TestIntMapMatchesBuiltin(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	ref := map[uint64]int64{}
	m := NewIntMap[int64](0)

	for i := 0; i < 300_000; i++ {
		k := rng.Uint64()%40_000 + 1 // small space → many collisions + overwrites; non-zero
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

	// Negative lookups over the full 64-bit space (mostly absent), non-zero.
	for i := 0; i < 100_000; i++ {
		k := rng.Uint64() | 1
		_, refOK := ref[k]
		gotOK := m.Get(k) != nil

		if refOK != gotOK {
			t.Fatalf("presence mismatch for %d: builtin=%v intmap=%v", k, refOK, gotOK)
		}
	}
}

// Cell is the find-or-insert primitive: it returns a writable pointer to the
// value cell and whether the key existed.
func TestIntMapCell(t *testing.T) {
	m := NewIntMap[int](0)

	p, existed := m.Cell(5)
	if existed {
		t.Fatalf("Cell(5) existed on empty map")
	}

	if *p != 0 {
		t.Fatalf("new cell = %d want 0", *p)
	}

	*p = 99

	if g := m.Get(5); g == nil || *g != 99 {
		t.Fatalf("Get(5) after Cell write = %v want *99", g)
	}

	p2, existed2 := m.Cell(5)
	if !existed2 || *p2 != 99 {
		t.Fatalf("Cell(5) again = %d,%v want 99,true", *p2, existed2)
	}

	*p2 = 100

	if g := m.Get(5); g == nil || *g != 100 {
		t.Fatalf("update via Cell = %v want *100", g)
	}

	if m.Len() != 1 {
		t.Fatalf("Len = %d want 1", m.Len())
	}
}

// Cell must keep inserting correctly across the grows it triggers.
func TestIntMapCellGrows(t *testing.T) {
	m := NewIntMap[int](0)
	const n = 50_000

	for i := 1; i <= n; i++ {
		p, existed := m.Cell(uint64(i) * 0x9E3779B97F4A7C15)
		if existed {
			t.Fatalf("Cell reported existing for fresh key %d", i)
		}

		*p = i
	}

	if m.Len() != n {
		t.Fatalf("Len = %d want %d", m.Len(), n)
	}

	for i := 1; i <= n; i++ {
		if p := m.Get(uint64(i) * 0x9E3779B97F4A7C15); p == nil || *p != i {
			t.Fatalf("Get(key %d) = %v want *%d", i, p, i)
		}
	}
}

// Pointer value type round-trips (the codegen split map stores *GeneratedFileInfo).
func TestIntMapPointerValues(t *testing.T) {
	type box struct{ n int }

	m := NewIntMap[*box](0)
	b := &box{n: 9}
	m.Put(123, b)

	if p := m.Get(123); p == nil || *p != b {
		t.Fatalf("pointer value not round-tripped")
	}

	if m.Get(124) != nil {
		t.Fatalf("absent pointer Get returned non-nil")
	}
}
