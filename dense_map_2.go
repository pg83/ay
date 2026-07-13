package main

type DenseMap2[K ~uint32, V1, V2 any] struct {
	idx   Vec[uint32]
	set   Vec[uint8]
	vals1 Vec[V1]
	vals2 Vec[V2]
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

	if len(m.set.s) == 0 {
		m.set.pushBack(0)
		m.vals1.pushBack(*new(V1))
		m.vals2.pushBack(*new(V2))
	}

	m.idx.ensureLen(int(k) + 1)
	m.set.pushBack(0)
	m.vals1.pushBack(*new(V1))
	m.vals2.pushBack(*new(V2))

	slot := uint32(len(m.set.s) - 1)

	m.idx.s[k] = slot

	return slot
}

func (m *DenseMap2[K, V1, V2]) get1(k K) (V1, bool) {
	if slot := m.rowSlot(k); slot != 0 {
		if *unsafeAt(m.set.s, uint64(slot))&1 != 0 {
			return *unsafeAt(m.vals1.s, uint64(slot)), true
		}
	}

	var zero V1

	return zero, false
}

func (m *DenseMap2[K, V1, V2]) put1(k K, v V1) {
	slot := m.ensureRow(k)

	*unsafeAt(m.vals1.s, uint64(slot)) = v
	*unsafeAt(m.set.s, uint64(slot)) |= 1
}

func (m *DenseMap2[K, V1, V2]) get2(k K) (V2, bool) {
	if slot := m.rowSlot(k); slot != 0 {
		if *unsafeAt(m.set.s, uint64(slot))&2 != 0 {
			return *unsafeAt(m.vals2.s, uint64(slot)), true
		}
	}

	var zero V2

	return zero, false
}

func (m *DenseMap2[K, V1, V2]) put2(k K, v V2) {
	slot := m.ensureRow(k)

	*unsafeAt(m.vals2.s, uint64(slot)) = v
	*unsafeAt(m.set.s, uint64(slot)) |= 2
}

func (m *DenseMap2[K, V1, V2]) len() int {
	if len(m.set.s) == 0 {
		return 0
	}

	return len(m.set.s) - 1
}
