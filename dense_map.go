package main

// DenseMap is a map from a small dense integer key K (an interned id — STR/VFS)
// to V, backed by two arrays instead of a hash map: idx, indexed by K, holds a
// 1-based slot into vals (0 means absent); vals is the compact, append-only value
// store. Lookup is a bounds check plus two loads — no hashing, no probing — which
// is why it replaces the hot map[STR] lookups. The idx array grows geometrically
// on demand and stays narrow (4 bytes/key) while values pack densely, so it pays
// off when the key space is large and sparsely populated. Single-goroutine use.
type DenseMap[K ~uint32, V any] struct {
	idx  []uint32
	vals []V
}

// Get returns the value for k and whether it was present.
func (m *DenseMap[K, V]) get(k K) (V, bool) {
	if int(k) < len(m.idx) {
		if slot := m.idx[k]; slot != 0 {
			return m.vals[slot], true
		}
	}

	var zero V

	return zero, false
}

// Put inserts or overwrites the value for k.
func (m *DenseMap[K, V]) put(k K, v V) {
	if int(k) < len(m.idx) {
		if slot := m.idx[k]; slot != 0 {
			m.vals[slot] = v

			return
		}
	}

	if len(m.vals) == 0 {
		m.vals = append(m.vals, *new(V)) // reserve slot 0 as the absent sentinel
	}

	m.growIdx(int(k))
	m.vals = append(m.vals, v)
	m.idx[k] = uint32(len(m.vals) - 1)
}

func (m *DenseMap[K, V]) growIdx(k int) {
	if k < len(m.idx) {
		return
	}

	n := len(m.idx) * 2

	if n <= k {
		n = k + 1
	}

	grown := make([]uint32, n)
	copy(grown, m.idx)
	m.idx = grown
}

// Len reports the number of distinct keys stored.
func (m *DenseMap[K, V]) Len() int {
	if len(m.vals) == 0 {
		return 0
	}

	return len(m.vals) - 1
}
