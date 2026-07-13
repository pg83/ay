package main

type DenseMap[K ~uint32, V any] struct {
	idx  Vec[uint32]
	vals Vec[V]
}

func (m *DenseMap[K, V]) get(k K) (V, bool) {
	if int(k) < len(m.idx.s) {
		if slot := m.idx.s[k]; slot != 0 {
			return m.vals.s[slot], true
		}
	}

	var zero V

	return zero, false
}

func (m *DenseMap[K, V]) put(k K, v V) {
	if int(k) < len(m.idx.s) {
		if slot := m.idx.s[k]; slot != 0 {
			m.vals.s[slot] = v

			return
		}
	}

	if len(m.vals.s) == 0 {
		m.vals.pushBack(*new(V))
	}

	m.idx.ensureLen(int(k) + 1)
	m.vals.pushBack(v)
	m.idx.s[k] = uint32(len(m.vals.s) - 1)
}

func (m *DenseMap[K, V]) len() int {
	if len(m.vals.s) == 0 {
		return 0
	}

	return len(m.vals.s) - 1
}
