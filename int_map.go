package main

// IntMap is an open-addressing hash map from uint64 keys to V that uses the key
// itself as its hash: the home slot is key & mask, with no mixing. It is meant
// for keys that are already well-distributed hashes (e.g. the high 64 bits of an
// xxh3-128), where identity indexing matches a real hash's spread while skipping
// the per-probe key hashing the runtime map performs (runtime maps re-hash even
// a uint64 key). Collisions resolve by linear probing over a power-of-two table,
// which is cache-friendly at the moderate load factor used here.
//
// Single-goroutine use only (no internal locking). There is no delete — the
// callers (intern table, codegen split, search-tier and source-under caches) are
// insert/lookup only.
//
// Key 0 doubles as the empty-slot sentinel, so it is stored out of band
// (freeVal/hasFree) and stays a usable key.
type IntMap[V any] struct {
	data     []intMapEntry[V]
	mask     uint64
	count    int // entries in data (excludes the out-of-band zero key)
	resizeAt int // grow once count reaches this
	freeVal  V
	hasFree  bool
}

type intMapEntry[V any] struct {
	k uint64
	v V
}

const (
	// intMapMinCap is the smallest table; must be a power of two.
	intMapMinCap = 8
	// intMapFillNum/intMapFillDen express the max load factor (count/cap) as a
	// rational to avoid float rounding: grow when count*Den >= cap*Num (= 5/8).
	intMapFillNum = 5
	intMapFillDen = 8
)

// NewIntMap returns a map sized to hold at least hint keys without growing.
func NewIntMap[V any](hint int) *IntMap[V] {
	c := intMapMinCap

	// Grow c (power of two) until its load-factor threshold covers hint.
	for c*intMapFillNum < hint*intMapFillDen {
		c <<= 1
	}

	m := &IntMap[V]{}
	m.alloc(c)

	return m
}

func (m *IntMap[V]) alloc(capacity int) {
	m.data = make([]intMapEntry[V], capacity)
	m.mask = uint64(capacity - 1)
	m.resizeAt = capacity * intMapFillNum / intMapFillDen
}

// Get returns the value stored for k and whether it was present.
func (m *IntMap[V]) Get(k uint64) (V, bool) {
	if k == 0 {
		return m.freeVal, m.hasFree
	}

	for i := k & m.mask; ; i = (i + 1) & m.mask {
		switch m.data[i].k {
		case k:
			return m.data[i].v, true
		case 0:
			var zero V

			return zero, false
		}
	}
}

// Put inserts or overwrites the value for k.
func (m *IntMap[V]) Put(k uint64, v V) {
	if k == 0 {
		m.freeVal = v
		m.hasFree = true

		return
	}

	for i := k & m.mask; ; i = (i + 1) & m.mask {
		switch m.data[i].k {
		case k:
			m.data[i].v = v

			return
		case 0:
			m.data[i] = intMapEntry[V]{k, v}
			m.count++

			if m.count >= m.resizeAt {
				m.grow()
			}

			return
		}
	}
}

// grow doubles the table and reinserts the live entries. The zero key lives out
// of band, so it survives untouched and is not part of the rehash.
func (m *IntMap[V]) grow() {
	old := m.data
	m.alloc(len(old) * 2)
	m.count = 0

	for _, e := range old {
		if e.k != 0 {
			m.Put(e.k, e.v)
		}
	}
}

// Len reports the number of distinct keys stored.
func (m *IntMap[V]) Len() int {
	n := m.count

	if m.hasFree {
		n++
	}

	return n
}
