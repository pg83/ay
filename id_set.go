package main

type IdSet struct {
	gen   []uint16
	epoch uint16
}

func (s *IdSet) reset(size uint32) {
	if uint32(len(s.gen)) < size {
		grown := uint32(len(s.gen)) * 2

		if grown < size {
			grown = size
		}

		s.gen = make([]uint16, grown)
		s.epoch = 1

		return
	}

	s.epoch++

	if s.epoch == 0 {
		for i := range s.gen {
			s.gen[i] = 0
		}

		s.epoch = 1
	}
}

func (s *IdSet) has(v VFS) bool {
	id := v.strID()

	return id < uint32(len(s.gen)) && s.gen[id] == s.epoch
}

func (s *IdSet) add(v VFS) {
	id := v.strID()

	if id >= uint32(len(s.gen)) {
		grown := uint32(len(s.gen)) * 2

		if grown <= id {
			grown = id + 1
		}

		g := make([]uint16, grown)

		copy(g, s.gen)
		s.gen = g
	}

	s.gen[id] = s.epoch
}

func (s *IdSet) spliceNew(win []VFS, block []VFS, k int) int {
	gen := s.gen
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
