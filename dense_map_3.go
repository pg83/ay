package main

type DenseRow3 struct {
	i1, i2, i3 uint32
}

type DenseMap3[K ~uint32, V1, V2, V3 any] struct {
	idx   []uint32
	rows  []DenseRow3
	vals1 []V1
	vals2 []V2
	vals3 []V3
}

func (m *DenseMap3[K, V1, V2, V3]) rowSlot(k K) uint32 {
	if int(k) < len(m.idx) {
		return m.idx[k]
	}

	return 0
}

func (m *DenseMap3[K, V1, V2, V3]) ensureRow(k K) uint32 {
	if int(k) < len(m.idx) {
		if slot := m.idx[k]; slot != 0 {
			return slot
		}
	}

	if len(m.rows) == 0 {
		m.rows = append(m.rows, DenseRow3{})
	}

	m.growIdx(int(k))
	m.rows = append(m.rows, DenseRow3{})

	slot := uint32(len(m.rows) - 1)

	m.idx[k] = slot

	return slot
}

func (m *DenseMap3[K, V1, V2, V3]) growIdx(k int) {
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

func (m *DenseMap3[K, V1, V2, V3]) get1(k K) (V1, bool) {
	if slot := m.rowSlot(k); slot != 0 {
		if vi := m.rows[slot].i1; vi != 0 {
			return m.vals1[vi], true
		}
	}

	var zero V1

	return zero, false
}

func (m *DenseMap3[K, V1, V2, V3]) put1(k K, v V1) {
	slot := m.ensureRow(k)

	if vi := m.rows[slot].i1; vi != 0 {
		m.vals1[vi] = v

		return
	}

	if len(m.vals1) == 0 {
		m.vals1 = append(m.vals1, *new(V1))
	}

	m.vals1 = append(m.vals1, v)
	m.rows[slot].i1 = uint32(len(m.vals1) - 1)
}

func (m *DenseMap3[K, V1, V2, V3]) get2(k K) (V2, bool) {
	if slot := m.rowSlot(k); slot != 0 {
		if vi := m.rows[slot].i2; vi != 0 {
			return m.vals2[vi], true
		}
	}

	var zero V2

	return zero, false
}

func (m *DenseMap3[K, V1, V2, V3]) put2(k K, v V2) {
	slot := m.ensureRow(k)

	if vi := m.rows[slot].i2; vi != 0 {
		m.vals2[vi] = v

		return
	}

	if len(m.vals2) == 0 {
		m.vals2 = append(m.vals2, *new(V2))
	}

	m.vals2 = append(m.vals2, v)
	m.rows[slot].i2 = uint32(len(m.vals2) - 1)
}

func (m *DenseMap3[K, V1, V2, V3]) get3(k K) (V3, bool) {
	if slot := m.rowSlot(k); slot != 0 {
		if vi := m.rows[slot].i3; vi != 0 {
			return m.vals3[vi], true
		}
	}

	var zero V3

	return zero, false
}

func (m *DenseMap3[K, V1, V2, V3]) put3(k K, v V3) {
	slot := m.ensureRow(k)

	if vi := m.rows[slot].i3; vi != 0 {
		m.vals3[vi] = v

		return
	}

	if len(m.vals3) == 0 {
		m.vals3 = append(m.vals3, *new(V3))
	}

	m.vals3 = append(m.vals3, v)
	m.rows[slot].i3 = uint32(len(m.vals3) - 1)
}

func (m *DenseMap3[K, V1, V2, V3]) len() int {
	if len(m.rows) == 0 {
		return 0
	}

	return len(m.rows) - 1
}
