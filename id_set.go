package main

// chaosEpochStart stands in for the first epoch of every epoch-stamped table,
// so tests reach the wraparound without 2^16 or 2^32 resets.
var chaosEpochStart = newChaosNumber("epoch-start")

type IdSet struct {
	gen   Vec[uint16]
	epoch uint16
}

func (s *IdSet) reset(size uint32) {
	if s.gen.freshLen(int(size)) {
		s.epoch = uint16(chaosEpochStart.number(1))

		return
	}

	s.epoch++

	if s.epoch == 0 {
		clear(s.gen.s)

		s.epoch = 1
	}
}

func (s *IdSet) has(v VFS) bool {
	id := v.strID()

	return id < uint32(s.gen.len()) && s.gen.s[id] == s.epoch
}

func (s *IdSet) add(v VFS) {
	id := v.strID()

	s.gen.ensureLen(int(id) + 1)

	s.gen.s[id] = s.epoch
}

func (s *IdSet) spliceOne(v VFS, block []VFS, k int) int {
	id := v.strID()

	if s.gen.s[id] == s.epoch {
		return k
	}

	s.gen.s[id] = s.epoch
	block[k] = v

	return k + 1
}

func (s *IdSet) spliceNew(win []VFS, block []VFS, k int) int {
	gen := s.gen.s
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
