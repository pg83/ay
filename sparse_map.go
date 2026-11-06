package main

type SparseIndex struct {
	m IntMap[uint32]
}

func sparseKey(k uint32) uint64 {
	return mix64(uint64(k) + 1)
}

func (idx *SparseIndex) get(k uint32) uint32 {
	if slot := idx.m.get(sparseKey(k)); slot != nil {
		return *slot
	}

	return 0
}

func (idx *SparseIndex) put(k, slot uint32) {
	idx.m.put(sparseKey(k), slot)
}

type SparseMap[K ~uint32, V any] struct {
	idx  SparseIndex
	vals Vec[V]
}

func newSparseMap[K ~uint32, V any](hint int) SparseMap[K, V] {
	var m SparseMap[K, V]

	m.idx.m.init(hint)

	return m
}

func (m *SparseMap[K, V]) get(k K) (V, bool) {
	if slot := m.idx.get(uint32(k)); slot != 0 {
		return *unsafeAt(m.vals.s, uint64(slot)), true
	}

	var zero V

	return zero, false
}

func (m *SparseMap[K, V]) put(k K, v V) {
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

func (m *SparseMap[K, V]) values() []V {
	if len(m.vals.s) == 0 {
		return nil
	}

	return m.vals.s[1:]
}
