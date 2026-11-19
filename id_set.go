package main

var idSetCells struct {
	gen   Vec[uint16]
	epoch uint16
}

type IdSet struct {
	epoch uint16
}

func (s *IdSet) reset(size uint32) {
	c := &idSetCells

	if c.gen.freshLen(int(size)) {
		c.epoch = uint16(chaosFirstEpoch(1))
	} else if c.epoch++; c.epoch == 0 {
		clear(c.gen.s)

		c.epoch = 1
	}

	s.epoch = c.epoch
}

func (s *IdSet) has(v VFS) bool {
	id := v.strID()

	return id < uint32(idSetCells.gen.len()) && idSetCells.gen.s[id] == s.epoch
}

func (s *IdSet) add(v VFS) {
	id := v.strID()

	idSetCells.gen.ensureLen(int(id) + 1)

	idSetCells.gen.s[id] = s.epoch
}

func (s *IdSet) spliceOne(v VFS, block []VFS, k int) int {
	id := v.strID()

	if idSetCells.gen.s[id] == s.epoch {
		return k
	}

	idSetCells.gen.s[id] = s.epoch
	block[k] = v

	return k + 1
}

func (s *IdSet) spliceNew(win []VFS, block []VFS, k int) int {
	gen := idSetCells.gen.s
	epoch := s.epoch

	for _, v := range win {
		id := v.strID()

		if gen[id] == epoch {
			continue
		}

		gen[id] = epoch
		block[k] = v
		k++
	}

	return k
}
