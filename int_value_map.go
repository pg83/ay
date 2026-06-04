package main

// IntValueMap maps uint64 keys to V, but stores the values contiguously in a
// side slice and keeps only a uint32 index into it in the hash table. It is
// built on IntMap[uint32], so the table entries stay small (8-byte key + 4-byte
// index) regardless of sizeof(V) and the values pack densely for locality. An
// insert is a single probe via IntMap.Cell: the index cell is found-or-reserved
// and the dense index written in place (the C++ "idx[k] returns a writable
// reference" pattern). Same constraints as IntMap — identity-hashed (keys must
// be uniform), single-goroutine, no delete, key 0 reserved.
type IntValueMap[V any] struct {
	idx  *IntMap[uint32]
	vals []V
}

func NewIntValueMap[V any](hint int) *IntValueMap[V] {
	return &IntValueMap[V]{
		idx:  NewIntMap[uint32](hint),
		vals: make([]V, 0, hint),
	}
}

// Get returns the value for k and whether it was present.
func (m *IntValueMap[V]) Get(k uint64) (V, bool) {
	if i, ok := m.idx.Get(k); ok {
		return m.vals[i], true
	}

	var zero V

	return zero, false
}

// Put inserts or overwrites the value for k. A new key appends to vals and
// records its index in the table cell; an existing key overwrites in place.
func (m *IntValueMap[V]) Put(k uint64, v V) {
	cell, existed := m.idx.Cell(k)

	if existed {
		m.vals[*cell] = v

		return
	}

	*cell = uint32(len(m.vals))
	m.vals = append(m.vals, v)
}

// Len reports the number of distinct keys stored.
func (m *IntValueMap[V]) Len() int { return len(m.vals) }
