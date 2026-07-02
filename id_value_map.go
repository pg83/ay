package main

type IdValueMap struct {
	gen   []uint32
	val   []int32
	epoch uint32
}

func (m *IdValueMap) reset(size uint32) {
	if uint32(len(m.gen)) < size {
		grown := uint32(len(m.gen)) * 2

		if grown < size {
			grown = size
		}

		m.gen = make([]uint32, grown)
		m.val = make([]int32, grown)
		m.epoch = 1

		return
	}

	m.epoch++

	if m.epoch == 0 {
		for i := range m.gen {
			m.gen[i] = 0
		}

		m.epoch = 1
	}
}

func (m *IdValueMap) put(k VFS, v int32) {
	id := uint32(k)

	if id >= uint32(len(m.gen)) {
		grown := uint32(len(m.gen)) * 2

		if grown <= id {
			grown = id + 1
		}

		g := make([]uint32, grown)
		vals := make([]int32, grown)

		copy(g, m.gen)
		copy(vals, m.val)
		m.gen = g
		m.val = vals
	}

	m.gen[id] = m.epoch
	m.val[id] = v
}

func (m *IdValueMap) get(k VFS) (int32, bool) {
	id := uint32(k)

	if id < uint32(len(m.gen)) && m.gen[id] == m.epoch {
		return m.val[id], true
	}

	return 0, false
}
