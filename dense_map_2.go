package main

type DenseRow2[V1, V2 any] struct {
	v1  V1
	v2  V2
	set uint8
}

type DenseMap2[K ~uint32, V1, V2 any] struct {
	idx  Vec[uint32]
	rows Vec[DenseRow2[V1, V2]]
}

func (m *DenseMap2[K, V1, V2]) rowSlot(k K) uint32 {
	if int(k) < len(m.idx.s) {
		return m.idx.s[k]
	}

	return 0
}

func (m *DenseMap2[K, V1, V2]) ensureRow(k K) uint32 {
	if int(k) < len(m.idx.s) {
		if slot := m.idx.s[k]; slot != 0 {
			return slot
		}
	}

	if len(m.rows.s) == 0 {
		m.rows.pushBack(DenseRow2[V1, V2]{})
	}

	m.idx.ensureLen(int(k) + 1)
	m.rows.pushBack(DenseRow2[V1, V2]{})

	slot := uint32(len(m.rows.s) - 1)

	m.idx.s[k] = slot

	return slot
}

func (m *DenseMap2[K, V1, V2]) get1(k K) (V1, bool) {
	if slot := m.rowSlot(k); slot != 0 {
		row := &m.rows.s[slot]

		if row.set&1 != 0 {
			return row.v1, true
		}
	}

	var zero V1

	return zero, false
}

func (m *DenseMap2[K, V1, V2]) put1(k K, v V1) {
	slot := m.ensureRow(k)
	row := &m.rows.s[slot]

	row.v1 = v
	row.set |= 1
}

func (m *DenseMap2[K, V1, V2]) get2(k K) (V2, bool) {
	if slot := m.rowSlot(k); slot != 0 {
		row := &m.rows.s[slot]

		if row.set&2 != 0 {
			return row.v2, true
		}
	}

	var zero V2

	return zero, false
}

func (m *DenseMap2[K, V1, V2]) put2(k K, v V2) {
	slot := m.ensureRow(k)
	row := &m.rows.s[slot]

	row.v2 = v
	row.set |= 2
}

func (m *DenseMap2[K, V1, V2]) len() int {
	if len(m.rows.s) == 0 {
		return 0
	}

	return len(m.rows.s) - 1
}
