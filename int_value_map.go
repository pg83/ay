package main

// IntValueMap maps uint64 keys to V, storing values contiguously in a side slice
// and keeping only a uint32 index in the hash table. Built on IntMap[uint32], so
// table entries stay small (8-byte key + 4-byte index) regardless of sizeof(V)
// and values pack densely. Insert is a single IntMap.Cell probe. Same
// constraints as IntMap: identity-hashed, single-goroutine, no delete, key 0
// reserved.
type IntValueMap[V any] struct {
	idx  *IntMap[uint32]
	vals []V
}

func newIntValueMap[V any](hint int) *IntValueMap[V] {
	return &IntValueMap[V]{
		idx:  newIntMap[uint32](hint),
		vals: make([]V, 0, hint),
	}
}

// Get returns a pointer to the value for k, or nil if absent. Valid until the
// next Put grows vals.
func (m *IntValueMap[V]) get(k uint64) *V {
	if i := m.idx.get(k); i != nil {
		return &m.vals[*i]
	}

	return nil
}

// Put inserts or overwrites the value for k. A new key appends to vals and
// records its index; an existing key overwrites in place.
func (m *IntValueMap[V]) put(k uint64, v V) {
	cell, existed := m.idx.cell(k)

	if existed {
		m.vals[*cell] = v

		return
	}

	*cell = uint32(len(m.vals))
	m.vals = append(m.vals, v)
}

// Len reports the number of distinct keys stored.
func (m *IntValueMap[V]) len() int {
	return len(m.vals)
}
