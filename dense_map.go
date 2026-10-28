package main

type DenseMap[K ~uint32, V any] struct {
	idx  DenseIndex
	vals Vec[V]
}

func (m *DenseMap[K, V]) get(k K) (V, bool) {
	if slot := m.idx.get(uint32(k)); slot != 0 {
		return *unsafeAt(m.vals.s, uint64(slot)), true
	}

	var zero V

	return zero, false
}

func (m *DenseMap[K, V]) put(k K, v V) {
	if slot := m.idx.get(uint32(k)); slot != 0 {
		*unsafeAt(m.vals.s, uint64(slot)) = v

		return
	}

	if len(m.vals.s) == 0 {
		m.vals.pushBack(*new(V))
	}

	m.vals.pushBack(v)
	m.idx.put(uint32(k), uint32(len(m.vals.s)-1))
}
