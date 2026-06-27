package main

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

func (m *IntValueMap[V]) get(k uint64) *V {
	if i := m.idx.get(k); i != nil {
		return &m.vals[*i]
	}

	return nil
}

func (m *IntValueMap[V]) put(k uint64, v V) {
	cell, existed := m.idx.cell(k)

	if existed {
		m.vals[*cell] = v

		return
	}

	*cell = uint32(len(m.vals))

	m.vals = append(m.vals, v)
}

func (m *IntValueMap[V]) len() int {
	return len(m.vals)
}
