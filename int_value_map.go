package main

type IntValueMap[V any] struct {
	idx  *IntMap[uint32]
	vals Vec[V]
}

func newIntValueMap[V any](hint int) *IntValueMap[V] {
	return &IntValueMap[V]{
		idx: newIntMap[uint32](hint),
	}
}

func (m *IntValueMap[V]) get(k uint64) *V {
	if i := m.idx.get(k); i != nil {
		return &m.vals.s[*i]
	}

	return nil
}

func (m *IntValueMap[V]) cell(k uint64) (*V, bool) {
	cell, existed := m.idx.cell(k)

	if existed {
		return &m.vals.s[*cell], true
	}

	*cell = uint32(m.vals.len())

	var zero V

	m.vals.pushBack(zero)

	return &m.vals.s[*cell], false
}

func (m *IntValueMap[V]) put(k uint64, v V) {
	cell, existed := m.idx.cell(k)

	if existed {
		m.vals.s[*cell] = v

		return
	}

	*cell = uint32(m.vals.len())
	m.vals.pushBack(v)
}

func (m *IntValueMap[V]) len() int {
	return m.vals.len()
}
