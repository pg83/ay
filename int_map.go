package main

// IntMap is an open-addressing hash map from uint64 keys to V that uses the key
// itself as its hash (home slot = key & mask, no mixing). Intended for keys that
// are already well-distributed hashes, where identity indexing matches a real
// hash's spread while skipping the per-probe re-hash the runtime map performs.
// Collisions resolve by linear probing over a power-of-two table.
//
// Single-goroutine use only (no locking). No delete — insert/lookup only.
//
// Key 0 is reserved as the empty-slot sentinel and must not be inserted; Get(0)
// is undefined. Callers whose keys can be 0 handle that out of band.
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
	// intMapFillNum/intMapFillDen are the max load factor as a rational (5/8):
	// grow when count*Den >= cap*Num.
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
// Returning a pointer avoids materialising a zero V on a miss and copying V on a
// hit. Valid only until the next mutating call (Put/Cell may move the array).
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
// present, inserting a zero-valued entry when absent. It is the find-or-insert
// primitive: the caller writes the value through the returned pointer. Cell
// grows the table *before* returning, so the cell is never in a soon-to-be-
// reallocated array, but the next Put/Cell may move it.
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

				break // at capacity — grow, then re-probe
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
