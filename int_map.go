package main

type IntMap[V any] struct {
	data     []IntMapEntry[V]
	mask     uint64
	count    int
	resizeAt int
}

type IntMapEntry[V any] struct {
	k uint64
	v V
}

const (
	intMapMinCap = 8

	intMapFillNum = 5
	intMapFillDen = 8
)

func newIntMap[V any](hint int) *IntMap[V] {
	c := intMapMinCap

	for c*intMapFillNum < hint*intMapFillDen {
		c <<= 1
	}

	m := &IntMap[V]{}

	m.alloc(c)

	return m
}

func (m *IntMap[V]) alloc(capacity int) {
	m.data = make([]IntMapEntry[V], capacity)
	m.mask = uint64(capacity - 1)
	m.resizeAt = capacity * intMapFillNum / intMapFillDen
}

func (m *IntMap[V]) get(k uint64) *V {
	for i := k & m.mask; ; i = (i + 1) & m.mask {
		switch m.data[i].k {
		case k:
			return &m.data[i].v
		case 0:
			return nil
		}
	}
}

func (m *IntMap[V]) cell(k uint64) (*V, bool) {
	for {
		i := k & m.mask

		for {
			ek := m.data[i].k

			if ek == k {
				return &m.data[i].v, true
			}

			if ek == 0 {
				if m.count < m.resizeAt {
					m.data[i].k = k
					m.count++

					return &m.data[i].v, false
				}

				break
			}

			i = (i + 1) & m.mask
		}

		m.grow()
	}
}

func (m *IntMap[V]) put(k uint64, v V) {
	cell, _ := m.cell(k)

	*cell = v
}

func (m *IntMap[V]) grow() {
	old := m.data

	m.alloc(len(old) * 2)
	m.count = 0

	for _, e := range old {
		if e.k != 0 {
			m.put(e.k, e.v)
		}
	}
}

func (m *IntMap[V]) len() int {
	return m.count
}
