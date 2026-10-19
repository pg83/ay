package main

type IdValueMap struct {
	gen   Vec[uint32]
	val   []int32
	epoch uint32
}

func (m *IdValueMap) reset(size uint32) {
	if m.gen.freshLen(int(size)) {
		m.val = make([]int32, m.gen.len())
		m.epoch = uint32(chaosFirstEpoch(1))

		return
	}

	m.epoch++

	if m.epoch == 0 {
		clear(m.gen.s)

		m.epoch = 1
	}
}

func (m *IdValueMap) put(k VFS, v int32) {
	id := uint32(k)

	m.gen.s[id] = m.epoch
	m.val[id] = v
}

func (m *IdValueMap) get(k VFS) (int32, bool) {
	id := uint32(k)

	if id < uint32(m.gen.len()) && m.gen.s[id] == m.epoch {
		return m.val[id], true
	}

	return 0, false
}
