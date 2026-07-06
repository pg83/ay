package main

type denseRow3 struct {
	i1, i2, i3 uint32
}

type DenseMap3[K ~uint32, V1, V2, V3 any] struct {
	idx   Vec[uint32]
	rows  Vec[denseRow3]
	vals1 Vec[V1]
	vals2 Vec[V2]
	vals3 Vec[V3]
}

func (m *DenseMap3[K, V1, V2, V3]) rowSlot(k K) uint32 {
	if int(k) < m.idx.len() {
		return m.idx.s[k]
	}

	return 0
}

func (m *DenseMap3[K, V1, V2, V3]) ensureRow(k K) uint32 {
	if int(k) < m.idx.len() {
		if slot := m.idx.s[k]; slot != 0 {
			return slot
		}
	}

	if m.rows.len() == 0 {
		m.rows.pushBack(denseRow3{})
	}

	m.idx.ensureLen(int(k) + 1)
	m.rows.pushBack(denseRow3{})

	slot := uint32(m.rows.len() - 1)

	m.idx.s[k] = slot

	return slot
}

func (m *DenseMap3[K, V1, V2, V3]) get1(k K) (V1, bool) {
	if slot := m.rowSlot(k); slot != 0 {
		if vi := m.rows.s[slot].i1; vi != 0 {
			return m.vals1.s[vi], true
		}
	}

	var zero V1

	return zero, false
}

func (m *DenseMap3[K, V1, V2, V3]) put1(k K, v V1) {
	slot := m.ensureRow(k)

	if vi := m.rows.s[slot].i1; vi != 0 {
		m.vals1.s[vi] = v

		return
	}

	if m.vals1.len() == 0 {
		m.vals1.pushBack(*new(V1))
	}

	m.vals1.pushBack(v)
	m.rows.s[slot].i1 = uint32(m.vals1.len() - 1)
}

func (m *DenseMap3[K, V1, V2, V3]) get2(k K) (V2, bool) {
	if slot := m.rowSlot(k); slot != 0 {
		if vi := m.rows.s[slot].i2; vi != 0 {
			return m.vals2.s[vi], true
		}
	}

	var zero V2

	return zero, false
}

func (m *DenseMap3[K, V1, V2, V3]) put2(k K, v V2) {
	slot := m.ensureRow(k)

	if vi := m.rows.s[slot].i2; vi != 0 {
		m.vals2.s[vi] = v

		return
	}

	if m.vals2.len() == 0 {
		m.vals2.pushBack(*new(V2))
	}

	m.vals2.pushBack(v)
	m.rows.s[slot].i2 = uint32(m.vals2.len() - 1)
}

func (m *DenseMap3[K, V1, V2, V3]) get3(k K) (V3, bool) {
	if slot := m.rowSlot(k); slot != 0 {
		if vi := m.rows.s[slot].i3; vi != 0 {
			return m.vals3.s[vi], true
		}
	}

	var zero V3

	return zero, false
}

func (m *DenseMap3[K, V1, V2, V3]) put3(k K, v V3) {
	slot := m.ensureRow(k)

	if vi := m.rows.s[slot].i3; vi != 0 {
		m.vals3.s[vi] = v

		return
	}

	if m.vals3.len() == 0 {
		m.vals3.pushBack(*new(V3))
	}

	m.vals3.pushBack(v)
	m.rows.s[slot].i3 = uint32(m.vals3.len() - 1)
}

func (m *DenseMap3[K, V1, V2, V3]) len() int {
	if m.rows.len() == 0 {
		return 0
	}

	return m.rows.len() - 1
}
