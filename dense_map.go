package main

type DenseMap[K ~uint32, V any] struct {
	idx  Vec[uint32]
	vals Vec[V]
}

func (m *DenseMap[K, V]) get(k K) (V, bool) {
	if int(k) < len(m.idx.s) {
		if slot := m.idx.s[k]; slot != 0 {
			return *unsafeAt(m.vals.s, uint64(slot)), true
		}
	}

	var zero V

	return zero, false
}

func (m *DenseMap[K, V]) put(k K, v V) {
	if int(k) < len(m.idx.s) {
		if slot := m.idx.s[k]; slot != 0 {
			*unsafeAt(m.vals.s, uint64(slot)) = v

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

type DenseMap2[K ~uint32, V1 any, V2 any] struct {
	m1 DenseMap[K, V1]
	m2 DenseMap[K, V2]
}

func (m *DenseMap2[K, V1, V2]) get1(k K) (V1, bool) {
	return m.m1.get(k)
}

func (m *DenseMap2[K, V1, V2]) put1(k K, v V1) {
	m.m1.put(k, v)
}

func (m *DenseMap2[K, V1, V2]) get2(k K) (V2, bool) {
	return m.m2.get(k)
}

func (m *DenseMap2[K, V1, V2]) put2(k K, v V2) {
	m.m2.put(k, v)
}
