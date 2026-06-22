package main

type DenseMap[K ~uint32, V any] struct {
	idx  []uint32
	vals []V
}

func (m *DenseMap[K, V]) get(k K) (V, bool) {
	if int(k) < len(m.idx) {
		if slot := m.idx[k]; slot != 0 {
			return m.vals[slot], true
		}
	}

	var zero V

	return zero, false
}

func (m *DenseMap[K, V]) put(k K, v V) {
	if int(k) < len(m.idx) {
		if slot := m.idx[k]; slot != 0 {
			m.vals[slot] = v

			return
		}
	}

	if len(m.vals) == 0 {
		m.vals = append(m.vals, *new(V))
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

func (m *DenseMap[K, V]) len() int {
	if len(m.vals) == 0 {
		return 0
	}

	return len(m.vals) - 1
}
