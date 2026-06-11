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
// callers are insert/lookup only.
//
// Key 0 is reserved as the empty-slot sentinel and must not be inserted; Get(0)
// is undefined. Callers whose keys can be 0 handle that out of band — the intern
// table, the sole caller, absorbs a 0 hi-hash through its lo-verify + exact
// string overflow, so it never reaches IntMap with key 0.
type IntMap[V any] struct {
	data     []IntMapEntry[V]
	mask     uint64
	count    int // number of stored keys
	resizeAt int // grow once count reaches this
}

type IntMapEntry[V any] struct {
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
func newIntMap[V any](hint int) *IntMap[V] {
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
	m.data = make([]IntMapEntry[V], capacity)
	m.mask = uint64(capacity - 1)
	m.resizeAt = capacity * intMapFillNum / intMapFillDen
}

// Get returns a pointer to the value stored for k, or nil if k is absent.
// Returning a pointer (rather than (V, bool)) avoids materialising a zero V on a
// miss and copying V on a hit — which matters when V is large. The pointer is
// valid only until the next mutating call (Put/Cell may grow and move the
// backing array).
func (m *IntMap[V]) get(k uint64) *V {
	for i := k & m.mask; ; i = (i + 1) & m.mask {
		switch m.data[i].k {
		case k:
			return &m.data[i].v
		case 0:
			return nil
		}
	}
}

// Cell returns a pointer to the value cell for k and whether k was already
// present, inserting a zero-valued entry when absent. It is the single-probe
// find-or-insert primitive — Go's analogue of C++ `map[k]` returning a writable
// reference — so the caller initialises or updates the value by writing through
// the returned pointer. The pointer is valid only for the caller's immediate
// use: Cell grows the table *before* returning (so the returned cell is never in
// a soon-to-be-reallocated array), but the next Put/Cell may move it.
func (m *IntMap[V]) cell(k uint64) (*V, bool) {
	for {
		i := k & m.mask

		for {
			ek := m.data[i].k

			if ek == k {
				return &m.data[i].v, true
			}

			if ek == 0 {
				if m.count < m.resizeAt {
					m.data[i].k = k
					m.count++

					return &m.data[i].v, false
				}

				break // at capacity — grow, then re-probe in the larger table
			}

			i = (i + 1) & m.mask
		}

		m.grow()
	}
}

// Put inserts or overwrites the value for k.
func (m *IntMap[V]) put(k uint64, v V) {
	cell, _ := m.cell(k)
	*cell = v
}

// grow doubles the table and reinserts the live entries.
func (m *IntMap[V]) grow() {
	old := m.data
	m.alloc(len(old) * 2)
	m.count = 0

	for _, e := range old {
		if e.k != 0 {
			m.put(e.k, e.v)
		}
	}
}

// Len reports the number of distinct keys stored.
func (m *IntMap[V]) len() int {
	return m.count
}
