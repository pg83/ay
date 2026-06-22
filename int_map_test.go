package main

import (
	"math/rand"
	"testing"
)

func TestIntMapBasicPutGet(t *testing.T) {
	m := newIntMap[int](0)

	if m.get(42) != nil {
		t.Fatalf("Get on empty map returned non-nil")
	}

	m.put(42, 100)

	if p := m.get(42); p == nil || *p != 100 {
		t.Fatalf("Get(42) = %v want *100", p)
	}

	if m.len() != 1 {
		t.Fatalf("Len = %d want 1", m.len())
	}
}

func TestIntMapOverwrite(t *testing.T) {
	m := newIntMap[string](0)
	m.put(7, "a")
	m.put(7, "b")

	if p := m.get(7); p == nil || *p != "b" {
		t.Fatalf("Get(7) = %v want *\"b\"", p)
	}

	if m.len() != 1 {
		t.Fatalf("Len = %d want 1 after overwrite", m.len())
	}
}

func TestIntMapCapacityIsPowerOfTwo(t *testing.T) {
	for _, hint := range []int{0, 1, 7, 8, 100, 1000, 1 << 20} {
		m := newIntMap[int](hint)
		c := len(m.data)

		if c&(c-1) != 0 || c < intMapMinCap {
			t.Fatalf("hint %d: capacity %d not a power of two >= %d", hint, c, intMapMinCap)
		}

		if c*intMapFillNum < hint*intMapFillDen {
			t.Fatalf("hint %d: capacity %d threshold below hint", hint, c)
		}
	}
}

func TestIntMapCollisionAndWraparound(t *testing.T) {
	m := newIntMap[int](0)
	cap0 := uint64(len(m.data))

	keys := []uint64{1, 1 + cap0, 1 + 2*cap0, 7, 7 + cap0, 7 + 2*cap0}

	for i, k := range keys {
		m.put(k, i)
	}

	for i, k := range keys {
		if p := m.get(k); p == nil || *p != i {
			t.Fatalf("Get(%d) = %v want *%d", k, p, i)
		}
	}
}

func TestIntMapGrowKeepsAll(t *testing.T) {
	m := newIntMap[uint64](0)
	const n = 100_000

	for i := uint64(1); i <= n; i++ {
		m.put(i*0x9E3779B97F4A7C15, i)
	}

	if m.len() != n {
		t.Fatalf("Len = %d want %d", m.len(), n)
	}

	for i := uint64(1); i <= n; i++ {
		if p := m.get(i * 0x9E3779B97F4A7C15); p == nil || *p != i {
			t.Fatalf("after grow Get(key %d) = %v want *%d", i, p, i)
		}
	}

	if m.get(0xdeadbeefdeadbeef) != nil {
		t.Fatalf("Get of absent key returned non-nil")
	}
}

func TestIntMapMatchesBuiltin(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	ref := map[uint64]int64{}
	m := newIntMap[int64](0)

	for i := 0; i < 300_000; i++ {
		k := rng.Uint64()%40_000 + 1
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

	for i := 0; i < 100_000; i++ {
		k := rng.Uint64() | 1
		_, refOK := ref[k]
		gotOK := m.get(k) != nil

		if refOK != gotOK {
			t.Fatalf("presence mismatch for %d: builtin=%v intmap=%v", k, refOK, gotOK)
		}
	}
}

func TestIntMapCell(t *testing.T) {
	m := newIntMap[int](0)

	p, existed := m.cell(5)

	if existed {
		t.Fatalf("Cell(5) existed on empty map")
	}

	if *p != 0 {
		t.Fatalf("new cell = %d want 0", *p)
	}

	*p = 99

	if g := m.get(5); g == nil || *g != 99 {
		t.Fatalf("Get(5) after Cell write = %v want *99", g)
	}

	p2, existed2 := m.cell(5)

	if !existed2 || *p2 != 99 {
		t.Fatalf("Cell(5) again = %d,%v want 99,true", *p2, existed2)
	}

	*p2 = 100

	if g := m.get(5); g == nil || *g != 100 {
		t.Fatalf("update via Cell = %v want *100", g)
	}

	if m.len() != 1 {
		t.Fatalf("Len = %d want 1", m.len())
	}
}

func TestIntMapCellGrows(t *testing.T) {
	m := newIntMap[int](0)
	const n = 50_000

	for i := 1; i <= n; i++ {
		p, existed := m.cell(uint64(i) * 0x9E3779B97F4A7C15)

		if existed {
			t.Fatalf("Cell reported existing for fresh key %d", i)
		}

		*p = i
	}

	if m.len() != n {
		t.Fatalf("Len = %d want %d", m.len(), n)
	}

	for i := 1; i <= n; i++ {
		if p := m.get(uint64(i) * 0x9E3779B97F4A7C15); p == nil || *p != i {
			t.Fatalf("Get(key %d) = %v want *%d", i, p, i)
		}
	}
}

func TestIntMapPointerValues(t *testing.T) {
	type box struct{ n int }

	m := newIntMap[*box](0)
	b := &box{n: 9}
	m.put(123, b)

	if p := m.get(123); p == nil || *p != b {
		t.Fatalf("pointer value not round-tripped")
	}

	if m.get(124) != nil {
		t.Fatalf("absent pointer Get returned non-nil")
	}
}
