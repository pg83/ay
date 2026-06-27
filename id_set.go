package main

type IdSet struct {
	gen   []uint32
	epoch uint32
}

func (s *IdSet) reset(size uint32) {
	if uint32(len(s.gen)) < size {
		grown := uint32(len(s.gen)) * 2

		if grown < size {
			grown = size
		}

		s.gen = make([]uint32, grown)
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
	id := uint32(v)

	return id < uint32(len(s.gen)) && s.gen[id] == s.epoch
}

func (s *IdSet) add(v VFS) {
	id := uint32(v)

	if id >= uint32(len(s.gen)) {
		grown := uint32(len(s.gen)) * 2

		if grown <= id {
			grown = id + 1
		}

		g := make([]uint32, grown)

		copy(g, s.gen)
		s.gen = g
	}

	s.gen[id] = s.epoch
}

func (s *IdSet) spliceNew(win []VFS, block []VFS, k int) int {
	gen := s.gen
	epoch := s.epoch

	for _, v := range win {
		if gen[v] == epoch {
			continue
		}

		gen[v] = epoch

		block[k] = v
		k++
	}

	return k
}
