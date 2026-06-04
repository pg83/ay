package main

import (
	"math/rand"
	"testing"
)

func TestIntMapBasicPutGet(t *testing.T) {
	m := NewIntMap[int](0)

	if _, ok := m.Get(42); ok {
		t.Fatalf("Get on empty map returned ok")
	}

	m.Put(42, 100)

	if v, ok := m.Get(42); !ok || v != 100 {
		t.Fatalf("Get(42) = %d,%v want 100,true", v, ok)
	}

	if m.Len() != 1 {
		t.Fatalf("Len = %d want 1", m.Len())
	}
}

func TestIntMapOverwrite(t *testing.T) {
	m := NewIntMap[string](0)
	m.Put(7, "a")
	m.Put(7, "b")

	if v, ok := m.Get(7); !ok || v != "b" {
		t.Fatalf("Get(7) = %q,%v want \"b\",true", v, ok)
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
		if v, ok := m.Get(k); !ok || v != i {
			t.Fatalf("Get(%d) = %d,%v want %d,true", k, v, ok, i)
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
		if v, ok := m.Get(i * 0x9E3779B97F4A7C15); !ok || v != i {
			t.Fatalf("after grow Get(key %d) = %d,%v want %d,true", i, v, ok, i)
		}
	}

	if _, ok := m.Get(0xdeadbeefdeadbeef); ok {
		t.Fatalf("Get of absent key returned ok")
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
		got, ok := m.Get(k)
		if !ok || got != v {
			t.Fatalf("Get(%d) = %d,%v want %d,true", k, got, ok, v)
		}
	}

	// Negative lookups over the full 64-bit space (mostly absent), non-zero.
	for i := 0; i < 100_000; i++ {
		k := rng.Uint64() | 1
		_, refOK := ref[k]
		_, gotOK := m.Get(k)

		if refOK != gotOK {
			t.Fatalf("presence mismatch for %d: builtin=%v intmap=%v", k, refOK, gotOK)
		}
	}
}

// Pointer value type round-trips (the codegen split map stores *GeneratedFileInfo).
func TestIntMapPointerValues(t *testing.T) {
	type box struct{ n int }

	m := NewIntMap[*box](0)
	b := &box{n: 9}
	m.Put(123, b)

	if got, ok := m.Get(123); !ok || got != b {
		t.Fatalf("pointer value not round-tripped")
	}

	if got, ok := m.Get(124); ok || got != nil {
		t.Fatalf("absent pointer Get = %v,%v want nil,false", got, ok)
	}
}
